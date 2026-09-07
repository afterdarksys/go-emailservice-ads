"""Delayed release, cancellation, and restored pending-submission qualification."""
import time


def qualify(request, port):
    _, boxes = request(port, 'Mailbox/get', {})
    folder = next(b['id'] for b in boxes['list'] if b['role'] == 'drafts')

    def schedule(label, seconds):
        _, draft = request(port, 'Email/set', {'create': {'draft': {
            'mailboxIds': {folder: True}, 'from': [{'email': 'qa@mail.test'}],
            'to': [{'email': 'probe@mail.test'}], 'subject': label}}})
        mid = draft['created']['draft']['id']
        _, sent = request(port, 'EmailSubmission/set', {'create': {'send': {
            'emailId': mid, 'identityId': 'primary', 'envelope': {
                'mailFrom': {'email': 'qa@mail.test', 'parameters': {'HOLDFOR': str(seconds)}},
                'rcptTo': [{'email': 'probe@mail.test'}]}}}})
        assert sent['created']['send']['undoStatus'] == 'pending', sent
        return sent['created']['send']['id'], mid

    canceled, _ = schedule('scheduled-canceled', 3600)
    _, foreign = request(port, 'EmailSubmission/set', {'update': {canceled: {'undoStatus': 'canceled'}}},
                         'probe@mail.test', 'isolated-test-password')
    assert foreign['notUpdated'][canceled]['type'] == 'notFound', foreign
    _, deleted = request(port, 'EmailSubmission/set', {'destroy': [canceled]})
    assert canceled in deleted['notDestroyed'], deleted
    _, result = request(port, 'EmailSubmission/set', {'update': {canceled: {'undoStatus': 'canceled'}}})
    assert canceled in result['updated'], result
    released, _ = schedule('scheduled-released', 2)
    deadline = time.monotonic() + 50
    while time.monotonic() < deadline:
        _, got = request(port, 'Email/query', {'filter': {'subject': 'scheduled-released'}},
                         'probe@mail.test', 'isolated-test-password')
        if got['ids']:
            assert len(got['ids']) == 1, got
            break
        time.sleep(.2)
    else:
        raise AssertionError('scheduled message was not released')
    _, result = request(port, 'EmailSubmission/set', {'update': {released: {'undoStatus': 'canceled'}}})
    assert result['notUpdated'][released]['type'] == 'cannotUnsend', result
    pending, mid = schedule('scheduled-after-restore', 3600)
    _, deleted = request(port, 'Email/set', {'destroy': [mid]})
    assert deleted['destroyed'] == [mid], deleted
    return {'pending': pending, 'canceled': canceled}


def restored(request, port, checkpoint):
    _, got = request(port, 'EmailSubmission/get', {'ids': [checkpoint['pending'], checkpoint['canceled']]})
    records = {r['id']: r for r in got['list']}
    assert records[checkpoint['pending']]['undoStatus'] == 'pending', records
    assert records[checkpoint['canceled']]['undoStatus'] == 'canceled', records
    _, query = request(port, 'EmailSubmission/query', {'filter': {'undoStatus': 'pending'}})
    _, result = request(port, 'EmailSubmission/set', {'ifInState': got['state'],
        'update': {checkpoint['pending']: {'undoStatus': 'canceled'}}})
    assert checkpoint['pending'] in result['updated'] and result['newState'] != got['state'], result
    _, changes = request(port, 'EmailSubmission/changes', {'sinceState': got['state']})
    assert changes['updated'] == [checkpoint['pending']], changes
    _, changes = request(port, 'EmailSubmission/queryChanges', {'sinceQueryState': query['queryState'],
                                                               'filter': {'undoStatus': 'pending'}})
    assert changes['removed'] == [checkpoint['pending']], changes
    for label in ['scheduled-canceled', 'scheduled-after-restore']:
        _, got = request(port, 'Email/query', {'filter': {'subject': label}},
                         'probe@mail.test', 'isolated-test-password')
        assert got['ids'] == [], got
