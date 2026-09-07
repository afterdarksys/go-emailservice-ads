package imap

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestUnsupportedMailboxMutationsFailExplicitly(t *testing.T) {
	user := NewUser(zap.NewNop(), &mailboxTestStore{}, "alice")
	if !errors.Is(user.CreateMailbox("Projects"), errMailboxMutationUnsupported) {
		t.Fatal("CREATE must fail explicitly")
	}
	if !errors.Is(user.DeleteMailbox("Projects"), errMailboxMutationUnsupported) {
		t.Fatal("DELETE must fail explicitly")
	}
	if !errors.Is(user.RenameMailbox("Projects", "Archive"), errMailboxMutationUnsupported) {
		t.Fatal("RENAME must fail explicitly")
	}
	mailbox := NewMailbox(zap.NewNop(), &mailboxTestStore{}, "alice", "INBOX")
	if !errors.Is(mailbox.SetSubscribed(true), errMailboxMutationUnsupported) {
		t.Fatal("SUBSCRIBE must fail explicitly")
	}
	if !errors.Is(mailbox.CopyMessages(false, nil, "Archive"), errMailboxMutationUnsupported) {
		t.Fatal("COPY must fail explicitly")
	}
	if !errors.Is(mailbox.MoveMessages(false, nil, "Archive"), errMailboxMutationUnsupported) {
		t.Fatal("MOVE must fail explicitly")
	}
}

type testLiteral struct{ *bytes.Reader }

func (l testLiteral) Len() int { return l.Reader.Len() }

func TestAppendRejectsUnpersistedMetadata(t *testing.T) {
	mailbox := NewMailbox(zap.NewNop(), &mailboxTestStore{}, "alice", "INBOX")
	if !errors.Is(mailbox.CreateMessage([]string{"\\Seen"}, time.Time{}, testLiteral{bytes.NewReader([]byte("body"))}), errMailboxMutationUnsupported) {
		t.Fatal("APPEND flags must fail explicitly")
	}
	if !errors.Is(mailbox.CreateMessage(nil, time.Now(), testLiteral{bytes.NewReader([]byte("body"))}), errMailboxMutationUnsupported) {
		t.Fatal("APPEND internal date must fail explicitly")
	}
}
