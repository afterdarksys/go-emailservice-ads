package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// JournalEntry represents a single message in the journal
type JournalEntry struct {
	ID           string            `json:"id"`
	MessageID    string            `json:"message_id"`
	From         string            `json:"from"`
	To           []string          `json:"to"`
	Data         []byte            `json:"data"`
	Tier         string            `json:"tier"`
	Attempts     int               `json:"attempts"`
	LastAttempt  time.Time         `json:"last_attempt,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	Status       string            `json:"status"` // pending, processing, delivered, failed
	ErrorMessage string            `json:"error_message,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// Journal provides write-ahead logging for message persistence
type Journal struct {
	basePath string
	logger   *zap.Logger
	mu       sync.RWMutex
	file     *os.File
	encoder  *json.Encoder
}

// NewJournal creates a new journal instance
func NewJournal(basePath string, logger *zap.Logger) (*Journal, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create journal directory: %w", err)
	}

	journalFile := filepath.Join(basePath, fmt.Sprintf("journal-%s.log", time.Now().Format("20060102-150405")))
	file, err := os.OpenFile(journalFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open journal file: %w", err)
	}

	j := &Journal{
		basePath: basePath,
		logger:   logger,
		file:     file,
		encoder:  json.NewEncoder(file),
	}

	logger.Info("Journal initialized", zap.String("path", journalFile))
	return j, nil
}

// Write appends an entry to the journal
func (j *Journal) Write(entry *JournalEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	if err := j.encoder.Encode(entry); err != nil {
		return fmt.Errorf("failed to write journal entry: %w", err)
	}

	// Force sync to disk for durability
	if err := j.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync journal: %w", err)
	}

	return nil
}

// Replay reads all journal entries and returns them for recovery
func (j *Journal) Replay() ([]*JournalEntry, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	files, err := filepath.Glob(filepath.Join(j.basePath, "journal-*.log"))
	if err != nil {
		return nil, fmt.Errorf("failed to list journal files: %w", err)
	}

	var entries []*JournalEntry
	for _, file := range files {
		fileEntries, err := j.replayFile(file)
		if err != nil {
			j.logger.Error("Failed to replay journal file", zap.String("file", file), zap.Error(err))
			continue
		}
		entries = append(entries, fileEntries...)
	}

	j.logger.Info("Journal replay complete", zap.Int("entries", len(entries)))
	return entries, nil
}

func (j *Journal) replayFile(filename string) ([]*JournalEntry, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []*JournalEntry
	decoder := json.NewDecoder(file)

	for {
		var entry JournalEntry
		if err := decoder.Decode(&entry); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to decode journal entry: %w", err)
		}
		entries = append(entries, &entry)
	}

	return entries, nil
}

// Rotate creates a new journal file and closes the old one
func (j *Journal) Rotate() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if err := j.file.Close(); err != nil {
		return fmt.Errorf("failed to close old journal: %w", err)
	}

	journalFile := filepath.Join(j.basePath, fmt.Sprintf("journal-%s.log", time.Now().Format("20060102-150405")))
	file, err := os.OpenFile(journalFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open new journal file: %w", err)
	}

	j.file = file
	j.encoder = json.NewEncoder(file)
	j.logger.Info("Journal rotated", zap.String("new_file", journalFile))
	return nil
}

// Close gracefully closes the journal
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if err := j.file.Sync(); err != nil {
		return err
	}
	return j.file.Close()
}
