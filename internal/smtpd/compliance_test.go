package smtpd

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/compliance"
	"testing"
	"time"
)

func TestComplianceHoldReleaseAndPreservation(t *testing.T) {
	q, s := newPersistenceTestQueue(t)
	q.platform.Compliance.Rules = []compliance.Rule{{Name: "legal", Domain: "example.test", Mode: "hold", LegalHold: true, Retention: time.Hour}}
	if err := q.Enqueue(&Message{From: "sender@outside.test", To: []string{"user@example.test"}, Tier: TierOut, Data: []byte("Subject: evidence\r\n\r\nbody")}); err != nil {
		t.Fatal(err)
	}
	items := s.ListCompliance("example.test")
	if len(items) != 1 || len(q.out) != 0 {
		t.Fatal("hold escaped")
	}
	id := items[0].MessageID
	if err := q.ReleaseCompliance(id, "officer", "reviewed"); err == nil {
		t.Fatal("legal hold released")
	}
	if err := s.UpdateStatus(id, "deleted", ""); err == nil {
		t.Fatal("generic deletion bypass")
	}
	if ok, _ := s.Transition(id, "compliance", "pending"); ok {
		t.Fatal("generic requeue bypass")
	}
	if err := s.SetComplianceHold(id, "officer", "case closed", false); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCompliance(id, "officer", "delete"); err == nil {
		t.Fatal("retention bypass")
	}
	if err := q.ReleaseCompliance(id, "officer", "approved"); err != nil {
		t.Fatal(err)
	}
	if len(q.out) != 1 {
		t.Fatal("release not queued")
	}
	e, err := s.Get(id)
	if err != nil || e.Status != "compliance_released" || len(e.Data) == 0 {
		t.Fatal("evidence lost", err)
	}
	if err := q.ReleaseCompliance(id, "officer", "again"); err == nil {
		t.Fatal("duplicate release")
	}
}
func TestComplianceCopyBCCAndCombinedHolds(t *testing.T) {
	q, s := newPersistenceTestQueue(t)
	q.platform.Compliance.Rules = []compliance.Rule{{Name: "copy", Domain: "example.test", Mode: "copy"}, {Name: "bcc", Domain: "example.test", Mode: "bcc", BCC: "monitor@audit.test"}}
	m := &Message{From: "sender@outside.test", To: []string{"user@example.test"}, Tier: TierOut, Data: []byte("Subject: test\r\n\r\nbody")}
	if err := q.Enqueue(m); err != nil {
		t.Fatal(err)
	}
	if len(m.To) != 2 || len(q.out) != 1 || len(s.ListCompliance("")) != 1 {
		t.Fatal("copy/BCC not applied")
	}
	q.platform.Compliance.Rules = []compliance.Rule{{Name: "a", Domain: "example.test", Mode: "hold", Retention: time.Hour}, {Name: "b", Domain: "outside.test", Mode: "hold", LegalHold: true}}
	if err := q.Enqueue(&Message{From: m.From, To: []string{"user@example.test"}, Tier: TierOut, Data: m.Data}); err != nil {
		t.Fatal(err)
	}
	items := s.ListCompliance("outside.test")
	if len(items) != 1 || items[0].Metadata["legal_hold"] != "true" || items[0].Metadata["retain_until"] != "" {
		t.Fatal("combined hold weakened")
	}
}

func TestNewDomainHoldInterceptsAlreadyQueuedMessage(t *testing.T) {
	q, s := newPersistenceTestQueue(t)
	m := &Message{From: "sender@outside.test", To: []string{"user@example.test"}, Tier: TierOut, Data: []byte("Subject: queued\r\n\r\nbody")}
	if err := q.Enqueue(m); err != nil {
		t.Fatal(err)
	}
	q.platform.Compliance.Rules = []compliance.Rule{{Name: "new", Domain: "example.test", Mode: "hold"}}
	q.processMessage("out", <-q.out)
	if len(s.ListCompliance("example.test")) != 1 {
		t.Fatal("existing queue bypassed new hold")
	}
	if _, err := s.Get(m.ID); err == nil {
		t.Fatal("normal delivery transaction remains active")
	}
}
