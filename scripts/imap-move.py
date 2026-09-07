"""MOVE acceptance against the running, authenticated TLS service."""
import contextlib
import datetime
import imaplib


def qualify(host, port, tls):
    def ok(result):
        assert result[0] == 'OK', result
        return result[1]
    with contextlib.ExitStack() as stack:
        sessions = []
        for _ in range(3):
            c = stack.enter_context(imaplib.IMAP4(host, port, timeout=10))
            ok(c.starttls(ssl_context=tls))
            ok(c.login('qa@mail.test', 'qa-new-password'))
            sessions.append(c)
        writer, source, destination = sessions
        ok(writer.create('MoveSource'))
        ok(writer.create('MoveArchive'))
        date = datetime.datetime(2020, 2, 3, 12, 0, tzinfo=datetime.timezone.utc)
        raw = b'Subject: persistent move\r\n\r\nmove payload\r\n'
        for flag in ('(\\Flagged customer)', '(\\Deleted)', '(\\Seen)'):
            ok(writer.append('MoveSource', flag, date, raw))
        ok(writer.append('MoveArchive', None, None, raw))
        ok(writer.select('MoveSource', readonly=True))
        assert writer.uid('MOVE', '1', 'MoveArchive')[0] == 'NO', 'read-only MOVE accepted'
        ok(writer.select('MoveSource'))
        ok(source.select('MoveSource'))
        ok(destination.select('MoveArchive'))
        destination.response('EXISTS')
        assert writer.uid('MOVE', '1', 'DoesNotExist')[0] == 'NO'
        assert writer.response('TRYCREATE')[1] != [None], 'missing destination omitted TRYCREATE'
        ok(writer.uid('MOVE', '99999', 'MoveArchive'))
        assert writer.response('EXPUNGE')[1] == [None], 'empty selection removed mail'
        ok(writer.uid('MOVE', '1,3', 'MoveArchive'))
        assert writer.response('EXPUNGE')[1] == [b'3', b'1']
        ok(source.noop())
        assert source.response('EXPUNGE')[1] == [b'3', b'1']
        assert ok(source.uid('search', None, 'ALL')) == [b'2'], 'unrelated deleted message removed'
        ok(destination.noop())
        assert destination.response('EXISTS')[1] == [b'3']
        fetched = repr(ok(destination.uid('fetch', '2:3', '(FLAGS INTERNALDATE BODY.PEEK[])')))
        assert 'customer' in fetched and '2020' in fetched and 'move payload' in fetched
        assert 'Deleted' not in fetched, 'MOVE added Deleted'
        # Exercise sequence MOVE and moving within the selected mailbox.
        imaplib.Commands.setdefault('MOVE', ('SELECTED',))
        ok(writer._simple_command('MOVE', '1', 'MoveArchive'))
        assert ok(writer.search(None, 'ALL')) == [b'']
        ok(writer.select('MoveArchive'))
        ok(writer.uid('MOVE', '2', 'MoveArchive'))
        assert ok(writer.uid('search', None, 'ALL')) == [b'1 3 4 5']
        for c in sessions:
            ok(c.close())
        ok(writer.delete('MoveSource'))
