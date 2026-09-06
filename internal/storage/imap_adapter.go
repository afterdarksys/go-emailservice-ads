package storage

import (
	"context"
	"fmt"

	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
	"github.com/google/uuid"
)

// IMAPAdapter adapts MessageStore to work with IMAP interface
type IMAPAdapter struct {
	store *MessageStore
}

// NewIMAPAdapter creates a new IMAP adapter for the message store
func NewIMAPAdapter(store *MessageStore) *IMAPAdapter {
	return &IMAPAdapter{
		store: store,
	}
}

// GetMessages retrieves all messages for a user's folder
func (a *IMAPAdapter) GetMessages(ctx context.Context, username, folder string) ([]imap.MessageSummary, error) {
	stored := a.store.ListByStatus("stored", "mailbox")

	var summaries []imap.MessageSummary
	for _, entry := range stored {
		if entry.Metadata["username"] != username || entry.Metadata["mailbox"] != folder {
			continue
		}
		summaries = append(summaries, imap.MessageSummary{
			ID:    entry.MessageID,
			Flags: []string{},
			Size:  int64(len(entry.Data)),
			Date:  entry.CreatedAt,
		})
	}

	return summaries, nil
}

// FetchMessage retrieves the full message data by ID
func (a *IMAPAdapter) FetchMessage(ctx context.Context, msgID string) ([]byte, error) {
	entry, err := a.store.Get(msgID)
	if err != nil {
		return nil, fmt.Errorf("message not found: %w", err)
	}

	return entry.Data, nil
}

// discardMessage removes an orphaned mailbox blob after its associated mailbox
// metadata could not be made durable.
func (a *IMAPAdapter) discardMessage(msgID string) error {
	return a.store.UpdateStatus(msgID, "delivered", "mailbox metadata persistence failed")
}

// StoreMessage stores a new message in a user's folder
func (a *IMAPAdapter) StoreMessage(ctx context.Context, username, folder string, data []byte) (string, error) {
	return a.storeMessageID(ctx, "", username, folder, data)
}
func (a *IMAPAdapter) storeMessageID(ctx context.Context, id, username, folder string, data []byte) (string, error) {
	if id == "" {
		id = generateMessageID()
	} else if e, err := a.store.Get(id); err == nil && e.Status == "stored" {
		return id, nil
	}

	// Create a journal entry for the message
	entry := &JournalEntry{
		MessageID: id,
		Data:      data,
		Tier:      "mailbox",
		Status:    "stored",
		Metadata: map[string]string{
			"username": username,
			"mailbox":  folder,
		},
	}

	msgID, _, err := a.store.Store(entry)
	if err != nil {
		return "", fmt.Errorf("failed to store message: %w", err)
	}

	return msgID, nil
}

// generateMessageID creates a unique message ID for IMAP-stored messages
func generateMessageID() string {
	return uuid.New().String()
}
