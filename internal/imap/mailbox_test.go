package imap

import (
	"bytes"
	"context"
	"testing"

	goimap "github.com/emersion/go-imap"
	"go.uber.org/zap"
)

type mailboxTestStore struct {
	messages    []MessageSummary
	uidValidity uint32
	uidNext     uint32
	raw         map[string][]byte
}

func (s *mailboxTestStore) GetMessages(context.Context, string, string) ([]MessageSummary, error) {
	return s.messages, nil
}
func (s *mailboxTestStore) FetchMessage(_ context.Context, id string) ([]byte, error) {
	return s.raw[id], nil
}
func (s *mailboxTestStore) StoreMessage(context.Context, string, string, []byte) (string, error) {
	return "", nil
}

func TestListMessagesReturnsStoredEnvelopeAndBody(t *testing.T) {
	store := &mailboxTestStore{
		messages: []MessageSummary{{ID: "one", UID: 10, Size: 70}},
		raw:      map[string][]byte{"one": []byte("Date: Mon, 02 Jan 2006 15:04:05 -0700\r\nFrom: Alice <alice@example.com>\r\nTo: Bob <bob@example.com>\r\nSubject: Hello\r\nContent-Type: text/plain\r\n\r\nbody")},
	}
	mailbox := NewMailbox(zap.NewNop(), store, "alice", "INBOX")
	ch := make(chan *goimap.Message, 1)
	if err := mailbox.ListMessages(false, nil, []goimap.FetchItem{goimap.FetchEnvelope, goimap.FetchBodyStructure, "BODY[]"}, ch); err != nil {
		t.Fatal(err)
	}
	msg := <-ch
	if msg.Envelope == nil || msg.Envelope.Subject != "Hello" || msg.BodyStructure == nil {
		t.Fatalf("FETCH returned incomplete message: %#v", msg)
	}
	var literal goimap.Literal
	for _, value := range msg.Body {
		literal = value
	}
	if literal == nil {
		t.Fatal("BODY[] literal missing")
	}
	data := make([]byte, literal.Len())
	if _, err := literal.Read(data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("Subject: Hello")) {
		t.Fatalf("BODY[] = %q", data)
	}
}
func (s *mailboxTestStore) GetUIDValidity(context.Context, string, string) (uint32, error) {
	return s.uidValidity, nil
}
func (s *mailboxTestStore) GetUIDNext(context.Context, string, string) (uint32, error) {
	return s.uidNext, nil
}
func (s *mailboxTestStore) AllocateUID(context.Context, string, string) (uint32, error) {
	return 0, nil
}
func (s *mailboxTestStore) UpdateMessageFlags(context.Context, string, string, string, goimap.FlagsOp, []string) error {
	return nil
}
func (s *mailboxTestStore) ExpungeDeleted(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func TestListMessagesHonorsUIDSequenceSet(t *testing.T) {
	store := &mailboxTestStore{messages: []MessageSummary{{ID: "one", UID: 10}, {ID: "two", UID: 20}}}
	mailbox := NewMailbox(zap.NewNop(), store, "alice", "INBOX")
	seqSet := new(goimap.SeqSet)
	seqSet.AddNum(20)
	ch := make(chan *goimap.Message, 2)

	if err := mailbox.ListMessages(true, seqSet, []goimap.FetchItem{goimap.FetchUid}, ch); err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	var got []*goimap.Message
	for msg := range ch {
		got = append(got, msg)
	}
	if len(got) != 1 || got[0].Uid != 20 {
		t.Fatalf("UID FETCH response = %#v, want only UID 20", got)
	}
}

func TestStatusUsesPersistentUIDState(t *testing.T) {
	store := &mailboxTestStore{uidValidity: 42, uidNext: 99}
	mailbox := NewMailbox(zap.NewNop(), store, "alice", "INBOX")

	status, err := mailbox.Status([]goimap.StatusItem{goimap.StatusUidNext, goimap.StatusUidValidity})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.UidValidity != 42 || status.UidNext != 99 {
		t.Fatalf("UID status = (%d, %d), want (42, 99)", status.UidValidity, status.UidNext)
	}
}

func TestIMAPLiteralLimit(t *testing.T) {
	if got := imapLiteralLimit(10 * 1024 * 1024); got != 10*1024*1024 {
		t.Fatalf("imapLiteralLimit() = %d, want configured limit", got)
	}
	if got := imapLiteralLimit(-1); got != 0 {
		t.Fatalf("imapLiteralLimit(-1) = %d, want 0", got)
	}
	if got := imapLiteralLimit(int(^uint32(0)) + 1); got != ^uint32(0) {
		t.Fatalf("imapLiteralLimit(overflow) = %d, want %d", got, ^uint32(0))
	}
}

func TestIMAPTLSMode(t *testing.T) {
	for _, mode := range []string{"", "starttls", "implicit", "disabled"} {
		if _, err := imapTLSMode(mode); err != nil {
			t.Fatalf("imapTLSMode(%q): %v", mode, err)
		}
	}
	if _, err := imapTLSMode("wrong"); err == nil {
		t.Fatal("invalid mode must fail")
	}
}
