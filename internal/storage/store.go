package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MessageStore provides persistent storage with deduplication
type MessageStore struct {
	basePath string
	logger   *zap.Logger
	journal  *Journal

	// In-memory index for fast lookups and deduplication
	index      map[string]*JournalEntry // message_id -> entry
	hashIndex  map[string]string        // content_hash -> message_id (for dedup)
	indexMu    sync.RWMutex

	// Dead letter queue
	dlq map[string]*JournalEntry
	dlqMu sync.RWMutex
}

// NewMessageStore creates a new persistent message store
func NewMessageStore(basePath string, logger *zap.Logger) (*MessageStore, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	journal, err := NewJournal(filepath.Join(basePath, "journal"), logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize journal: %w", err)
	}

	store := &MessageStore{
		basePath:  basePath,
		logger:    logger,
		journal:   journal,
		index:     make(map[string]*JournalEntry),
		hashIndex: make(map[string]string),
		dlq:       make(map[string]*JournalEntry),
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
		if entry.Status == "delivered" {
			continue // Skip already delivered messages
		}

		s.index[entry.MessageID] = entry

		// Rebuild hash index for deduplication
		if len(entry.Data) > 0 {
			hash := s.hashContent(entry.Data)
			s.hashIndex[hash] = entry.MessageID
		}

		// Move failed messages to DLQ
		if entry.Status == "failed" {
			s.dlqMu.Lock()
			s.dlq[entry.MessageID] = entry
			s.dlqMu.Unlock()
		}

		recovered++
	}

	s.logger.Info("Message store recovered", zap.Int("messages", recovered))
	return nil
}

// Store persists a message and checks for duplicates
func (s *MessageStore) Store(entry *JournalEntry) (string, bool, error) {
	// Check for duplicate by content hash
	hash := s.hashContent(entry.Data)

	s.indexMu.Lock()
	if existingID, exists := s.hashIndex[hash]; exists {
		s.indexMu.Unlock()
		s.logger.Debug("Duplicate message detected",
			zap.String("existing_id", existingID),
			zap.String("hash", hash))
		return existingID, true, nil
	}

	// Store in journal first (WAL pattern)
	entry.Status = "pending"
	entry.CreatedAt = time.Now()

	if err := s.journal.Write(entry); err != nil {
		s.indexMu.Unlock()
		return "", false, fmt.Errorf("failed to journal message: %w", err)
	}

	// Update in-memory index
	s.index[entry.MessageID] = entry
	s.hashIndex[hash] = entry.MessageID
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
	}

	// Remove from index if delivered
	if status == "delivered" {
		delete(s.index, messageID)
		// Clean up hash index
		hash := s.hashContent(entry.Data)
		delete(s.hashIndex, hash)
	}

	s.indexMu.Unlock()
	return nil
}

// ListPending returns all pending messages for a tier
func (s *MessageStore) ListPending(tier string) []*JournalEntry {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()

	var pending []*JournalEntry
	for _, entry := range s.index {
		if tier == "" || entry.Tier == tier {
			if entry.Status == "pending" {
				pending = append(pending, entry)
			}
		}
	}

	return pending
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
		"pending":   0,
		"processing": 0,
		"dlq":       len(s.dlq),
		"total":     len(s.index),
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

// hashContent generates a SHA256 hash of message content for deduplication
func (s *MessageStore) hashContent(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// writeToTierFile writes message to tier-specific file for efficient bulk recovery
func (s *MessageStore) writeToTierFile(entry *JournalEntry) error {
	tierPath := filepath.Join(s.basePath, "tiers", entry.Tier)
	if err := os.MkdirAll(tierPath, 0755); err != nil {
		return err
	}

	filename := filepath.Join(tierPath, fmt.Sprintf("%s.json", entry.MessageID))
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

// Close gracefully shuts down the store
func (s *MessageStore) Close() error {
	s.logger.Info("Closing message store")
	return s.journal.Close()
}
