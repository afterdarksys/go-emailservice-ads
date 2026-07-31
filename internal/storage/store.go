package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// MessageStore provides persistent storage for delivery transactions and mailbox blobs.
type MessageStore struct {
	basePath string
	logger   *zap.Logger
	journal  *Journal

	// In-memory index for fast lookups.
	index   map[string]*JournalEntry // message_id -> entry
	indexMu sync.RWMutex

	// Dead letter queue
	dlq   map[string]*JournalEntry
	dlqMu sync.RWMutex
}

// NewMessageStore creates a new persistent message store
func NewMessageStore(basePath string, logger *zap.Logger) (*MessageStore, error) {
	if err := os.MkdirAll(basePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	journal, err := NewJournal(filepath.Join(basePath, "journal"), logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize journal: %w", err)
	}

	store := &MessageStore{
		basePath: basePath,
		logger:   logger,
		journal:  journal,
		index:    make(map[string]*JournalEntry),
		dlq:      make(map[string]*JournalEntry),
	}

	// Replay journal for disaster recovery
	if err := store.recover(); err != nil {
		return nil, fmt.Errorf("failed to recover from journal: %w", err)
	}

	return store, nil
}

// recover replays the journal and rebuilds the in-memory index
func (s *MessageStore) recover() error {
	entries, err := s.journal.Replay()
	if err != nil {
		return err
	}

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	recovered := 0
	for _, entry := range entries {
		if entry.MessageID == "" {
			s.logger.Warn("Skipping journal entry without message ID", zap.String("journal_id", entry.ID))
			continue
		}

		// The journal is an append-only state log. Apply records in order so the
		// final record for a message wins; a delivered record is a tombstone for
		// any earlier pending record.
		switch entry.Status {
		case "delivered":
			delete(s.index, entry.MessageID)
			s.dlqMu.Lock()
			delete(s.dlq, entry.MessageID)
			s.dlqMu.Unlock()
		case "failed":
			s.index[entry.MessageID] = entry
			s.dlqMu.Lock()
			s.dlq[entry.MessageID] = entry
			s.dlqMu.Unlock()
		case "processing":
			// A worker may have crashed after claiming this message. Requeue it
			// for at-least-once delivery rather than stranding it indefinitely.
			entry.Status = "pending"
			s.index[entry.MessageID] = entry
		default:
			s.index[entry.MessageID] = entry
		}

		recovered++
	}

	s.logger.Info("Message store recovered", zap.Int("messages", recovered))
	return nil
}

// Store persists a distinct accepted message transaction. Content-addressable
// blobs may be introduced below this layer, but matching message bytes do not
// make separate SMTP transactions duplicates.
func (s *MessageStore) Store(entry *JournalEntry) (string, bool, error) {
	if entry.MessageID == "" {
		entry.MessageID = uuid.NewString()
	}
	if entry.Status == "" {
		entry.Status = "pending"
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	s.indexMu.Lock()
	if _, exists := s.index[entry.MessageID]; exists {
		s.indexMu.Unlock()
		return "", false, fmt.Errorf("message ID already exists: %s", entry.MessageID)
	}

	// Store in journal first (WAL pattern)
	if err := s.journal.Write(entry); err != nil {
		s.indexMu.Unlock()
		return "", false, fmt.Errorf("failed to journal message: %w", err)
	}

	// Update in-memory index
	s.index[entry.MessageID] = entry
	s.indexMu.Unlock()

	// Write to tier-specific storage file for efficient recovery
	if err := s.writeToTierFile(entry); err != nil {
		s.logger.Error("Failed to write to tier file", zap.Error(err))
	}

	return entry.MessageID, false, nil
}

// Get retrieves a message by ID
func (s *MessageStore) Get(messageID string) (*JournalEntry, error) {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()

	entry, exists := s.index[messageID]
	if !exists {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}

	return entry, nil
}

// UpdateStatus updates message status and journals the change
func (s *MessageStore) UpdateStatus(messageID, status string, errorMsg string) error {
	s.indexMu.Lock()
	entry, exists := s.index[messageID]
	if !exists {
		s.indexMu.Unlock()
		return fmt.Errorf("message not found: %s", messageID)
	}

	entry.Status = status
	entry.LastAttempt = time.Now()
	entry.Attempts++
	if errorMsg != "" {
		entry.ErrorMessage = errorMsg
	}

	// Journal the status update
	if err := s.journal.Write(entry); err != nil {
		s.indexMu.Unlock()
		return err
	}

	// Move to DLQ if permanently failed
	if status == "failed" {
		s.dlqMu.Lock()
		s.dlq[messageID] = entry
		s.dlqMu.Unlock()
	} else {
		s.dlqMu.Lock()
		delete(s.dlq, messageID)
		s.dlqMu.Unlock()
	}

	// Remove from index if delivered
	if status == "delivered" {
		delete(s.index, messageID)
	}

	s.indexMu.Unlock()
	return nil
}

// UpdateRecipients durably replaces the envelope recipients for a queued
// transaction. It is used after mixed-recipient delivery so retries target only
// the recipients that still need an attempt.
func (s *MessageStore) UpdateRecipients(messageID string, recipients []string) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	entry, exists := s.index[messageID]
	if !exists {
		return fmt.Errorf("message not found: %s", messageID)
	}
	entry.To = append([]string(nil), recipients...)
	if err := s.journal.Write(entry); err != nil {
		return err
	}
	return nil
}

// ListPending returns all pending messages for a tier
func (s *MessageStore) ListPending(tier string) []*JournalEntry {
	return s.ListByStatus("pending", tier)
}

// ListByStatus returns entries with a requested state, optionally scoped to a
// tier. Mailbox blobs are deliberately not pending delivery work.
func (s *MessageStore) ListByStatus(status, tier string) []*JournalEntry {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()

	var entries []*JournalEntry
	for _, entry := range s.index {
		if tier == "" || entry.Tier == tier {
			if entry.Status == status {
				entries = append(entries, entry)
			}
		}
	}

	return entries
}

// GetDLQ returns all messages in the dead letter queue
func (s *MessageStore) GetDLQ() []*JournalEntry {
	s.dlqMu.RLock()
	defer s.dlqMu.RUnlock()

	dlq := make([]*JournalEntry, 0, len(s.dlq))
	for _, entry := range s.dlq {
		dlq = append(dlq, entry)
	}

	return dlq
}

// RetryFromDLQ moves a message from DLQ back to pending
func (s *MessageStore) RetryFromDLQ(messageID string) error {
	s.dlqMu.Lock()
	entry, exists := s.dlq[messageID]
	if !exists {
		s.dlqMu.Unlock()
		return fmt.Errorf("message not in DLQ: %s", messageID)
	}

	delete(s.dlq, messageID)
	s.dlqMu.Unlock()

	// Reset for retry
	entry.Status = "pending"
	entry.ErrorMessage = ""

	s.indexMu.Lock()
	s.index[messageID] = entry
	s.indexMu.Unlock()

	return s.journal.Write(entry)
}

// Stats returns storage statistics
func (s *MessageStore) Stats() map[string]int {
	s.indexMu.RLock()
	s.dlqMu.RLock()
	defer s.indexMu.RUnlock()
	defer s.dlqMu.RUnlock()

	stats := map[string]int{
		"pending":    0,
		"processing": 0,
		"dlq":        len(s.dlq),
		"total":      len(s.index),
	}

	for _, entry := range s.index {
		if entry.Status == "pending" {
			stats["pending"]++
		} else if entry.Status == "processing" {
			stats["processing"]++
		}
	}

	return stats
}

// writeToTierFile writes message to tier-specific file for efficient bulk recovery
func (s *MessageStore) writeToTierFile(entry *JournalEntry) error {
	tierPath := filepath.Join(s.basePath, "tiers", entry.Tier)
	if err := os.MkdirAll(tierPath, 0700); err != nil {
		return err
	}

	filename := filepath.Join(tierPath, fmt.Sprintf("%s.json", entry.MessageID))
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0600)
}

// Close gracefully shuts down the store
func (s *MessageStore) Close() error {
	s.logger.Info("Closing message store")
	return s.journal.Close()
}
