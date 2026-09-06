"""Live multi-session notification checks, imported by the release smoke test."""
import contextlib
import imaplib
import concurrent.futures
import threading


def qualify(host, port, tls):
    def ok(result):
        assert result[0] == 'OK', result
        return result[1]

    with contextlib.ExitStack() as stack:
        clients = []
        for _ in range(3):
            client = stack.enter_context(imaplib.IMAP4(host, port, timeout=10))
            ok(client.starttls(ssl_context=tls))
            ok(client.login('qa@mail.test', 'qa-new-password'))
            clients.append(client)
        writer = clients[0]
        outsider = stack.enter_context(imaplib.IMAP4(host, port, timeout=10))
        ok(outsider.starttls(ssl_context=tls))
        ok(outsider.login('probe@mail.test', 'isolated-test-password'))
        ok(outsider.create('MultiSession'))
        ok(outsider.select('MultiSession'))
        outsider.response('EXISTS')
        ok(writer.create('MultiSession'))
        for client in clients:
            assert ok(client.select('MultiSession')) == [b'0']
            client.response('EXISTS')
        raw = b'From: qa@mail.test\r\nSubject: multi-session\r\n\r\nTest\r\n'
        ok(writer.append('MultiSession', None, None, raw))
        for index, client in enumerate(clients):
            ok(client.noop())
            assert client.response('EXISTS')[1] == [b'1'], ('EXISTS', index)
        # Both observers wait in IDLE while the writer changes the message.
        idle_tags = []
        for client in clients[1:]:
            tag = client._new_tag()
            client.send(tag + b' IDLE\r\n')
            assert client._get_response() is None, 'IDLE did not continue'
            idle_tags.append(tag)
        assert b'\\Flagged' in repr(ok(writer.store('1', '+FLAGS', '(\\Flagged)'))).encode()
        # STORE consumes the writer's own FETCH; every other session must get one.
        for index, client in enumerate(clients[1:], 1):
            client._get_response()
            changed = client.response('FETCH')[1]
            assert len(changed) == 1 and b'\\Flagged' in (changed[0] or b''), ('FLAGS', index, changed)
            client.send(b'DONE\r\n')
            ok(client._command_complete('IDLE', idle_tags[index - 1]))
        ok(writer.store('1', '+FLAGS.SILENT', '(\\Deleted)'))
        assert writer.response('FETCH')[1] == [None], 'silent STORE echoed flags'
        for client in clients[1:]:
            ok(client.noop())
            assert b'\\Deleted' in (client.response('FETCH')[1][0] or b'')
        assert ok(writer.expunge()) == [b'1']
        for index, client in enumerate(clients[1:], 1):
            ok(client.noop())
            assert client.response('EXPUNGE')[1] == [b'1'], ('EXPUNGE', index)
        # Independent connections append and change flags at the same time.
        barrier = threading.Barrier(2)
        def append_many(client):
            barrier.wait(timeout=10)
            for _ in range(8):
                ok(client.append('MultiSession', None, None, raw))
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            jobs = [pool.submit(append_many, client) for client in clients[:2]]
            for job in jobs:
                job.result(timeout=30)
        uids = ok(writer.uid('search', None, 'ALL'))[0].split()
        assert len(uids) == len(set(uids)) == 16, ('concurrent APPEND', uids)
        barrier = threading.Barrier(2)
        def flag_many(client, flag):
            barrier.wait(timeout=10)
            ok(client.uid('store', '1:*', '+FLAGS', '(' + flag + ')'))
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            jobs = [pool.submit(flag_many, clients[0], 'writerOne'),
                    pool.submit(flag_many, clients[1], 'writerTwo')]
            for job in jobs:
                job.result(timeout=30)
        assert len(ok(writer.uid('search', None, 'KEYWORD', 'writerOne', 'KEYWORD', 'writerTwo'))[0].split()) == 16, 'concurrent STORE lost flags'
        # Abruptly drop an IDLE observer, mutate while offline, then reconcile
        # from UIDVALIDITY + a full UID listing on a new connection.
        observer = clients.pop()
        ok(observer.select('MultiSession'))
        validity = observer.response('UIDVALIDITY')[1]
        tag = observer._new_tag()
        observer.send(tag + b' IDLE\r\n')
        assert observer._get_response() is None
        observer.shutdown()
        observer.state = 'LOGOUT'  # ExitStack must not send LOGOUT on the dead socket.
        ok(writer.uid('store', uids[0], '+FLAGS', '(\\Deleted)'))
        ok(writer.expunge())
        ok(writer.append('MultiSession', None, None, raw))
        reconnected = stack.enter_context(imaplib.IMAP4(host, port, timeout=10))
        ok(reconnected.starttls(ssl_context=tls))
        ok(reconnected.login('qa@mail.test', 'qa-new-password'))
        assert ok(reconnected.select('MultiSession')) == [b'16']
        assert reconnected.response('UIDVALIDITY')[1] == validity
        after = ok(reconnected.uid('search', None, 'ALL'))[0].split()
        assert uids[0] not in after and set(uids[1:]).issubset(after)
        assert max(map(int, after)) > max(map(int, uids)), 'UID reused after reconnect'
        ok(reconnected.close())
        ok(outsider.noop())
        for response in ('EXISTS', 'FETCH', 'EXPUNGE'):
            assert outsider.response(response)[1] == [None], ('account leak', response)
        assert ok(outsider.search(None, 'ALL')) == [b'']
        ok(outsider.close())
        ok(outsider.delete('MultiSession'))
        for client in clients:
            ok(client.close())
        ok(writer.delete('MultiSession'))
