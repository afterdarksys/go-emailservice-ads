package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestStoreAcceptsIdenticalTransactions(t *testing.T) {
	store, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore() error = %v", err)
	}
	defer store.Close()

	firstID, duplicate, err := store.Store(&JournalEntry{
		From: "sender@example.test",
		To:   []string{"first@example.test"},
		Data: []byte("Subject: same\r\n\r\nbody"),
		Tier: "out",
	})
	if err != nil || duplicate || firstID == "" {
		t.Fatalf("first Store() = (%q, duplicate=%v, err=%v), want a new message", firstID, duplicate, err)
	}

	secondID, duplicate, err := store.Store(&JournalEntry{
		From: "sender@example.test",
		To:   []string{"second@example.test"},
		Data: []byte("Subject: same\r\n\r\nbody"),
		Tier: "out",
	})
	if err != nil || duplicate || secondID == "" {
		t.Fatalf("second Store() = (%q, duplicate=%v, err=%v), want a new message", secondID, duplicate, err)
	}
	if firstID == secondID {
		t.Fatalf("identical transactions reused message ID %q", firstID)
	}
	if got := len(store.ListPending("out")); got != 2 {
		t.Fatalf("ListPending(out) returned %d messages, want 2", got)
	}
}

func TestDeliveredMessagesStayDeliveredAfterRecovery(t *testing.T) {
	path := t.TempDir()
	store, err := NewMessageStore(path, zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore() error = %v", err)
	}

	messageID, _, err := store.Store(&JournalEntry{
		MessageID: "delivered-message",
		Data:      []byte("body"),
		Tier:      "out",
	})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	if err := store.UpdateStatus(messageID, "delivered", ""); err != nil {
		t.Fatalf("UpdateStatus(delivered) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	recovered, err := NewMessageStore(path, zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore(recovered) error = %v", err)
	}
	defer recovered.Close()

	if got := len(recovered.ListPending("out")); got != 0 {
		t.Fatalf("recovered pending messages = %d, want 0", got)
	}
	if _, err := recovered.Get(messageID); err == nil {
		t.Fatal("Get() found a delivered message after recovery")
	}
}

func TestMailboxMessagesAreNotPendingDeliveryWork(t *testing.T) {
	store, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore() error = %v", err)
	}
	defer store.Close()

	adapter := NewIMAPAdapter(store)
	messageID, err := adapter.StoreMessage(context.Background(), "alice", "INBOX", []byte("Subject: retained\r\n\r\nbody"))
	if err != nil {
		t.Fatalf("StoreMessage() error = %v", err)
	}
	if got := len(store.ListPending("")); got != 0 {
		t.Fatalf("pending delivery messages = %d, want 0", got)
	}

	messages, err := adapter.GetMessages(context.Background(), "alice", "INBOX")
	if err != nil {
		t.Fatalf("GetMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0].ID != messageID {
		t.Fatalf("GetMessages() = %#v, want mailbox message %q", messages, messageID)
	}
}

func TestRecoveryKeepsValidRecordsBeforeTruncatedJournalTail(t *testing.T) {
	path := t.TempDir()
	store, err := NewMessageStore(path, zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore() error = %v", err)
	}
	messageID, _, err := store.Store(&JournalEntry{
		MessageID: "valid-before-tail",
		Data:      []byte("body"),
		Tier:      "out",
	})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	files, err := filepath.Glob(filepath.Join(path, "journal", "journal-*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("journal files = %v, err = %v", files, err)
	}
	f, err := os.OpenFile(files[0], os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := f.WriteString(`{"incomplete":`); err != nil {
		f.Close()
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close(tail) error = %v", err)
	}

	recovered, err := NewMessageStore(path, zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore(recovered) error = %v", err)
	}
	defer recovered.Close()

	if _, err := recovered.Get(messageID); err != nil {
		t.Fatalf("valid journal prefix was not recovered: %v", err)
	}
}

func TestMessageStorageUsesOwnerOnlyFilePermissions(t *testing.T) {
	path := t.TempDir()
	store, err := NewMessageStore(path, zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore() error = %v", err)
	}
	defer store.Close()

	if _, _, err := store.Store(&JournalEntry{MessageID: "private", Data: []byte("body"), Tier: "out"}); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	journalFiles, err := filepath.Glob(filepath.Join(path, "journal", "journal-*.log"))
	if err != nil || len(journalFiles) != 1 {
		t.Fatalf("journal files = %v, err = %v", journalFiles, err)
	}
	for _, name := range []string{journalFiles[0], filepath.Join(path, "tiers", "out", "private.json")} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", name, err)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Errorf("permissions for %q = %o, want 0600", name, got)
		}
	}
}

func TestMailboxStoreDoesNotAcknowledgeOrphanedMessage(t *testing.T) {
	store, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore() error = %v", err)
	}
	defer store.Close()

	adapter := NewIMAPAdapter(store)
	mailboxStore, err := NewMailboxStore(adapter, filepath.Join(t.TempDir(), "mailbox.db"))
	if err != nil {
		t.Fatalf("NewMailboxStore() error = %v", err)
	}
	if err := mailboxStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := mailboxStore.StoreMessage(context.Background(), "alice", "INBOX", []byte("body")); err == nil {
		t.Fatal("StoreMessage() succeeded after mailbox metadata storage was unavailable")
	}
	messages, err := adapter.GetMessages(context.Background(), "alice", "INBOX")
	if err != nil {
		t.Fatalf("GetMessages() error = %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("orphaned mailbox messages = %#v, want none", messages)
	}
}

func TestMailboxStorePersistsUIDState(t *testing.T) {
	store, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore() error = %v", err)
	}
	defer store.Close()

	mailboxStore, err := NewMailboxStore(NewIMAPAdapter(store), filepath.Join(t.TempDir(), "mailbox.db"))
	if err != nil {
		t.Fatalf("NewMailboxStore() error = %v", err)
	}
	defer mailboxStore.Close()

	ctx := context.Background()
	for _, body := range [][]byte{[]byte("Subject: one\r\n\r\nbody"), []byte("Subject: two\r\n\r\nbody")} {
		if _, err := mailboxStore.StoreMessage(ctx, "alice", "INBOX", body); err != nil {
			t.Fatalf("StoreMessage() error = %v", err)
		}
	}

	messages, err := mailboxStore.GetMessages(ctx, "alice", "INBOX")
	if err != nil {
		t.Fatalf("GetMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].UID != 1 || messages[1].UID != 2 {
		t.Fatalf("message UIDs = %#v, want 1 then 2", messages)
	}
	uidNext, err := mailboxStore.GetUIDNext(ctx, "alice", "INBOX")
	if err != nil || uidNext != 3 {
		t.Fatalf("GetUIDNext() = (%d, %v), want (3, nil)", uidNext, err)
	}
	firstValidity, err := mailboxStore.GetUIDValidity(ctx, "alice", "INBOX")
	if err != nil {
		t.Fatalf("GetUIDValidity() error = %v", err)
	}
	secondValidity, err := mailboxStore.GetUIDValidity(ctx, "alice", "INBOX")
	if err != nil || firstValidity == 0 || firstValidity != secondValidity {
		t.Fatalf("UIDVALIDITY values = (%d, %d, %v), want one stable non-zero value", firstValidity, secondValidity, err)
	}
}
