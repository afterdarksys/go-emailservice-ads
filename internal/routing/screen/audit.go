package screen

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AuditEntry represents a single screening audit log entry
type AuditEntry struct {
	Timestamp    time.Time `json:"timestamp"`
	FromAddress  string    `json:"from_address"`
	ToAddress    string    `json:"to_address"`
	Watchers     []string  `json:"watchers"`
	RuleName     string    `json:"rule_name"`
	MessageHash  string    `json:"message_hash,omitempty"`
	Success      bool      `json:"success"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

// AuditLogger handles screening audit logging
type AuditLogger struct {
	logPath string
	logger  *zap.Logger
	file    *os.File
	mu      sync.Mutex
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(logPath string, logger *zap.Logger) (*AuditLogger, error) {
	if logPath == "" {
		logPath = "/var/log/mail/screen-audit.log"
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll("/var/log/mail", 0755); err != nil {
		// Try current directory if /var/log/mail fails
		logPath = "./screen-audit.log"
	}

	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log: %w", err)
	}

	al := &AuditLogger{
		logPath: logPath,
		logger:  logger,
		file:    file,
	}

	logger.Info("Screen audit logger initialized", zap.String("path", logPath))

	return al, nil
}

// LogScreen logs a mail screening event
func (al *AuditLogger) LogScreen(from, to string, watchers []string, ruleName string) error {
	entry := AuditEntry{
		Timestamp:   time.Now().UTC(),
		FromAddress: from,
		ToAddress:   to,
		Watchers:    watchers,
		RuleName:    ruleName,
		Success:     true,
	}

	return al.writeEntry(&entry)
}

// LogScreenError logs a failed screening attempt
func (al *AuditLogger) LogScreenError(from, to string, watchers []string, ruleName, errMsg string) error {
	entry := AuditEntry{
		Timestamp:    time.Now().UTC(),
		FromAddress:  from,
		ToAddress:    to,
		Watchers:     watchers,
		RuleName:     ruleName,
		Success:      false,
		ErrorMessage: errMsg,
	}

	return al.writeEntry(&entry)
}

// writeEntry writes an audit entry to the log file
func (al *AuditLogger) writeEntry(entry *AuditEntry) error {
	al.mu.Lock()
	defer al.mu.Unlock()

	// Marshal to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		al.logger.Error("Failed to marshal audit entry", zap.Error(err))
		return fmt.Errorf("failed to marshal audit entry: %w", err)
	}

	// Write to file
	if _, err := al.file.Write(append(data, '\n')); err != nil {
		al.logger.Error("Failed to write audit entry", zap.Error(err))
		return fmt.Errorf("failed to write audit entry: %w", err)
	}

	// Sync to disk for durability
	if err := al.file.Sync(); err != nil {
		al.logger.Warn("Failed to sync audit log", zap.Error(err))
	}

	return nil
}

// Close closes the audit log file
func (al *AuditLogger) Close() error {
	al.mu.Lock()
	defer al.mu.Unlock()

	if al.file != nil {
		return al.file.Close()
	}

	return nil
}

// Rotate rotates the audit log file
func (al *AuditLogger) Rotate() error {
	al.mu.Lock()
	defer al.mu.Unlock()

	// Close current file
	if al.file != nil {
		al.file.Close()
	}

	// Rename current log
	timestamp := time.Now().Format("20060102-150405")
	oldPath := al.logPath
	newPath := fmt.Sprintf("%s.%s", oldPath, timestamp)

	if err := os.Rename(oldPath, newPath); err != nil {
		al.logger.Warn("Failed to rename audit log", zap.Error(err))
	}

	// Open new file
	file, err := os.OpenFile(al.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open new audit log: %w", err)
	}

	al.file = file

	al.logger.Info("Screen audit log rotated",
		zap.String("old_path", oldPath),
		zap.String("new_path", newPath))

	return nil
}
