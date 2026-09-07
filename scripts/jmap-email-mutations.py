"""Live JMAP move/destroy checks with selected IMAP sessions and restore."""
import contextlib
import datetime
import imaplib


def qualify(request, port, imap_port, tls):
    def ok(result):
        assert result[0] == 'OK', result
        return result[1]
    _, created = request(port, 'Mailbox/set', {'create': {
        'source': {'name': 'JMAPMoveSource'}, 'dest': {'name': 'JMAPMoveDest'}}})
    source_id, dest_id = (created['created'][key]['id'] for key in ('source', 'dest'))
    with contextlib.ExitStack() as stack:
        source, dest = [stack.enter_context(imaplib.IMAP4('localhost', imap_port, timeout=10)) for _ in range(2)]
        for client in (source, dest):
            ok(client.starttls(ssl_context=tls)); ok(client.login('qa@mail.test', 'qa-new-password'))
        date = datetime.datetime(2020, 2, 3, 12, 0, tzinfo=datetime.timezone.utc)
        for subject, flags in [('destroy', None), ('unrelated', '(\\Deleted)'), ('move', '(\\Flagged custom)')]:
            ok(source.append('JMAPMoveSource', flags, date, f'Subject: jmap-{subject}\r\n\r\npayload\r\n'.encode()))
        _, query = request(port, 'Email/query', {'filter': {'inMailbox': source_id}})
        _, emails = request(port, 'Email/get', {'ids': query['ids']})
        ids = {email['subject']: email['id'] for email in emails['list']}
        moved, destroyed = ids['jmap-move'], ids['jmap-destroy']
        _, boxes = request(port, 'Mailbox/get', {})
        ok(source.select('JMAPMoveSource')); ok(dest.select('JMAPMoveDest'))
        source.response('EXISTS'); dest.response('EXISTS')
        method, written = request(port, 'Email/set', {'ifInState': emails['state'],
            'update': {moved: {'mailboxIds': {dest_id: True}, 'keywords/$seen': True}}, 'destroy': [destroyed]})
        assert method == 'Email/set' and moved in written['updated'] and written['destroyed'] == [destroyed], written
        ok(source.noop()); ok(dest.noop())
        assert source.response('EXPUNGE')[1] == [b'3', b'1'], 'incorrect removal sequence'
        assert ok(source.uid('search', None, 'ALL')) == [b'2'], 'unrelated Deleted message removed'
        assert dest.response('EXISTS')[1] == [b'1'], 'destination arrival missing'
        fetched = repr(ok(dest.fetch('1', '(UID FLAGS INTERNALDATE BODY.PEEK[])')))
        assert 'custom' in fetched and 'Flagged' in fetched and 'Seen' in fetched and '2020' in fetched and 'payload' in fetched, fetched
        method, stale = request(port, 'Email/set', {'ifInState': emails['state'], 'destroy': [moved]})
        assert method == 'error' and stale['type'] == 'stateMismatch', stale
        method, foreign = request(port, 'Email/set', {'destroy': [moved]}, 'probe@mail.test', 'isolated-test-password')
        assert method == 'Email/set' and foreign['notDestroyed'][moved]['type'] == 'notFound', foreign
        method, result = request(port, 'Email/get', {'ids': [moved, destroyed]})
        assert method == 'Email/get' and result['notFound'] == [destroyed], result
        assert result['list'][0]['blobId'] == moved and result['list'][0]['mailboxIds'] == {dest_id: True}, result
        # Patch membership back and forth; each move keeps the email/blob ID.
        for old, new in [(dest_id, source_id), (source_id, dest_id)]:
            method, result = request(port, 'Email/set', {'update': {moved: {'mailboxIds/' + old: None, 'mailboxIds/' + new: True}}})
            assert method == 'Email/set' and moved in result['updated'], result
        # Logout leaves the unrelated IMAP Deleted message available.
    return {'moved': moved, 'destroyed': destroyed, 'dest': dest_id,
            'state': emails['state'], 'mailbox_state': boxes['state'], 'source': source_id}


def restored(request, port, checkpoint):
    method, changes = request(port, 'Email/changes', {'sinceState': checkpoint['state']})
    assert method == 'Email/changes' and checkpoint['moved'] in changes['updated'] and checkpoint['destroyed'] in changes['destroyed'], changes
    method, emails = request(port, 'Email/get', {'ids': [checkpoint['moved'], checkpoint['destroyed']]})
    assert method == 'Email/get' and emails['notFound'] == [checkpoint['destroyed']], emails
    assert emails['list'][0]['mailboxIds'] == {checkpoint['dest']: True}, emails
    method, changes = request(port, 'Mailbox/changes', {'sinceState': checkpoint['mailbox_state']})
    assert method == 'Mailbox/changes' and {checkpoint['source'], checkpoint['dest']} <= set(changes['updated']), changes
