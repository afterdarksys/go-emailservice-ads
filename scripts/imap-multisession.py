"""Live multi-session notification checks, imported by the release smoke test."""
import contextlib
import imaplib


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
        ok(outsider.noop())
        for response in ('EXISTS', 'FETCH', 'EXPUNGE'):
            assert outsider.response(response)[1] == [None], ('account leak', response)
        assert ok(outsider.search(None, 'ALL')) == [b'']
        ok(outsider.close())
        ok(outsider.delete('MultiSession'))
        for client in clients:
            ok(client.close())
        ok(writer.delete('MultiSession'))
