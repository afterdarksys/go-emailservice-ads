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
	if err = os.WriteFile(path, []byte(`require ["fileinto", "imap4flags"]; addflag "  \\Seen   \\Flagged  "; fileinto "Projects";`), 0600); err != nil {
		t.Fatal(err)
	}
	msg := &Message{ID: "sieve1", From: "sender@example.test", Data: []byte("Subject: test\r\n\r\nbody")}
	if err = q.deliverLocal(msg, []string{"alice@example.test"}); err != nil {
		t.Fatal(err)
	}
	messages, err := q.imapStore.GetMessages(q.ctx, "alice@example.test", "Projects")
	if err != nil || len(messages) != 1 || len(messages[0].Flags) != 2 || messages[0].Flags[0] != `\Seen` || messages[0].Flags[1] != `\Flagged` {
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

func TestLocalSieveExplicitAndNamedFlags(t *testing.T) {
	for _, tc := range []struct {
		name, script, folder string
		flags                []string
	}{
		{"named override", `setflag "\\Seen"; setflag "work" "\\Flagged customer"; if hasflag :contains "work" "CUSTOM" { fileinto :flags "${work}" "Projects"; }`, "Projects", []string{`\Flagged`, "customer"}},
		{"empty keep override", `addflag "\\Seen"; keep :flags "";`, "INBOX", nil},
		{"default snapshot", `addflag "\\Seen"; fileinto "Projects"; addflag "\\Flagged";`, "Projects", []string{`\Seen`}},
		{"invalid flags ignored", `keep :flags ["\\Bogus", "\\Recent", "bad*flag", "\\Seen"];`, "INBOX", []string{`\Seen`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			script := `require ["fileinto","imap4flags","variables"]; ` + tc.script
			if err = os.WriteFile(filepath.Join(dir, "alice@example.test.sieve"), []byte(script), 0600); err != nil {
				t.Fatal(err)
			}
			msg := &Message{ID: "compatibility", From: "sender@test", Data: []byte("Subject: flags\r\n\r\nbody")}
			for n := 0; n < 2; n++ {
				if err = q.deliverLocal(msg, []string{"alice@example.test"}); err != nil {
					t.Fatal(err)
				}
			}
			messages, err := q.imapStore.GetMessages(q.ctx, "alice@example.test", tc.folder)
			if err != nil || len(messages) != 1 {
				t.Fatalf("delivery/retry: %+v %v", messages, err)
			}
			if len(messages[0].Flags) != len(tc.flags) {
				t.Fatalf("stored flags %v; want %v", messages[0].Flags, tc.flags)
			}
			for idx, flag := range tc.flags {
				if messages[0].Flags[idx] != flag {
					t.Fatalf("stored flags %v; want %v", messages[0].Flags, tc.flags)
				}
			}
		})
	}
}
