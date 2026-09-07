package smtpd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
)

func sieveWorkflowQueue(t *testing.T, script string) *QueueManager {
	t.Helper()
	q := newDeliverLocalTestQueue(t)
	q.hostname = "mail.test"
	q.dataDir = t.TempDir()
	q.out = make(chan *Message, 16)
	q.metrics = &QueueMetrics{Enqueued: map[QueueTier]int64{}, Processed: map[QueueTier]int64{}, Failed: map[QueueTier]int64{}}
	m, err := policy.NewManager(&policy.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	q.policyManager = m
	if err = os.MkdirAll(filepath.Join(q.dataDir, "sieve"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(q.dataDir, "sieve", "alice@example.test.sieve"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	return q
}
func TestSieveMultipleDeliveryAndRedirectRetry(t *testing.T) {
	q := sieveWorkflowQueue(t, `require ["copy","imap4flags"]; addflag "\\Seen"; fileinto "A"; addflag "\\Flagged"; fileinto "A"; fileinto "B"; redirect :copy "other@example.test";`)
	msg := &Message{ID: "parent", From: "sender@example.test", Data: []byte("From: sender@example.test\r\nTo: alice@example.test\r\nBcc: hidden@example.test\r\n\r\nbody")}
	if err := q.deliverLocal(msg, []string{"alice@example.test"}); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"A", "B"} {
		messages, err := q.imapStore.GetMessages(q.ctx, "alice@example.test", folder)
		if err != nil || len(messages) != 1 || len(messages[0].Flags) != 2 {
			t.Fatal(folder, messages, err)
		}
	}
	queued := q.store.ListByStatus("queued", "out")
	if len(queued) != 1 || bytes.Contains(queued[0].Data, []byte("Bcc:")) || !bytes.Contains(queued[0].Data, []byte("with Sieve")) {
		t.Fatal(queued)
	}
	if err := q.store.UpdateStatus(queued[0].MessageID, "delivered", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(q.dataDir, "sieve", "alice@example.test.sieve"), []byte(`fileinto "Different";`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := q.deliverLocal(msg, []string{"alice@example.test"}); err != nil {
		t.Fatal(err)
	}
	if len(q.store.ListByStatus("queued", "out")) != 0 {
		t.Fatal("redirect repeated after delivered child")
	}
	if messages, _ := q.imapStore.GetMessages(q.ctx, "alice@example.test", "A"); len(messages) != 1 {
		t.Fatal("mailbox delivery repeated", messages)
	}
	if messages, _ := q.imapStore.GetMessages(q.ctx, "alice@example.test", "Different"); len(messages) != 0 {
		t.Fatal("retry used edited script", messages)
	}
}
func TestSieveVacationSuppressionAndLoopGuards(t *testing.T) {
	q := sieveWorkflowQueue(t, `require "vacation"; vacation :days 7 :subject "Away" "Back soon café";`)
	base := "From: sender@example.test\r\nTo: alice@example.test\r\nSubject: hello\r\n"
	for i, extra := range []string{"", "", "Auto-Submitted: auto-replied\r\n", "List-Id: newsletter.test\r\n", "Precedence: bulk\r\n"} {
		msg := &Message{ID: string(rune('a' + i)), From: "sender@example.test", Data: []byte(base + extra + "\r\nbody")}
		if err := q.deliverLocal(msg, []string{"alice@example.test"}); err != nil {
			t.Fatal(err)
		}
	}
	queued := q.store.ListByStatus("queued", "out")
	if len(queued) != 1 || queued[0].From != "" || !bytes.Contains(queued[0].Data, []byte("Auto-Submitted: auto-replied")) {
		t.Fatal(queued)
	}
	if err := q.store.UpdateStatus(queued[0].MessageID, "delivered", ""); err != nil {
		t.Fatal(err)
	}
	if err := q.store.Compact(0); err != nil {
		t.Fatal(err)
	}
	if err := q.deliverLocal(&Message{ID: "later", From: "sender@example.test", Data: []byte(base + "\r\nbody")}, []string{"alice@example.test"}); err != nil {
		t.Fatal(err)
	}
	if len(q.store.ListByStatus("queued", "out")) != 0 {
		t.Fatal("vacation suppression lost after compaction")
	}
	other := sieveWorkflowQueue(t, `redirect "other@example.test";`)
	if err := other.deliverLocal(&Message{ID: "loop", From: "sender@example.test", Data: []byte(strings.Repeat("Received: loop\r\n", 10) + base + "\r\nbody")}, []string{"alice@example.test"}); err == nil {
		t.Fatal("redirect loop accepted")
	}
	if len(other.store.ListByStatus("queued", "out")) != 0 {
		t.Fatal("loop queued")
	}
}

func TestSieveHonorsExistingDeliveryCheckpoint(t *testing.T) {
	q := sieveWorkflowQueue(t, `fileinto "Archive";`)
	msg := &Message{ID: "old-parent", From: "sender@example.test", Data: []byte("Subject: migration\r\n\r\nbody")}
	if _, err := q.imapStore.DeliverOnceWithFlags(q.ctx, msg.ID+"/alice@example.test", "alice@example.test", "Archive", msg.Data, nil); err != nil {
		t.Fatal(err)
	}
	if err := q.deliverLocal(msg, []string{"alice@example.test"}); err != nil {
		t.Fatal(err)
	}
	if messages, _ := q.imapStore.GetMessages(q.ctx, "alice@example.test", "Archive"); len(messages) != 1 {
		t.Fatal("upgrade repeated delivery", messages)
	}
}
