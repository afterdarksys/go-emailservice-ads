package storage

import (
	"context"
	"go.uber.org/zap"
	"path/filepath"
	"testing"
)

func TestThreadUpgradeInvalidatesStateOnlyOnce(t *testing.T) {
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
	if _, err = s.DeliverOnce(ctx, "message", "alice", "INBOX", []byte("Message-ID: <message@example.test>\r\n\r\nbody")); err != nil {
		t.Fatal(err)
	}
	_, emailBefore, _ := s.EmailSnapshot(ctx, "alice")
	_, boxBefore, _ := s.MailboxSnapshot(ctx, "alice")
	if _, err = s.db.Exec("DELETE FROM email_sync_meta WHERE key='thread-identity-v1'"); err != nil {
		t.Fatal(err)
	}
	if err = initMailboxSync(s.db); err != nil {
		t.Fatal(err)
	}
	_, emailAfter, _ := s.EmailSnapshot(ctx, "alice")
	_, boxAfter, _ := s.MailboxSnapshot(ctx, "alice")
	if emailBefore == emailAfter || boxBefore == boxAfter {
		t.Fatal("legacy state not invalidated")
	}
	if err = initMailboxSync(s.db); err != nil {
		t.Fatal(err)
	}
	_, emailAgain, _ := s.EmailSnapshot(ctx, "alice")
	_, boxAgain, _ := s.MailboxSnapshot(ctx, "alice")
	if emailAgain != emailAfter || boxAgain != boxAfter {
		t.Fatal("restart reset current state")
	}
}
