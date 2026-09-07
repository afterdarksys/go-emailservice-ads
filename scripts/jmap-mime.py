"""Live MIME tree creation, owned part reuse and independent restored bytes."""
import base64
import json
import urllib.error
import urllib.request


def download(port, blob, user='qa@mail.test', password='qa-new-password'):
    req = urllib.request.Request(f'http://127.0.0.1:{port}/jmap/download/primary/{blob}/part',
        headers={'Authorization': 'Basic ' + base64.b64encode(f'{user}:{password}'.encode()).decode()})
    with urllib.request.urlopen(req, timeout=10) as response:
        return response.read()


def qualify(request, port):
    _, boxes = request(port, 'Mailbox/get', {})
    folder = next(b['id'] for b in boxes['list'] if b['role'] == 'drafts')
    source = {'mailboxIds': {folder: True}, 'subject': 'MIME source',
              'bodyStructure': {'partId': 'text', 'type': 'text/plain'},
              'bodyValues': {'text': {'value': 'reusable café'}}}
    _, result = request(port, 'Email/set', {'create': {'source': source}})
    source_id = result['created']['source']['id']
    _, result = request(port, 'Email/get', {'ids': [source_id]})
    part_blob = result['list'][0]['bodyStructure']['blobId']
    assert download(port, part_blob) == 'reusable café'.encode()
    _, probe_boxes = request(port, 'Mailbox/get', {}, 'probe@mail.test', 'isolated-test-password')
    foreign_obj = {'mailboxIds': {probe_boxes['list'][0]['id']: True},
                   'bodyStructure': {'blobId': part_blob, 'type': 'text/plain'}}
    _, foreign = request(port, 'Email/set', {'create': {'foreign': foreign_obj}},
                         'probe@mail.test', 'isolated-test-password')
    assert foreign['notCreated']['foreign'] == {'type': 'blobNotFound', 'notFound': [part_blob]}, foreign
    for user, password in [('probe@mail.test', 'isolated-test-password')]:
        try:
            download(port, part_blob, user, password)
        except urllib.error.HTTPError as error:
            assert error.code == 404, error
        else:
            raise AssertionError('foreign part download succeeded')
    copy = {'mailboxIds': {folder: True}, 'subject': 'MIME independent copy',
            'bodyStructure': {'type': 'multipart/related', 'subParts': [
                {'type': 'text/html', 'partId': 'html'},
                {'type': 'text/plain', 'charset': 'utf-8', 'blobId': part_blob,
                 'cid': 'reused@example.test', 'disposition': 'attachment', 'name': 'café.txt'}]},
            'bodyValues': {'html': {'value': '<a href="cid:reused@example.test">text</a>'}}}
    _, copied = request(port, 'Email/set', {'create': {'copy': copy}, 'destroy': [source_id]})
    assert copied['destroyed'] == [source_id] and 'copy' in copied['created'], copied
    checkpoint = {'id': copied['created']['copy']['id'], 'deleted_blob': part_blob}
    restored(request, port, checkpoint)
    return checkpoint


def restored(request, port, checkpoint):
    _, result = request(port, 'Email/get', {'ids': [checkpoint['id']]})
    email = result['list'][0]
    assert email['bodyStructure']['type'] == 'multipart/related', email
    part = email['attachments'][0]
    assert part['cid'] == 'reused@example.test' and part['name'] == 'café.txt', part
    assert download(port, part['blobId']) == 'reusable café'.encode()
    try:
        download(port, checkpoint['deleted_blob'])
    except urllib.error.HTTPError as error:
        assert error.code == 404, error
    else:
        raise AssertionError('deleted source blob remains accessible')
