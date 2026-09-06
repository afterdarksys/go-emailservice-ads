package smtpd

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/bounce"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	"testing"
)

func TestExhaustedRetriesGenerateFinalNotification(t *testing.T) {
	q, s := newPersistenceTestQueue(t)
	q.emergency = make(chan *Message, 4)
	q.bounceGenerator = bounce.NewBounceGenerator("mail.test", "postmaster@mail.test")
	id, _, err := s.Store(&storage.JournalEntry{From: "sender@mail.test", To: []string{"recipient@remote.test"}, Data: []byte("Subject: hello\r\n\r\nbody"), Status: "pending", Tier: "out", Attempts: 5})
	if err != nil {
		t.Fatal(err)
	}
	rs := NewRetryScheduler(s, q, DefaultRetryPolicy(), zap.NewNop())
	defer rs.Shutdown()
	rs.processRetries()
	e, err := s.Get(id)
	if err != nil || e.Status != "failed" {
		t.Fatal("not moved to failed", err)
	}
	if len(q.emergency) != 1 {
		t.Fatal("final DSN missing")
	}
	dsn := <-q.emergency
	if !dsn.IsBounce || dsn.From != "" || dsn.To[0] != "sender@mail.test" {
		t.Fatal("invalid DSN envelope")
	}
	rs.processRetries()
	if len(q.emergency) != 0 {
		t.Fatal("completed failure notified twice")
	}
}
