package smtpd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"go.uber.org/zap"
)

func TestJMAPSubmitUsesSMTPAdmissionAndStripsBcc(t *testing.T) {
	q, store := newPersistenceTestQueue(t)
	q.intQ = make(chan *Message, 4)
	v := auth.NewValidator(zap.NewNop())
	if err := v.GetUserStore().AddUser("alice", "test-password-123", "alice@example.test"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Server.Domain = "hub.example.test"
	cfg.Server.LocalDomains = []string{"example.test"}
	cfg.Server.MaxMessageBytes = 1024
	cfg.Server.MaxRecipients = 10
	cfg.Platform.ValidateRecipients = true
	srv := NewServerWithValidator(cfg, zap.NewNop(), q, nil, v)
	ctx := context.Background()
	data := []byte("From: alice@example.test\r\nBcc: bob@remote.test\r\nSubject: private\r\n\r\nbody")
	for _, tc := range []struct {
		user, from, to string
		data           []byte
	}{
		{"alice", "impostor@example.test", "bob@remote.test", data},
		{"alice", "alice@example.test", "missing@example.test", data},
		{"unknown", "alice@example.test", "bob@remote.test", data},
		{"alice", "alice@example.test", "bob@remote.test", []byte("From: impostor@example.test\r\n\r\nbody")},
		{"alice", "alice@example.test", "bob@remote.test", []byte(strings.Repeat("x", 1025))},
	} {
		if _, err := srv.Submit(ctx, tc.user, "127.0.0.1", "draft", tc.from, []string{tc.to}, tc.data); err == nil {
			t.Fatal("accepted invalid submission", tc)
		}
	}
	if records, err := srv.Submissions(ctx, "alice"); err != nil || len(records) != 0 {
		t.Fatal(records, err)
	}
	r, err := srv.Submit(ctx, "alice", "127.0.0.1", "draft", "alice@example.test", []string{"bob@remote.test"}, data)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.EmailID != "draft" || r.UndoStatus != "final" || r.DeliveryStatus != nil {
		t.Fatal(r)
	}
	queued := store.ListByStatus("queued", "int")
	if len(queued) != 1 || strings.Contains(strings.ToLower(string(queued[0].Data)), "bcc:") || !strings.Contains(string(queued[0].Data), "body") {
		t.Fatal(queued)
	}
	if records, err := srv.Submissions(ctx, "alice"); err != nil || len(records) != 1 || records[0].ID != r.ID {
		t.Fatal(records, err)
	}
	if records, err := srv.Submissions(ctx, "bob"); err != nil || len(records) != 0 {
		t.Fatal(records, err)
	}
	if err = store.UpdateStatus(queued[0].MessageID, "delivered", ""); err != nil {
		t.Fatal(err)
	}
	if records, err := srv.Submissions(ctx, "alice"); err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.Submit(ctx, "alice", "127.0.0.1", "draft", "alice@example.test", []string{"bob@remote.test"}, data); err == nil {
		t.Fatal("acknowledged failed durable acceptance")
	}
}

func TestJMAPSubmitHonorsRejectAndDurableDiscard(t *testing.T) {
	q, store := newPersistenceTestQueue(t)
	v := auth.NewValidator(zap.NewNop())
	if err := v.GetUserStore().AddUser("alice", "test-password-123", "alice@example.test"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Server.Domain = "example.test"
	cfg.Server.MaxRecipients = 10
	manager, err := policy.NewManager(&policy.ManagerConfig{Logger: zap.NewNop()})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServerWithValidator(cfg, zap.NewNop(), q, manager, v)
	for _, script := range []string{`reject("blocked")`, `discard()`} {
		if err = manager.AddPolicy(&policy.PolicyConfig{Name: script, Type: policy.PolicyTypeStarlark, Enabled: true, Script: script, Scope: policy.PolicyScope{Type: "global"}, MaxExecutionTime: time.Second}); err != nil {
			t.Fatal(err)
		}
		r, err := srv.Submit(context.Background(), "alice", "127.0.0.1", "draft", "alice@example.test", []string{"bob@remote.test"}, []byte("From: alice@example.test\r\nSubject: policy\r\n\r\nbody"))
		if script == `reject("blocked")` {
			if err == nil {
				t.Fatal("policy rejection bypassed")
			}
		} else if err != nil || r.ID == "" {
			t.Fatal("discard acceptance lost", r, err)
		}
		if err = manager.RemovePolicy(script); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.ListByStatus("queued", "")) != 0 || len(store.ListByStatus("pending", "")) != 0 {
		t.Fatal("policy-rejected/discarded mail queued")
	}
	if records, err := srv.Submissions(context.Background(), "alice"); err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
}

func TestScheduledSubmissionUsesAdmissionAndCanCancel(t *testing.T) {
	q, store := newPersistenceTestQueue(t)
	q.intQ = make(chan *Message, 4)
	v := auth.NewValidator(zap.NewNop())
	if err := v.GetUserStore().AddUser("alice", "test-password-123", "alice@example.test"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Server.Domain = "hub.example.test"
	cfg.Server.MaxMessageBytes = 1024
	srv := NewServerWithValidator(cfg, zap.NewNop(), q, nil, v)
	ctx := context.Background()
	sub, err := srv.SubmitAt(ctx, "alice", "127.0.0.1", "draft", "alice@example.test", []string{"bob@remote.test"}, []byte("From: alice@example.test\r\nBcc: bob@remote.test\r\n\r\nbody"), time.Now().Add(time.Hour))
	if err != nil || sub.UndoStatus != "pending" {
		t.Fatal(sub, err)
	}
	if len(q.intQ) != 0 || len(store.ListByStatus("scheduled", "")) != 1 {
		t.Fatal("scheduled message dispatched early")
	}
	if err = srv.DestroySubmission(ctx, "alice", sub.ID); err == nil {
		t.Fatal("deleted pending receipt")
	}
	if err = srv.CancelSubmission(ctx, "alice", sub.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.ListByStatus("scheduled", "")) != 0 {
		t.Fatal("cancellation retained active payload")
	}
	records, err := srv.Submissions(ctx, "alice")
	if err != nil || len(records) != 1 || records[0].UndoStatus != "canceled" {
		t.Fatal(records, err)
	}
}
