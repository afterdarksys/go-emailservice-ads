"""Live JMAP writes, IMAP interoperability and durable state reconciliation."""
import base64
import imaplib
import json
import urllib.request


def request(port, name, args, user='qa@mail.test', password='qa-new-password'):
    args = dict(args, accountId='primary')
    body = {'using': ['urn:ietf:params:jmap:core', 'urn:ietf:params:jmap:mail'],
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
    return {'id': mid, 'state': written['newState']}


def restored(jmap_port, checkpoint):
    method, changes = request(jmap_port, 'Email/changes', {'sinceState': checkpoint['state']})
    assert method == 'Email/changes' and checkpoint['id'] in changes['updated'], changes
    method, result = request(jmap_port, 'Email/get', {'ids': [checkpoint['id']]})
    assert method == 'Email/get' and result['list'][0]['keywords'].get('$seen') and result['list'][0]['keywords'].get('$flagged'), result
