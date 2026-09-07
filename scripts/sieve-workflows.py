"""Live multiple delivery, redirect and persistent vacation suppression."""
import pathlib
import smtplib
import time

SCRIPT = '''require ["fileinto", "imap4flags", "copy", "vacation"];
if header :is "Subject" "Sieve live workflows" {
    addflag "\\\\Seen";
    fileinto "SieveA";
    addflag "\\\\Flagged";
    fileinto "SieveB";
    redirect :copy "probe@mail.test";
    vacation :days 7 :subject "Sieve vacation receipt" "Away from the test mailbox";
    stop;
}
'''


def send(smtp_port, tls):
    with smtplib.SMTP('localhost', smtp_port, timeout=10) as client:
        client.starttls(context=tls); client.login('probe@mail.test', 'isolated-test-password')
        assert not client.sendmail('probe@mail.test', ['qa@mail.test'],
            'From: probe@mail.test\r\nTo: qa@mail.test\r\nSubject: Sieve live workflows\r\n\r\nLive Sieve payload\r\n')


def count(request, port, user, password, subject):
    method, result = request(port, 'Email/query', {'filter': {'subject': subject}}, user, password)
    assert method == 'Email/query', result
    return len(result['ids'])


def wait_counts(request, port, copies, replies):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        qa = count(request, port, 'qa@mail.test', 'qa-new-password', 'Sieve live workflows')
        forwards = count(request, port, 'probe@mail.test', 'isolated-test-password', 'Sieve live workflows')
        vacation = count(request, port, 'probe@mail.test', 'isolated-test-password', 'Sieve vacation receipt')
        if (qa, forwards, vacation) == (copies * 2, copies, replies):
            return
        time.sleep(.1)
    raise AssertionError(('unexpected Sieve delivery counts', qa, forwards, vacation))


def qualify(request, port, smtp_port, tls, data_dir):
    folder = pathlib.Path(data_dir) / 'sieve'; folder.mkdir(exist_ok=True)
    (folder / 'qa@mail.test.sieve').write_text(SCRIPT)
    send(smtp_port, tls); wait_counts(request, port, 1, 1)
    send(smtp_port, tls); wait_counts(request, port, 2, 1)
    _, boxes = request(port, 'Mailbox/get', {})
    for name, flagged in [('SieveA', False), ('SieveB', True)]:
        box = next(b['id'] for b in boxes['list'] if b['name'] == name)
        _, query = request(port, 'Email/query', {'filter': {'inMailbox': box}})
        _, emails = request(port, 'Email/get', {'ids': query['ids']})
        assert len(emails['list']) == 2, emails
        assert all(e['keywords'].get('$seen') and bool(e['keywords'].get('$flagged')) == flagged for e in emails['list']), emails


def restored(request, port, smtp_port, tls):
    wait_counts(request, port, 2, 1)
    send(smtp_port, tls)
    wait_counts(request, port, 3, 1)
