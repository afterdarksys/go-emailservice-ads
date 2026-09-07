"""Live query history and submission receipt lifecycle across restore."""
import pathlib
import runpy
import time


def qualify(request, port):
    batch = runpy.run_path(str(pathlib.Path(__file__).with_name('jmap-compose-send.py')))['batch']
    _, boxes = request(port, 'Mailbox/get', {})
    folder = next(b['id'] for b in boxes['list'] if b['role'] == 'drafts')
    response = batch(port, [
        ['Email/set', {'create': {'draft': {'mailboxIds': {folder: True},
            'from': [{'email': 'qa@mail.test'}], 'to': [{'email': 'probe@mail.test'}],
            'subject': 'JMAP lifecycle'}}}, 'create'],
        ['EmailSubmission/set', {'create': {'send': {'emailId': '#draft', 'identityId': 'primary'}}}, 'send']])
    mid, receipt = response['createdIds']['draft'], response['createdIds']['send']
    receipt_state = response['methodResponses'][1][1]['newState']
    _, email_query = request(port, 'Email/query', {'filter': {'subject': 'JMAP lifecycle'}})
    _, receipt_query = request(port, 'EmailSubmission/query', {'filter': {'emailIds': [mid]}})
    assert email_query['ids'] == [mid] and receipt_query['ids'] == [receipt], (email_query, receipt_query)
    assert email_query['canCalculateChanges'] and receipt_query['canCalculateChanges']
    for kind, anchor, filters, original in [
        ('Email', mid, {'subject': 'JMAP lifecycle'}, email_query),
        ('EmailSubmission', receipt, {'emailIds': [mid]}, receipt_query)]:
        method, page = request(port, kind + '/query', {
            'filter': filters, 'anchor': anchor, 'anchorOffset': -1, 'limit': 1})
        assert method == kind + '/query' and page['ids'] == [anchor] and page['position'] == 0, page
        assert page['queryState'] == original['queryState'], page
        _, page = request(port, kind + '/query', {'filter': filters, 'position': -1})
        assert page['ids'] == [anchor], page
        method, error = request(port, kind + '/query', {'anchor': anchor},
                                'probe@mail.test', 'isolated-test-password')
        assert method == 'error' and error['type'] == 'anchorNotFound', error
    response = batch(port, [['EmailSubmission/set', {'destroy': [receipt],
        'onSuccessDestroyEmail': [receipt]}, 'destroy']])
    assert response['methodResponses'][0][1]['destroyed'] == [receipt], response
    assert response['methodResponses'][1][0] == 'Email/set' and response['methodResponses'][1][1]['destroyed'] == [mid], response
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        _, got = request(port, 'Email/query', {'filter': {'subject': 'JMAP lifecycle'}}, 'probe@mail.test', 'isolated-test-password')
        if got['ids']:
            assert len(got['ids']) == 1, got
            break
        time.sleep(.1)
    else:
        raise AssertionError('deleting email/receipt cancelled accepted delivery')
    checkpoint = {'id': mid, 'receipt': receipt, 'state': receipt_state,
                  'email_query': email_query['queryState'], 'receipt_query': receipt_query['queryState']}
    restored(request, port, checkpoint)
    return checkpoint


def restored(request, port, checkpoint):
    _, result = request(port, 'EmailSubmission/get', {'ids': [checkpoint['receipt']]})
    assert result['notFound'] == [checkpoint['receipt']], result
    method, changes = request(port, 'EmailSubmission/changes', {'sinceState': checkpoint['state']})
    assert method == 'EmailSubmission/changes' and checkpoint['receipt'] in changes['destroyed'], changes
    for kind, token, filters, removed in [
        ('Email', checkpoint['email_query'], {'subject': 'JMAP lifecycle'}, checkpoint['id']),
        ('EmailSubmission', checkpoint['receipt_query'], {'emailIds': [checkpoint['id']]}, checkpoint['receipt'])]:
        method, changes = request(port, kind + '/queryChanges', {'sinceQueryState': token, 'filter': filters})
        assert method == kind + '/queryChanges' and changes['removed'] == [removed] and not changes['added'], changes
        method, error = request(port, kind + '/query', {'anchor': removed, 'filter': filters})
        assert method == 'error' and error['type'] == 'anchorNotFound', error
