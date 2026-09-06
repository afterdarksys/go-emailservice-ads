package smtpd

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalSievePersistsFlagsAndDefersInvalidScript(t *testing.T) {
	q := newDeliverLocalTestQueue(t)
	q.dataDir = t.TempDir()
	manager, err := policy.NewManager(&policy.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	q.policyManager = manager
	dir := filepath.Join(q.dataDir, "sieve")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "alice@example.test.sieve")
	if err = os.WriteFile(path, []byte(`require ["fileinto", "imap4flags"]; addflag "\\Seen"; fileinto "Projects";`), 0600); err != nil {
		t.Fatal(err)
	}
	msg := &Message{ID: "sieve1", From: "sender@example.test", Data: []byte("Subject: test\r\n\r\nbody")}
	if err = q.deliverLocal(msg, []string{"alice@example.test"}); err != nil {
		t.Fatal(err)
	}
	messages, err := q.imapStore.GetMessages(q.ctx, "alice@example.test", "Projects")
	if err != nil || len(messages) != 1 || len(messages[0].Flags) != 1 || messages[0].Flags[0] != `\Seen` {
		t.Fatalf("lost Sieve result: %+v %v", messages, err)
	}
	if err = q.deliverLocal(msg, []string{"alice@example.test"}); err != nil {
		t.Fatal(err)
	}
	messages, _ = q.imapStore.GetMessages(q.ctx, "alice@example.test", "Projects")
	if len(messages) != 1 {
		t.Fatal("duplicate retry delivery")
	}
	if err = os.WriteFile(path, []byte(`unknown_action;`), 0600); err != nil {
		t.Fatal(err)
	}
	msg.ID = "sieve2"
	if err = q.deliverLocal(msg, []string{"alice@example.test"}); err == nil {
		t.Fatal("invalid script silently bypassed")
	}
	inbox, err := q.imapStore.GetMessages(q.ctx, "alice@example.test", "INBOX")
	if err != nil || len(inbox) != 0 {
		t.Fatal("failed policy delivered to inbox", err)
	}
}
