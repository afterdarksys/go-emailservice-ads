package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/auditlog"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// MessageStore provides persistent storage for delivery transactions and mailbox blobs.
type MessageStore struct {
	threadIDs map[string]string
	audit     *auditlog.Log
	lockFile  *os.File
	limits    Limits
	closed    bool
	closeOnce sync.Once
	closeErr  error
	basePath  string
	logger    *zap.Logger
	journal   *Journal

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

	lockFile, err := acquireLock(filepath.Join(basePath, ".owner.lock"))
	if err != nil {
		return nil, err
	}
	journal, err := NewJournal(filepath.Join(basePath, "journal"), logger)
	if err != nil {
		lockFile.Close()
		return nil, fmt.Errorf("failed to initialize journal: %w", err)
	}

	store := &MessageStore{
		basePath: basePath,
		lockFile: lockFile,
		logger:   logger,
		journal:  journal,
		index:    make(map[string]*JournalEntry),
		dlq:      make(map[string]*JournalEntry),
	}

	// Replay journal for disaster recovery
	store.audit, err = auditlog.Open(filepath.Join(basePath, "audit-chain.jsonl"))
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("audit integrity: %w", err)
	}
	if err := store.recover(); err != nil {
		store.Close()
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
		case "delivered", "deleted":
			delete(s.index, entry.MessageID)
			s.dlqMu.Lock()
			delete(s.dlq, entry.MessageID)
			s.dlqMu.Unlock()
		case "failed":
			s.index[entry.MessageID] = entry
			s.dlqMu.Lock()
			s.dlq[entry.MessageID] = entry
			s.dlqMu.Unlock()
		case "processing", "queued":
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
	return s.StoreWithReceipt(entry, nil)
}

// StoreWithReceipt journals queue acceptance and its receipt in one record batch.
func (s *MessageStore) StoreWithReceipt(entry *JournalEntry, receipts ...*JournalEntry) (string, bool, error) {
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

	if s.closed {
		s.indexMu.Unlock()
		return "", false, fmt.Errorf("store closed")
	}
	if err := s.diskCapacity(int64(len(entry.Data))*3 + 4096); err != nil {
		s.indexMu.Unlock()
		return "", false, err
	}
	if entry.Tier != "mailbox" && entry.Tier != "emergency" && entry.Tier != "jmap_submission" && entry.Tier != "sieve_effect" {
		if err := s.capacity(int64(len(entry.Data))); err != nil {
			s.indexMu.Unlock()
			return "", false, err
		}
	}
	batch := []*JournalEntry{entry}
	seen := map[string]bool{entry.MessageID: true}
	for _, receipt := range receipts {
		if receipt == nil {
			continue
		}
		if receipt.MessageID == "" || seen[receipt.MessageID] {
			s.indexMu.Unlock()
			return "", false, fmt.Errorf("invalid receipt ID")
		}
		if _, ok := s.index[receipt.MessageID]; ok {
			s.indexMu.Unlock()
			return "", false, fmt.Errorf("receipt already exists")
		}
		seen[receipt.MessageID] = true
		batch = append(batch, receipt)
	}
	var err error
	if len(batch) == 1 {
		err = s.journal.Write(entry)
	} else {
		err = s.journal.WriteBatch(batch...)
	}
	if err != nil {
		s.indexMu.Unlock()
		return "", false, fmt.Errorf("failed to journal message: %w", err)
	}

	// Update in-memory index
	for _, record := range batch {
		s.index[record.MessageID] = cloneEntry(record)
	}
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

	return cloneEntry(entry), nil
}

// UpdateStatus updates message status and journals the change
func (s *MessageStore) UpdateStatus(messageID, status string, errorMsg string) error {
	s.indexMu.Lock()
	entry, exists := s.index[messageID]
	if !exists {
		s.indexMu.Unlock()
		return fmt.Errorf("message not found: %s", messageID)
	}

	entry = cloneEntry(entry)
	if entry.Metadata["compliance"] == "true" {
		s.indexMu.Unlock()
		return fmt.Errorf("use compliance controls for preserved records")
	}
	entry.Status = status
	if status == "processing" {
		entry.LastAttempt = time.Now()
		entry.Attempts++
	}
	if errorMsg != "" {
		entry.ErrorMessage = errorMsg
	}

	// Journal the status update
	if err := s.journal.Write(entry); err != nil {
		s.indexMu.Unlock()
		return err
	}

	s.index[messageID] = entry
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
	entry = cloneEntry(entry)
	if entry.Metadata["compliance"] == "true" {
		return fmt.Errorf("compliance evidence envelope is immutable")
	}
	entry.To = append([]string(nil), recipients...)
	if err := s.journal.Write(entry); err != nil {
		return err
	}
	s.index[messageID] = entry
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
				entries = append(entries, cloneEntry(entry))
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
		dlq = append(dlq, cloneEntry(entry))
	}

	return dlq
}

// RetryFromDLQ moves a message from DLQ back to pending
func (s *MessageStore) RetryFromDLQ(messageID string) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	e, ok := s.index[messageID]
	if !ok || e.Status != "failed" {
		return fmt.Errorf("message not in DLQ")
	}
	v := cloneEntry(e)
	v.Status = "pending"
	v.Attempts = 0
	v.ErrorMessage = ""
	if err := s.journal.Write(v); err != nil {
		return err
	}
	s.index[messageID] = v
	s.dlqMu.Lock()
	delete(s.dlq, messageID)
	s.dlqMu.Unlock()
	return nil
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
	s.closeOnce.Do(func() {
		s.indexMu.Lock()
		defer s.indexMu.Unlock()
		s.closed = true
		s.closeErr = s.journal.Close()
		if s.lockFile != nil {
			s.lockFile.Close()
		}
	})
	return s.closeErr
}
