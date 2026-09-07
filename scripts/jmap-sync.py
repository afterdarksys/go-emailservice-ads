"""Live JMAP writes, IMAP interoperability and durable state reconciliation."""
import base64
import imaplib
import json
import pathlib
import runpy
import urllib.request


def request(port, name, args, user='qa@mail.test', password='qa-new-password'):
    args = dict(args, accountId='primary')
    body = {'using': ['urn:ietf:params:jmap:core', 'urn:ietf:params:jmap:mail', 'urn:ietf:params:jmap:submission'],
            'methodCalls': [[name, args, 'test']]}
    req = urllib.request.Request(f'http://127.0.0.1:{port}/jmap/api', data=json.dumps(body).encode(),
        headers={'Content-Type': 'application/json', 'Authorization': 'Basic ' + base64.b64encode(f'{user}:{password}'.encode()).decode()})
    with urllib.request.urlopen(req, timeout=10) as response:
        method, result, _ = json.load(response)['methodResponses'][0]
    return method, result


def qualify(jmap_port, imap_port, tls):
    method, initial = request(jmap_port, 'Email/get', {'ids': []})
    assert method == 'Email/get'
    with imaplib.IMAP4('localhost', imap_port, timeout=10) as client:
        client.starttls(ssl_context=tls); client.login('qa@mail.test', 'qa-new-password')
        assert client.create('JMAPSync')[0] == 'OK'
        assert client.append('JMAPSync', None, None, b'Subject: live-jmap-sync\r\n\r\nbody\r\n')[0] == 'OK'
        method, changes = request(jmap_port, 'Email/changes', {'sinceState': initial['state']})
        assert method == 'Email/changes' and len(changes['created']) == 1, changes
        mid = changes['created'][0]
        method, written = request(jmap_port, 'Email/set', {'ifInState': changes['newState'], 'update': {mid: {'keywords/$seen': True}}})
        assert method == 'Email/set' and mid in written['updated'], written
        client.select('JMAPSync')
        assert b'\\Seen' in repr(client.fetch('1', '(FLAGS)')).encode(), 'JMAP write absent in IMAP'
        assert client.store('1', '+FLAGS', '(\\Flagged)')[0] == 'OK'
        method, stale = request(jmap_port, 'Email/set', {'ifInState': written['newState'], 'update': {mid: {'keywords/$seen': None}}})
        assert method == 'error' and stale['type'] == 'stateMismatch', stale
        method, foreign = request(jmap_port, 'Email/set', {'update': {mid: {'keywords/$seen': None}}}, 'probe@mail.test', 'isolated-test-password')
        assert method == 'Email/set' and foreign['notUpdated'][mid]['type'] == 'notFound', foreign
        client.close()
    return {'id': mid, 'state': written['newState'], 'mailbox': qualify_mailboxes(jmap_port, imap_port, tls),
            'mutations': mutation_checks()['qualify'](request, jmap_port, imap_port, tls),
            'imports': import_checks()['qualify'](request, jmap_port, imap_port, tls),
            'scheduled': schedule_checks()['qualify'](request, jmap_port),
            'composition': composition_checks()['qualify'](request, jmap_port, imap_port, tls),
            'workflows': workflow_checks()['qualify'](request, jmap_port),
            'mime': mime_checks()['qualify'](request, jmap_port)}


def schedule_checks():
    return runpy.run_path(str(pathlib.Path(__file__).with_name("jmap-scheduled.py")))


def mime_checks():
    return runpy.run_path(str(pathlib.Path(__file__).with_name("jmap-mime.py")))


def workflow_checks():
    return runpy.run_path(str(pathlib.Path(__file__).with_name("jmap-workflows.py")))


def composition_checks():
    return runpy.run_path(str(pathlib.Path(__file__).with_name("jmap-compose-send.py")))


def mutation_checks():
    return runpy.run_path(str(pathlib.Path(__file__).with_name("jmap-email-mutations.py")))


def import_checks():
    return runpy.run_path(str(pathlib.Path(__file__).with_name("jmap-import.py")))


def restored(jmap_port, checkpoint):
    mime_checks()["restored"](request, jmap_port, checkpoint["mime"])
    workflow_checks()["restored"](request, jmap_port, checkpoint["workflows"])
    composition_checks()["restored"](request, jmap_port, checkpoint["composition"])
    import_checks()["restored"](request, jmap_port, checkpoint["imports"])
    mutation_checks()["restored"](request, jmap_port, checkpoint["mutations"])
    box = checkpoint['mailbox']
    method, changes = request(jmap_port, 'Mailbox/changes', {'sinceState': box['state']})
    assert method == 'Mailbox/changes' and box['id'] in changes['updated'], changes
    method, result = request(jmap_port, 'Mailbox/get', {'ids': [box['id']]})
    assert method == 'Mailbox/get' and result['list'][0]['name'] == 'JMAPRestored', result
    assert result['list'][0]['isSubscribed'] and result['list'][0]['totalEmails'] == 1, result
    method, changes = request(jmap_port, 'Email/changes', {'sinceState': checkpoint['state']})
    assert method == 'Email/changes' and checkpoint['id'] in changes['updated'], changes
    method, result = request(jmap_port, 'Email/get', {'ids': [checkpoint['id']]})
    assert method == 'Email/get' and result['list'][0]['keywords'].get('$seen') and result['list'][0]['keywords'].get('$flagged'), result
    schedule_checks()["restored"](request, jmap_port, checkpoint["scheduled"])


def qualify_mailboxes(port, imap_port, tls):
    _, initial = request(port, 'Mailbox/get', {})
    inbox = next(b['id'] for b in initial['list'] if b['role'] == 'inbox')
    method, result = request(port, 'Mailbox/set', {'ifInState': initial['state'], 'create': {
        'parent': {'name': 'JMAPFolders'},
        'child': {'name': 'Child', 'parentId': '#parent', 'isSubscribed': True}}})
    assert method == 'Mailbox/set' and len(result['created']) == 2, result
    parent, child = (result['created'][k]['id'] for k in ('parent', 'child'))
    method, renamed = request(port, 'Mailbox/set', {'update': {parent: {'name': 'JMAPRenamed'}}})
    assert method == 'Mailbox/set' and parent in renamed['updated'], renamed
    with imaplib.IMAP4('localhost', imap_port, timeout=10) as client:
        client.starttls(ssl_context=tls); client.login('qa@mail.test', 'qa-new-password')
        assert b'JMAPRenamed/Child' in repr(client.lsub()).encode()
        assert client.append('JMAPRenamed/Child', None, None, b'Subject: mailbox-sync\r\n\r\nbody\r\n')[0] == 'OK'
    method, blocked = request(port, 'Mailbox/set', {'destroy': [parent, child, inbox]})
    assert method == 'Mailbox/set', blocked
    assert blocked['notDestroyed'][parent]['type'] == 'mailboxHasChild', blocked
    assert blocked['notDestroyed'][child]['type'] == 'mailboxHasEmail', blocked
    assert blocked['notDestroyed'][inbox]['type'] == 'forbidden', blocked
    method, stale = request(port, 'Mailbox/set', {'ifInState': initial['state'], 'update': {child: {'isSubscribed': False}}})
    assert method == 'error' and stale['type'] == 'stateMismatch', stale
    method, moved = request(port, 'Mailbox/set', {'update': {child: {'parentId': None, 'name': 'JMAPChild'}}})
    assert method == 'Mailbox/set' and child in moved['updated'], moved
    method, deleted = request(port, 'Mailbox/set', {'destroy': [parent]})
    assert method == 'Mailbox/set' and deleted['destroyed'] == [parent], deleted
    method, foreign = request(port, 'Mailbox/set', {'destroy': [child]}, 'probe@mail.test', 'isolated-test-password')
    assert method == 'Mailbox/set' and foreign['notDestroyed'][child]['type'] == 'notFound', foreign
    method, changes = request(port, 'Mailbox/changes', {'sinceState': initial['state'], 'maxChanges': 1})
    seen = set(changes['created'])
    while changes['hasMoreChanges']:
        method, changes = request(port, 'Mailbox/changes', {'sinceState': changes['newState'], 'maxChanges': 1})
        assert method == 'Mailbox/changes', changes
        seen.update(changes['created'])
    assert child in seen, changes
    method, boxes = request(port, 'Mailbox/get', {'ids': [child]})
    assert method == 'Mailbox/get' and boxes['list'][0]['totalEmails'] == 1, boxes
    with imaplib.IMAP4('localhost', imap_port, timeout=10) as client:
        client.starttls(ssl_context=tls); client.login('qa@mail.test', 'qa-new-password')
        assert client.rename('JMAPChild', 'JMAPRestored')[0] == 'OK'
    return {'id': child, 'state': boxes['state']}
