package storage

import (
	"context"
	"errors"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"go.uber.org/zap"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEmailMoveDestroyAtomicAndDurable(t *testing.T) {
	ctx := context.Background()
	raw, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	path := filepath.Join(t.TempDir(), "mail.db")
	s, err := NewMailboxStore(NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	boxes, _, err := s.MailboxSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	folders := map[string]string{}
	for _, b := range boxes {
		folders[b.Path] = b.ID
	}
	destination := []string{folders["Sent"]}
	date := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	a, err := s.AppendMessage(ctx, "alice", "INBOX", []byte("Subject: moved\r\n\r\npayload"), []string{`\Flagged`, "custom"}, date)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: destroyed\r\n\r\npayload"))
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := s.AppendMessage(ctx, "alice", "INBOX", []byte("Subject: unrelated\r\n\r\npayload"), []string{`\Deleted`}, date)
	if err != nil {
		t.Fatal(err)
	}
	_, initial, err := s.EmailSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	_, mailboxState, _ := s.MailboxSnapshot(ctx, "alice")
	if _, err = s.db.Exec(`CREATE TRIGGER reject_uid BEFORE UPDATE OF uidnext ON mailbox_state WHEN NEW.mailbox='Sent' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	r, err := s.SetEmails(ctx, "alice", initial, map[string]mailstate.EmailPatch{a: {Mailboxes: &destination, Keywords: mailstate.Patch{Add: []string{`\Seen`}}}}, nil)
	if err != nil || r.NotUpdated[a] != "serverFail" || r.NewState != initial {
		t.Fatal("failed object leaked state", r, err)
	}
	messages, state, err := s.EmailSnapshot(ctx, "alice")
	if err != nil || state != initial || messages[a].Folder != "INBOX" || len(messages[a].Flags) != 2 {
		t.Fatal(messages, state, err)
	}
	_, afterMailbox, _ := s.MailboxSnapshot(ctx, "alice")
	if afterMailbox != mailboxState {
		t.Fatal("mailbox events escaped rollback")
	}
	if _, err = s.db.Exec(`DROP TRIGGER reject_uid`); err != nil {
		t.Fatal(err)
	}
	s.quotaBytes = 1 // Moving existing mail requires no new quota.
	r, err = s.SetEmails(ctx, "alice", initial, map[string]mailstate.EmailPatch{a: {Mailboxes: &destination, Keywords: mailstate.Patch{Add: []string{`\Seen`}}}, b: {}}, []string{b, b, "missing"})
	if err != nil || len(r.Updated) != 1 || len(r.Destroyed) != 1 || r.NotUpdated[b] != "willDestroy" || r.NotDestroyed["missing"] != "notFound" {
		t.Fatal(r, err)
	}
	messages, _, err = s.EmailSnapshot(ctx, "alice")
	if err != nil || messages[a].Folder != "Sent" || messages[a].MailboxID != destination[0] || !messages[a].Date.Equal(date) || len(messages[a].Flags) != 3 {
		t.Fatal(messages, err)
	}
	if _, ok := messages[b]; ok {
		t.Fatal("deleted still visible")
	}
	if _, ok := messages[unrelated]; !ok {
		t.Fatal("unrelated Deleted mail removed")
	}
	if _, err = s.FetchMessage(ctx, a); err != nil {
		t.Fatal("moved payload lost", err)
	}
	assertEmailTombstone(t, raw, b)
	if _, err = s.SetEmails(ctx, "alice", initial, nil, []string{a}); !errors.Is(err, mailstate.ErrStateMismatch) {
		t.Fatal("stale destroy accepted", err)
	}
	// A membership no-op must preserve its UID and state.
	uid := messages[a].UID
	r, err = s.SetEmails(ctx, "alice", r.NewState, map[string]mailstate.EmailPatch{a: {Mailboxes: &destination}}, nil)
	if err != nil || r.OldState != r.NewState {
		t.Fatal(r, err)
	}
	messages, _, _ = s.EmailSnapshot(ctx, "alice")
	if messages[a].UID != uid {
		t.Fatal("no-op reassigned UID")
	}
	other, _, err := s.MailboxSnapshot(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	foreign := []string{other[0].ID}
	r, err = s.SetEmails(ctx, "alice", "", map[string]mailstate.EmailPatch{a: {Mailboxes: &foreign}}, nil)
	if err != nil || r.NotUpdated[a] != "invalidProperties" {
		t.Fatal(r, err)
	}
	r, err = s.SetEmails(ctx, "bob", "", map[string]mailstate.EmailPatch{a: {Mailboxes: &foreign}}, []string{unrelated})
	if err != nil || r.NotUpdated[a] != "notFound" || r.NotDestroyed[unrelated] != "notFound" {
		t.Fatal(r, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewMailboxStore(NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := s.EmailChanges(ctx, "alice", initial, 500)
	if err != nil || len(changes.Updated) != 1 || changes.Updated[0] != a || len(changes.Destroyed) != 1 || changes.Destroyed[0] != b {
		t.Fatal(changes, err)
	}
	changes, err = s.MailboxChanges(ctx, "alice", mailboxState, 500)
	if err != nil || len(changes.Updated) != 2 {
		t.Fatal(changes, err)
	}
}

func TestEmailMutationsConditionalConcurrencyAndPartialFailure(t *testing.T) {
	ctx := context.Background()
	raw, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	s, err := NewMailboxStore(NewIMAPAdapter(raw), filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: one\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: two\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER reject_destroy BEFORE UPDATE OF expunged ON message_flags WHEN NEW.subject='two' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	r, err := s.SetEmails(ctx, "alice", "", nil, []string{a, b})
	if err != nil || len(r.Destroyed) != 1 || r.Destroyed[0] != a || r.NotDestroyed[b] != "serverFail" {
		t.Fatal(r, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER reject_destroy`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, flag := range []string{`\Seen`, `\Flagged`} {
		wg.Add(1)
		go func(flag string) {
			defer wg.Done()
			<-start
			_, e := s.SetEmails(ctx, "alice", r.NewState, map[string]mailstate.EmailPatch{b: {Keywords: mailstate.Patch{Add: []string{flag}}}}, nil)
			results <- e
		}(flag)
	}
	close(start)
	wg.Wait()
	close(results)
	passed, rejected := 0, 0
	for err := range results {
		if err == nil {
			passed++
		} else if errors.Is(err, mailstate.ErrStateMismatch) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || rejected != 1 {
		t.Fatal(passed, rejected)
	}
}

func TestEmailDestroyRecoversInterruptedPayloadCleanup(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	raw, err := NewMessageStore(filepath.Join(root, "spool"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { raw.Close() }()
	path := filepath.Join(root, "mail.db")
	s, err := NewMailboxStore(NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	id, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: cleanup\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate unavailable payload storage after SQLite remains writable.
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := s.SetEmails(ctx, "alice", "", nil, []string{id})
	if err != nil || len(r.Destroyed) != 1 {
		t.Fatal("committed deletion was reported as failed", r, err)
	}
	messages, _, err := s.EmailSnapshot(ctx, "alice")
	if err != nil || len(messages) != 0 {
		t.Fatal("deleted message exposed", messages, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err = NewMessageStore(filepath.Join(root, "spool"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	s, err = NewMailboxStore(NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	assertEmailTombstone(t, raw, id)
	changes, err := s.EmailChanges(ctx, "alice", r.OldState, 500)
	if err != nil || len(changes.Destroyed) != 1 || changes.Destroyed[0] != id {
		t.Fatal(changes, err)
	}
}

func assertEmailTombstone(t *testing.T, raw *MessageStore, id string) {
	t.Helper()
	if _, err := raw.Get(id); err == nil {
		t.Fatal("deleted payload remains indexed")
	}
	entries, err := raw.journal.Replay()
	if err != nil {
		t.Fatal(err)
	}
	status := ""
	for _, entry := range entries {
		if entry.MessageID == id {
			status = entry.Status
		}
	}
	if status != "deleted" {
		t.Fatal("missing durable payload tombstone", status)
	}
}
