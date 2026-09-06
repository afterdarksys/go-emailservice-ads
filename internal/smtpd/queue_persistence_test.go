package smtpd

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/delivery"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

func newPersistenceTestQueue(t *testing.T) (*QueueManager, *storage.MessageStore) {
	t.Helper()
	store, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	return &QueueManager{
		logger: zap.NewNop(),
		store:  store,
		out:    make(chan *Message, 4),
		ctx:    context.Background(),
		metrics: &QueueMetrics{
			Enqueued:  make(map[QueueTier]int64),
			Processed: make(map[QueueTier]int64),
			Failed:    make(map[QueueTier]int64),
		},
	}, store
}

func TestEnqueuePersistsIdenticalTransactionsSeparately(t *testing.T) {
	queue, store := newPersistenceTestQueue(t)
	data := []byte("Subject: identical\r\n\r\nbody")

	if err := queue.Enqueue(&Message{From: "sender@example.test", To: []string{"first@example.test"}, Data: data, Tier: TierOut}); err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	if err := queue.Enqueue(&Message{From: "sender@example.test", To: []string{"second@example.test"}, Data: data, Tier: TierOut}); err != nil {
		t.Fatalf("second Enqueue() error = %v", err)
	}

	if got := len(store.ListByStatus("queued", "out")); got != 2 {
		t.Fatalf("pending transactions = %d, want 2", got)
	}
}

func TestRequeueStoredDoesNotPersistSecondTransaction(t *testing.T) {
	queue, store := newPersistenceTestQueue(t)
	messageID, _, err := store.Store(&storage.JournalEntry{
		MessageID: "retry-message",
		From:      "sender@example.test",
		To:        []string{"recipient@example.test"},
		Data:      []byte("body"),
		Tier:      string(TierOut),
	})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	if err := store.UpdateStatus(messageID, "processing", ""); err != nil {
		t.Fatalf("UpdateStatus(processing) error = %v", err)
	}

	msg := &Message{ID: messageID, Tier: TierOut}
	if err := queue.RequeueStored(msg); err != nil {
		t.Fatalf("RequeueStored() error = %v", err)
	}
	if got := <-queue.out; got != msg {
		t.Fatalf("requeued message = %#v, want %#v", got, msg)
	}
	if _, err := store.Get(messageID); err != nil {
		t.Fatalf("original durable transaction missing: %v", err)
	}
}

func TestFinalizeDeliveryKeepsFailedMessagePending(t *testing.T) {
	queue, store := newPersistenceTestQueue(t)
	messageID, _, err := store.Store(&storage.JournalEntry{
		MessageID: "failed-message",
		Data:      []byte("body"),
		Tier:      string(TierOut),
	})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	queue.finalizeDelivery(&Message{ID: messageID, Tier: TierOut}, errors.New("temporary remote failure"))
	if got := len(store.ListPending("out")); got != 1 {
		t.Fatalf("pending messages after failed delivery = %d, want 1", got)
	}
	entry, err := store.Get(messageID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if entry.Status != "pending" {
		t.Fatalf("status after failed delivery = %q, want pending", entry.Status)
	}
}

func TestRecipientOutcomesSelectOnlyTemporaryRecipients(t *testing.T) {
	queue, store := newPersistenceTestQueue(t)
	id, _, err := store.Store(&storage.JournalEntry{MessageID: "mixed", To: []string{"ok@example.test", "temp@example.test", "perm@example.test"}, Data: []byte("body"), Tier: string(TierOut)})
	if err != nil {
		t.Fatal(err)
	}
	message := &Message{ID: id, To: []string{"ok@example.test", "temp@example.test", "perm@example.test"}}
	queue.handleRecipientOutcomes(message, &delivery.DeliveryResult{Recipients: []delivery.RecipientResult{{Recipient: "ok@example.test", Success: true}, {Recipient: "temp@example.test"}, {Recipient: "perm@example.test", IsPermanent: true}}}, message.To)
	entry, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.To) != 1 || message.To[0] != "temp@example.test" {
		t.Fatalf("persisted retry recipients = %#v", entry.To)
	}
}

func TestPermanentRecipientOutcomesDoNotRetry(t *testing.T) {
	result := &delivery.DeliveryResult{Recipients: []delivery.RecipientResult{{Recipient: "perm@example.test", IsPermanent: true}}}
	if resultHasTemporaryRecipient(result) {
		t.Fatal("permanent-only outcome must not retry")
	}
}
