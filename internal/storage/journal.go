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
	failed   error
	basePath string
	logger   *zap.Logger
	mu       sync.RWMutex
	file     *os.File
	encoder  *json.Encoder
}

// NewJournal creates a new journal instance
func NewJournal(basePath string, logger *zap.Logger) (*Journal, error) {
	if err := os.MkdirAll(basePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create journal directory: %w", err)
	}

	journalFile := filepath.Join(basePath, journalName())
	file, err := os.OpenFile(journalFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open journal file: %w", err)
	}

	if err = syncDir(basePath); err != nil {
		file.Close()
		return nil, err
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
	return j.writeLocked(entry)
}

// writeLocked rolls back an unacknowledged partial append. If rollback fails,
// latch the journal unhealthy so a later append cannot bury a corrupt record.
func (j *Journal) writeLocked(entry *JournalEntry) error {
	if j.failed != nil {
		return j.failed
	}
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	offset, err := j.file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	err = j.encoder.Encode(entry)
	if err == nil {
		err = j.file.Sync()
	}
	if err == nil {
		return nil
	}
	original := err
	rollback := j.file.Truncate(offset)
	if rollback == nil {
		_, rollback = j.file.Seek(offset, io.SeekStart)
	}
	if rollback == nil {
		rollback = j.file.Sync()
	}
	j.encoder = json.NewEncoder(j.file)
	if rollback != nil {
		j.failed = fmt.Errorf("journal rollback failed: %v (write: %w)", rollback, original)
		return j.failed
	}
	return fmt.Errorf("journal write rolled back: %w", original)
}

// Replay reads all journal entries and returns them for recovery
func (j *Journal) Replay() ([]*JournalEntry, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	files, err := filepath.Glob(filepath.Join(j.basePath, "journal-*.log"))
	if err != nil {
		return nil, fmt.Errorf("failed to list journal files: %w", err)
	}

	var cp checkpoint
	raw, err := os.ReadFile(filepath.Join(j.basePath, "checkpoint.json"))
	if err == nil {
		if err = json.Unmarshal(raw, &cp); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	covered := map[string]bool{}
	for _, name := range cp.Covered {
		covered[name] = true
	}
	var entries []*JournalEntry
	for _, file := range files {
		if covered[filepath.Base(file)] {
			continue
		}
		fileEntries, err := j.replayFile(file)
		if err != nil {
			return nil, fmt.Errorf("replay journal file %s: %w", file, err)
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
			// A process crash can leave an incomplete final JSON line after one or
			// more valid, fsynced records. Keep the valid prefix; any other decode
			// failure is corruption that must halt recovery rather than silently
			// dropping a whole journal file.
			if err == io.ErrUnexpectedEOF {
				j.logger.Warn("Ignoring truncated final journal record", zap.String("file", filename))
				break
			}
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

	journalFile := filepath.Join(j.basePath, journalName())
	file, err := os.OpenFile(journalFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
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
