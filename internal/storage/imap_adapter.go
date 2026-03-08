package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
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
	// For now, this is a stub that returns pending messages
	// In a full implementation, this would query user-specific mailboxes
	pending := a.store.ListPending("")

	var summaries []imap.MessageSummary
	for _, entry := range pending {
		// Filter by user if the recipient matches
		// This is simplified - a real implementation would have proper mailbox storage
		summaries = append(summaries, imap.MessageSummary{
			ID:    entry.MessageID,
			Flags: []string{},
			Size:  int64(len(entry.Data)),
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

// StoreMessage stores a new message in a user's folder
func (a *IMAPAdapter) StoreMessage(ctx context.Context, username, folder string, data []byte) (string, error) {
	// Create a journal entry for the message
	entry := &JournalEntry{
		MessageID: generateMessageID(),
		Data:      data,
		Tier:      "user", // User-stored messages use "user" tier
		Status:    "stored",
		// Recipients would need to be parsed from the message data
		// For now, storing with username as recipient
		To: []string{username},
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
