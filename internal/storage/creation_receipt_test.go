package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"go.uber.org/zap"
)

func TestEmailCreationSavepointRollsBackPayloadUIDAndEvents(t *testing.T) {
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
	boxes, _, err := s.MailboxSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, box := range boxes {
		ids[box.Path] = box.ID
	}
	mid, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: old\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	_, before, _ := s.EmailSnapshot(ctx, "alice")
	if _, err = s.db.Exec(`CREATE TRIGGER reject_create_uid BEFORE UPDATE OF uidnext ON mailbox_state WHEN NEW.mailbox='Sent' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	create := map[string]mailstate.EmailCreation{
		"bad":  {MailboxID: ids["Sent"], Data: []byte("Subject: failed\r\n\r\nbody")},
		"good": {MailboxID: ids["INBOX"], Data: []byte("Subject: created\r\n\r\nbody")},
	}
	r, err := s.SetEmailsWithCreates(ctx, "alice", before, create, nil, []string{mid})
	if err != nil || r.NotCreated["bad"] != "serverFail" || len(r.Created) != 1 || len(r.Destroyed) != 1 {
		t.Fatal(r, err)
	}
	emails, _, err := s.EmailSnapshot(ctx, "alice")
	if err != nil || len(emails) != 1 || emails[r.Created["good"].ID].UID != 2 {
		t.Fatal(emails, err)
	}
	if got := len(raw.ListByStatus("stored", "mailbox")); got != 1 {
		t.Fatalf("orphan raw payloads: %d", got)
	}
	changes, err := s.EmailChanges(ctx, "alice", before, 500)
	if err != nil || len(changes.Created) != 1 || len(changes.Destroyed) != 1 {
		t.Fatal(changes, err)
	}
	var next int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM mailbox_state WHERE username='alice' AND mailbox='Sent'`).Scan(&next); err != nil || next != 0 {
		t.Fatal(next, err)
	}
	if _, err = s.SetEmailsWithCreates(ctx, "alice", before, create, nil, nil); err != mailstate.ErrStateMismatch {
		t.Fatal(err)
	}
}

func TestQueueReceiptAtomicRecoveryAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovery", true: "journal failure"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			s, err := NewMessageStore(dir, zap.NewNop())
			if err != nil {
				t.Fatal(err)
			}
			queue := &JournalEntry{MessageID: "queued", Tier: "out", Data: []byte("Subject: send\r\n\r\nbody")}
			receipt := &JournalEntry{MessageID: "receipt", Tier: "jmap_submission", Status: "jmap_submission", Metadata: map[string]string{"username": "alice", "submission": "receipt metadata"}}
			if fail {
				if err = s.journal.Close(); err != nil {
					t.Fatal(err)
				}
			}
			_, _, err = s.StoreWithReceipt(queue, receipt)
			if fail {
				if err == nil {
					t.Fatal("acknowledged journal failure")
				}
				if _, err = s.Get("queued"); err == nil {
					t.Fatal("queue leaked")
				}
				if _, err = s.Get("receipt"); err == nil {
					t.Fatal("receipt leaked")
				}
				s.Close()
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = NewMessageStore(dir, zap.NewNop())
			if err != nil {
				t.Fatal(err)
			}
			if len(s.ListPending("out")) != 1 || len(s.ListByStatus("jmap_submission", "jmap_submission")) != 1 {
				t.Fatal("atomic pair missing after replay")
			}
			if err = s.UpdateStatus("queued", "delivered", ""); err != nil {
				t.Fatal(err)
			}
			s.SetLimits(Limits{MaxMessages: 1})
			if err = s.Health(); err != nil {
				t.Fatal("receipt consumed queue capacity", err)
			}
			if err = s.Compact(0); err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = NewMessageStore(dir, zap.NewNop())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if len(s.ListPending("out")) != 0 || len(s.ListByStatus("jmap_submission", "jmap_submission")) != 1 {
				t.Fatal("delivery removed receipt or replayed mail")
			}
		})
	}
}
