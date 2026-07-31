package smtpd

import (
	"context"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

// newDeliverLocalTestQueue builds a QueueManager backed by a real
// MailboxStore, matching what internal/imap.Backend.Login queries.
func newDeliverLocalTestQueue(t *testing.T) *QueueManager {
	t.Helper()
	store, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	adapter := storage.NewIMAPAdapter(store)
	imapStore, err := storage.NewMailboxStore(adapter, filepath.Join(t.TempDir(), "mailbox.db"))
	if err != nil {
		t.Fatalf("NewMailboxStore() error = %v", err)
	}
	t.Cleanup(func() { _ = imapStore.Close() })

	return &QueueManager{
		logger:    zap.NewNop(),
		store:     store,
		imapStore: imapStore,
		ctx:       context.Background(),
	}
}

// Threats: on a shared mailhub, delivery keyed by bare local-part instead of
// the full address collides same-named mailboxes across domains (help@a.com
// and help@b.com both landing in one "help" mailbox — cross-tenant mail
// exposure) and never matches the full-address username IMAP login actually
// authenticates with, making delivered mail unreachable by its real owner.
func TestDeliverLocalUsesFullAddressNotBareLocalPart(t *testing.T) {
	qm := newDeliverLocalTestQueue(t)

	msg := &Message{ID: "m1", Data: []byte("From: sender@example.test\r\nTo: help@gomeow.media\r\n\r\nbody\r\n")}
	if err := qm.deliverLocal(msg, []string{"help@gomeow.media"}); err != nil {
		t.Fatalf("deliverLocal() error = %v", err)
	}

	got, err := qm.imapStore.GetMessages(context.Background(), "help@gomeow.media", "INBOX")
	if err != nil {
		t.Fatalf("GetMessages(help@gomeow.media) error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetMessages(help@gomeow.media) = %d messages, want 1 — delivery must be reachable under the full address IMAP login uses", len(got))
	}
}

func TestDeliverLocalDoesNotCollideAcrossDomains(t *testing.T) {
	qm := newDeliverLocalTestQueue(t)

	if err := qm.deliverLocal(&Message{ID: "m1", Data: []byte("To: help@gomeow.media\r\n\r\nfor gomeow\r\n")}, []string{"help@gomeow.media"}); err != nil {
		t.Fatalf("deliverLocal(gomeow.media) error = %v", err)
	}
	if err := qm.deliverLocal(&Message{ID: "m2", Data: []byte("To: help@brooklyncats.show\r\n\r\nfor brooklyncats\r\n")}, []string{"help@brooklyncats.show"}); err != nil {
		t.Fatalf("deliverLocal(brooklyncats.show) error = %v", err)
	}

	ctx := context.Background()
	gomeow, err := qm.imapStore.GetMessages(ctx, "help@gomeow.media", "INBOX")
	if err != nil {
		t.Fatalf("GetMessages(help@gomeow.media) error = %v", err)
	}
	brooklyn, err := qm.imapStore.GetMessages(ctx, "help@brooklyncats.show", "INBOX")
	if err != nil {
		t.Fatalf("GetMessages(help@brooklyncats.show) error = %v", err)
	}

	if len(gomeow) != 1 {
		t.Fatalf("help@gomeow.media has %d messages, want exactly 1 (not brooklyncats.show's mail)", len(gomeow))
	}
	if len(brooklyn) != 1 {
		t.Fatalf("help@brooklyncats.show has %d messages, want exactly 1 (not gomeow.media's mail)", len(brooklyn))
	}
}
