package smtpd

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/bounce"
	"github.com/afterdarksys/go-emailservice-ads/internal/compliance"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"github.com/emersion/go-smtp"
	"testing"
)

func TestDSNOptionsAndDurableNotice(t *testing.T) {
	q, s := newPersistenceTestQueue(t)
	q.emergency = make(chan *Message, 4)
	q.bounceGenerator = bounce.NewBounceGenerator("mail.test", "postmaster@mail.test")
	m := &Message{ID: "source", From: "sender@test", To: []string{"rcpt@test"}, Data: []byte("Subject: test\r\n\r\nbody"), DSNRecipients: map[string]smtp.RcptOptions{"rcpt@test": {Notify: []smtp.DSNNotify{smtp.DSNNotifyDelayed}}}}
	if _, _, err := s.Store(&storage.JournalEntry{MessageID: m.ID, From: m.From, To: m.To, Data: m.Data, Tier: "out"}); err != nil {
		t.Fatal(err)
	}
	if m.wantsDSN("rcpt@test", "FAILURE") {
		t.Fatal("NOTIFY selection ignored")
	}
	if err := q.sendDSN(m, "rcpt@test", "DELAY", "delayed"); err != nil {
		t.Fatal(err)
	}
	if err := q.sendDSN(m, "rcpt@test", "DELAY", "delayed"); err != nil {
		t.Fatal(err)
	}
	if len(q.emergency) != 1 {
		t.Fatal("duplicate delay notice")
	}
	notice := <-q.emergency
	reports, err := bounce.ParseReport(notice.Data)
	if err != nil || len(reports) != 1 || reports[0].Action != "delayed" {
		t.Fatal("DSN roundtrip", reports, err)
	}
}

func TestDSNPreservesComplianceHold(t *testing.T) {
	q, s := newPersistenceTestQueue(t)
	q.emergency = make(chan *Message, 4)
	q.bounceGenerator = bounce.NewBounceGenerator("mail.test", "postmaster@mail.test")
	q.platform.Compliance.Rules = []compliance.Rule{{Name: "hold", Domain: "sender.test", Mode: "hold"}}
	m := &Message{ID: "held-notice-source", From: "sender@sender.test", To: []string{"recipient@remote.test"}, Data: []byte("Subject: test\r\n\r\nbody")}
	if _, _, err := s.Store(&storage.JournalEntry{MessageID: m.ID, From: m.From, To: m.To, Data: m.Data, Tier: "out"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := q.sendDSN(m, m.To[0], "DELAY", "delayed"); err != nil {
			t.Fatal(err)
		}
	}
	if len(q.emergency) != 0 || len(s.ListCompliance("sender.test")) != 1 {
		t.Fatal("DSN bypassed or duplicated compliance hold")
	}
}
