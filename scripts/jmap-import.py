"""Live upload/import, selected IMAP arrival and blob persistence qualification."""
import base64
import imaplib
import json
import urllib.error
import urllib.request

RAW = ('From: sender@example.test\r\nSubject: JMAP imported café\r\n'
       'MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary="test-boundary"\r\n\r\n'
       '--test-boundary\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello import\r\n'
       '--test-boundary\r\nContent-Type: application/octet-stream\r\n'
       'Content-Disposition: attachment; filename="tiny.bin"\r\nContent-Transfer-Encoding: base64\r\n\r\nAQID\r\n'
       '--test-boundary--\r\n').encode()


def binary_request(port, path, data=None, user='qa@mail.test', password='qa-new-password'):
    headers = {'Authorization': 'Basic ' + base64.b64encode(f'{user}:{password}'.encode()).decode()}
    if data is not None:
        headers['Content-Type'] = 'message/rfc822'
    req = urllib.request.Request(f'http://127.0.0.1:{port}' + path, data=data, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()


def qualify(request, port, imap_port, tls):
    status, body = binary_request(port, '/jmap/upload/primary/', RAW)
    assert status == 201, (status, body)
    blob = json.loads(body)['blobId']
    assert binary_request(port, f'/jmap/download/primary/{blob}/mail.eml')[1] == RAW
    assert binary_request(port, f'/jmap/download/primary/{blob}/mail.eml', user='probe@mail.test', password='isolated-test-password')[0] == 404
    _, result = request(port, 'Mailbox/set', {'create': {'target': {'name': 'JMAPImported'}}})
    folder = result['created']['target']['id']
    _, initial = request(port, 'Email/get', {'ids': []})
    with imaplib.IMAP4('localhost', imap_port, timeout=10) as client:
        client.starttls(ssl_context=tls); client.login('qa@mail.test', 'qa-new-password')
        assert client.select('JMAPImported')[0] == 'OK'
        client.response('EXISTS')
        item = {'blobId': blob, 'mailboxIds': {folder: True}, 'keywords': {'$seen': True, 'customer': True}, 'receivedAt': '2020-01-02T00:00:00Z'}
        method, imported = request(port, 'Email/import', {'ifInState': initial['state'], 'emails': {'mail': item}})
        assert method == 'Email/import' and 'mail' in imported['created'], imported
        mid = imported['created']['mail']['id']
        assert imported['created']['mail']['size'] == len(RAW), imported
        client.noop()
        assert client.response('EXISTS')[1] == [b'1'], 'import arrival not delivered'
        result, fetched = client.fetch('1', '(FLAGS INTERNALDATE BODY.PEEK[])')
        assert result == 'OK' and any(isinstance(part, tuple) and part[1] == RAW for part in fetched), fetched
        assert 'Seen' in repr(fetched) and 'customer' in repr(fetched) and '2020' in repr(fetched), fetched
        method, stale = request(port, 'Email/import', {'ifInState': initial['state'], 'emails': {'stale': item}})
        assert method == 'error' and stale['type'] == 'stateMismatch', stale
    _, foreign_boxes = request(port, 'Mailbox/get', {}, 'probe@mail.test', 'isolated-test-password')
    method, foreign = request(port, 'Email/import', {'emails': {'foreign': {'blobId': blob, 'mailboxIds': {foreign_boxes['list'][0]['id']: True}}}}, 'probe@mail.test', 'isolated-test-password')
    assert method == 'Email/import' and foreign['notCreated']['foreign']['type'] == 'invalidProperties', foreign
    method, email = request(port, 'Email/get', {'ids': [mid]})
    assert method == 'Email/get' and email['list'][0]['hasAttachment'], email
    attachment = email['list'][0]['attachments'][0]['blobId']
    assert binary_request(port, f'/jmap/download/primary/{attachment}/tiny.bin')[1] == b'\x01\x02\x03'
    return {'blob': blob, 'id': mid, 'folder': folder, 'state': initial['state']}


def restored(request, port, checkpoint):
    assert binary_request(port, f'/jmap/download/primary/{checkpoint["blob"]}/mail.eml')[1] == RAW, 'uploaded blob missing after restore'
    assert binary_request(port, f'/jmap/download/primary/{checkpoint["id"]}/mail.eml')[1] == RAW, 'imported message changed after restore'
    method, changes = request(port, 'Email/changes', {'sinceState': checkpoint['state']})
    assert method == 'Email/changes' and checkpoint['id'] in changes['created'], changes
    method, imported = request(port, 'Email/import', {'ifInState': changes['newState'], 'emails': {'again': {'blobId': checkpoint['blob'], 'mailboxIds': {checkpoint['folder']: True}}}})
    assert method == 'Email/import' and imported['created']['again']['id'] != checkpoint['id'], imported
