"""Live structured MIME composition, submission admission and receipt recovery."""
import base64
import email
import email.policy
import imaplib
import json
import time
import urllib.request


def batch(port, calls):
    payload = {'using': ['urn:ietf:params:jmap:core', 'urn:ietf:params:jmap:mail',
                         'urn:ietf:params:jmap:submission'], 'methodCalls': calls}
    req = urllib.request.Request(f'http://127.0.0.1:{port}/jmap/api', data=json.dumps(payload).encode(),
        headers={'Content-Type': 'application/json', 'Authorization': 'Basic ' +
                 base64.b64encode(b'qa@mail.test:qa-new-password').decode()})
    with urllib.request.urlopen(req, timeout=10) as response:
        return json.load(response)


def qualify(request, port, imap_port, tls):
    method, identity = request(port, 'Identity/get', {})
    assert method == 'Identity/get' and identity['list'][0]['email'] == 'qa@mail.test', identity
    _, boxes = request(port, 'Mailbox/get', {})
    drafts = next(b['id'] for b in boxes['list'] if b['role'] == 'drafts')
    sent = next(b['id'] for b in boxes['list'] if b['role'] == 'sent')
    assert all(b['myRights']['maySubmit'] for b in boxes['list']), boxes
    _, before = request(port, 'Email/get', {'ids': []})
    _, receipts_before = request(port, 'EmailSubmission/get', {})
    obj = {'mailboxIds': {drafts: True}, 'keywords': {'$draft': True},
           'from': [{'name': 'QA', 'email': 'qa@mail.test'}],
           'bcc': [{'email': 'probe@mail.test'}], 'subject': 'JMAP composed café',
           'textBody': [{'partId': 'text', 'type': 'text/plain'}],
           'htmlBody': [{'partId': 'html', 'type': 'text/html'}],
           'bodyValues': {'text': {'value': 'composed hello café'},
                          'html': {'value': '<p>composed hello café</p>'}}}
    # Upload a binary attachment independently of the imported-email checks.
    req = urllib.request.Request(f'http://127.0.0.1:{port}/jmap/upload/primary/', data=b'\x00\x01\xff',
        headers={'Content-Type': 'application/octet-stream', 'Authorization': 'Basic ' +
                 base64.b64encode(b'qa@mail.test:qa-new-password').decode()})
    with urllib.request.urlopen(req, timeout=10) as response:
        blob = json.load(response)['blobId']
    obj['attachments'] = [{'blobId': blob, 'name': 'tiny.bin', 'type': 'application/octet-stream'}]
    with imaplib.IMAP4('localhost', imap_port, timeout=10) as client:
        client.starttls(ssl_context=tls); client.login('qa@mail.test', 'qa-new-password')
        assert client.select('Drafts')[0] == 'OK'
        client.response('EXISTS')
        response = batch(port, [
            ['Email/set', {'accountId': 'primary', 'ifInState': before['state'], 'create': {'draft': obj}}, 'compose'],
            ['EmailSubmission/set', {'accountId': 'primary', 'ifInState': receipts_before['state'],
             'create': {'send': {'emailId': '#draft', 'identityId': 'primary'}},
             'onSuccessUpdateEmail': {'#send': {'mailboxIds': {sent: True}, 'keywords/$draft': None}}}, 'send']])
        composed, submitted = (v[1] for v in response['methodResponses'][:2])
        assert len(response['methodResponses']) == 3 and response['methodResponses'][2][0] == 'Email/set', response
        assert 'draft' in composed.get('created', {}), response
        assert 'send' in submitted.get('created', {}), response
        mid, receipt = composed['created']['draft']['id'], submitted['created']['send']['id']
        assert response['createdIds']['draft'] == mid and response['createdIds']['send'] == receipt, response
        assert submitted['created']['send']['undoStatus'] == 'final', submitted
        client.noop()
        assert client.response('EXISTS')[1][0] is not None, 'composed draft did not notify IMAP'
    # Stale state and malformed success hooks reject before a second send.
    for args, kind in [
        ({'ifInState': receipts_before['state']}, 'stateMismatch'),
        ({'onSuccessDestroyEmail': True}, 'invalidArguments')]:
        method, rejected = request(port, 'EmailSubmission/set', dict(args, create={'again': {'emailId': mid, 'identityId': 'primary'}}))
        assert method == 'error' and rejected['type'] == kind, rejected
    _, foreign = request(port, 'EmailSubmission/get', {'ids': [receipt]}, 'probe@mail.test', 'isolated-test-password')
    assert foreign['notFound'] == [receipt], foreign
    _, foreign = request(port, 'EmailSubmission/set', {'create': {'send': {'emailId': mid, 'identityId': 'primary'}}}, 'probe@mail.test', 'isolated-test-password')
    assert foreign['notCreated']['send']['type'] == 'invalidProperties', foreign
    # The implicit Email/set files Sent and clears the draft keyword.
    _, draft = request(port, 'Email/get', {'ids': [mid]})
    assert not draft['list'][0]['keywords'].get('$draft') and draft['list'][0]['mailboxIds'].get(sent), draft
    received_id = None
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        _, incoming = request(port, 'Email/query', {'filter': {'subject': 'JMAP composed café'}}, 'probe@mail.test', 'isolated-test-password')
        if incoming['ids']:
            assert len(incoming['ids']) == 1, incoming
            received_id = incoming['ids'][0]
            break
        time.sleep(.1)
    assert received_id, 'submitted message did not reach recipient'
    with imaplib.IMAP4('localhost', imap_port, timeout=10) as client:
        client.starttls(ssl_context=tls); client.login('probe@mail.test', 'isolated-test-password')
        assert client.select('INBOX')[0] == 'OK'
        _, matches = client.search(None, 'SUBJECT', '"JMAP composed"')
        assert len(matches[0].split()) == 1, matches
        status, parts = client.fetch(matches[0].split()[0], '(BODY.PEEK[])')
        assert status == 'OK', parts
        raw = next(part[1] for part in parts if isinstance(part, tuple))
        parsed = email.message_from_bytes(raw, policy=email.policy.default)
        assert parsed['Bcc'] is None and parsed['Subject'] == 'JMAP composed café', parsed
        assert parsed.get_body(('plain',)).get_content() == 'composed hello café'
        assert parsed.get_body(('html',)).get_content() == '<p>composed hello café</p>'
        assert next(parsed.iter_attachments()).get_payload(decode=True) == b'\x00\x01\xff'
    return {'id': mid, 'receipt': receipt, 'received': received_id, 'state': submitted['newState']}


def restored(request, port, checkpoint):
    method, receipt = request(port, 'EmailSubmission/get', {'ids': [checkpoint['receipt']]})
    assert method == 'EmailSubmission/get' and receipt['state'] == checkpoint['state'], receipt
    assert receipt['list'][0]['emailId'] == checkpoint['id'] and receipt['list'][0]['deliveryStatus'] is None, receipt
    _, source = request(port, 'Email/get', {'ids': [checkpoint['id']]})
    assert not source['list'][0]['keywords'].get('$draft') and source['list'][0]['hasAttachment'], source
    _, received = request(port, 'Email/get', {'ids': [checkpoint['received']]}, 'probe@mail.test', 'isolated-test-password')
    assert received['list'][0]['subject'] == 'JMAP composed café' and received['list'][0]['hasAttachment'], received
