"""Live thread grouping, collapsed queries, SSE reconnect and restore checks."""
import base64
import json
import pathlib
import runpy
import urllib.request


def event(port, token=None):
    headers = {'Authorization': 'Basic ' + base64.b64encode(b'qa@mail.test:qa-new-password').decode()}
    if token:
        headers['Last-Event-ID'] = token
    req = urllib.request.Request(f'http://127.0.0.1:{port}/jmap/events/?types=Email,Thread&closeafter=state&ping=0', headers=headers)
    with urllib.request.urlopen(req, timeout=10) as response:
        lines = response.read().decode().splitlines()
        assert response.headers.get_content_type() == 'text/event-stream'
    token = next(line[4:] for line in lines if line.startswith('id: '))
    data = json.loads(next(line[6:] for line in lines if line.startswith('data: ')))
    assert data['@type'] == 'StateChange' and set(data['changed']['primary']) == {'Email', 'Thread'}, data
    return token, data


def qualify(request, port):
    binary = runpy.run_path(str(pathlib.Path(__file__).with_name('jmap-import.py')))['binary_request']
    _, boxes = request(port, 'Mailbox/set', {'create': {'threads': {'name': 'ThreadQualification'}}})
    folder = boxes['created']['threads']['id']
    ids = []
    for index, headers in enumerate(['Message-ID: <uat-root@mail.test>\r\n', 'Message-ID: <uat-reply@mail.test>\r\nReferences: <uat-root@mail.test>\r\n']):
        raw = (headers + 'Subject: threaded UAT\r\n\r\nbody\r\n').encode()
        status, data = binary(port, '/jmap/upload/primary/', raw)
        assert status == 201, data
        blob = json.loads(data)['blobId']
        _, created = request(port, 'Email/import', {'emails': {'mail': {'blobId': blob, 'mailboxIds': {folder: True}, 'receivedAt': f'202{index}-01-01T00:00:00Z'}}})
        ids.append(created['created']['mail']['id'])
    _, emails = request(port, 'Email/get', {'ids': ids})
    thread_ids = {mail['threadId'] for mail in emails['list']}
    assert len(thread_ids) == 1, emails
    thread = thread_ids.pop()
    _, before = request(port, 'Thread/get', {'ids': [thread]})
    assert before['list'][0]['emailIds'] == ids, before
    _, collapsed = request(port, 'Email/query', {'filter': {'inMailbox': folder}, 'collapseThreads': True})
    assert collapsed['ids'] == [ids[1]] and collapsed['total'] == 1, collapsed
    old_event, _ = event(port)
    request(port, 'Email/set', {'destroy': [ids[0]]})
    new_event, _ = event(port, old_event)
    assert new_event != old_event
    _, changed = request(port, 'Thread/changes', {'sinceState': before['state']})
    assert thread in changed['updated'], changed
    _, foreign = request(port, 'Thread/get', {'ids': [thread]}, 'probe@mail.test', 'isolated-test-password')
    assert foreign['notFound'] == [thread], foreign
    return {'thread': thread, 'email': ids[1]}


def restored(request, port, checkpoint):
    _, threads = request(port, 'Thread/get', {'ids': [checkpoint['thread']]})
    assert threads['list'][0]['emailIds'] == [checkpoint['email']], threads
