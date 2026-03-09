package divert

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/afterdarksys/go-emailservice-ads/internal/routing/groups"
)

// DivertConfig represents the divert.yaml configuration
type DivertConfig struct {
	DivertRules []DivertRule    `yaml:"divert_rules"`
	Settings    DivertSettings  `yaml:"settings"`
}

// DivertRule defines a mail diversion rule
type DivertRule struct {
	Name         string       `yaml:"name"`
	Enabled      bool         `yaml:"enabled"`
	Match        MatchCriteria `yaml:"match"`
	Action       DivertAction  `yaml:"action"`
}

// MatchCriteria defines when a rule should trigger
type MatchCriteria struct {
	Type             string   `yaml:"type"` // recipient, sender, group, domain, content
	Value            string   `yaml:"value"`
	Pattern          string   `yaml:"pattern,omitempty"`
	CaseInsensitive  bool     `yaml:"case_insensitive,omitempty"`
	Schedule         *Schedule `yaml:"schedule,omitempty"`
}

// Schedule defines time-based matching
type Schedule struct {
	Days  []string `yaml:"days,omitempty"`  // monday, tuesday, etc.
	Hours string   `yaml:"hours,omitempty"` // all, business, or HH:MM-HH:MM
}

// DivertAction defines what to do when rule matches
type DivertAction struct {
	DivertTo      string `yaml:"divert_to"`
	Reason        string `yaml:"reason"`
	NotifySender  bool   `yaml:"notify_sender"`
	Encrypt       bool   `yaml:"encrypt"`
	AttachOriginal bool  `yaml:"attach_original,omitempty"`
}

// DivertSettings contains global diversion settings
type DivertSettings struct {
	AuditLog          string `yaml:"audit_log"`
	AttachmentFormat  string `yaml:"attachment_format"`
	IncludeHeaders    bool   `yaml:"include_headers"`
	HashAlgorithm     string `yaml:"hash_algorithm"`
	MaxMessageSize    int64  `yaml:"max_message_size"`
}

// Engine handles mail diversion logic
type Engine struct {
	config       *DivertConfig
	logger       *zap.Logger
	groupManager *groups.Manager
	auditLogger  *AuditLogger
	composer     *Composer
	configPath   string

	mu sync.RWMutex
}

// NewEngine creates a new divert engine
func NewEngine(configPath string, logger *zap.Logger, groupManager *groups.Manager) (*Engine, error) {
	config, err := LoadDivertConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load divert config: %w", err)
	}

	// Initialize audit logger
	auditLogger, err := NewAuditLogger(config.Settings.AuditLog, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit logger: %w", err)
	}

	// Initialize composer
	composer := NewComposer(logger, &config.Settings)

	e := &Engine{
		config:       config,
		logger:       logger,
		groupManager: groupManager,
		auditLogger:  auditLogger,
		composer:     composer,
		configPath:   configPath,
	}

	logger.Info("Divert engine initialized",
		zap.Int("rules_count", len(config.DivertRules)))

	return e, nil
}

// LoadDivertConfig loads divert configuration from file
func LoadDivertConfig(path string) (*DivertConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read divert config: %w", err)
	}

	var config DivertConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse divert config: %w", err)
	}

	// Set defaults
	if config.Settings.AttachmentFormat == "" {
		config.Settings.AttachmentFormat = "message/rfc822"
	}
	if config.Settings.HashAlgorithm == "" {
		config.Settings.HashAlgorithm = "sha256"
	}
	if config.Settings.MaxMessageSize == 0 {
		config.Settings.MaxMessageSize = 52428800 // 50MB
	}

	return &config, nil
}

// CheckDivert checks if a message should be diverted
// Returns: shouldDivert, newRecipient, divertReason, error
func (e *Engine) CheckDivert(ctx context.Context, from, to string, data []byte) (bool, string, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check each enabled rule
	for _, rule := range e.config.DivertRules {
		if !rule.Enabled {
			continue
		}

		matched, err := e.matchesRule(ctx, &rule, from, to, data)
		if err != nil {
			e.logger.Warn("Error matching rule",
				zap.String("rule", rule.Name),
				zap.Error(err))
			continue
		}

		if matched {
			e.logger.Info("Divert rule matched",
				zap.String("rule", rule.Name),
				zap.String("from", from),
				zap.String("to", to),
				zap.String("divert_to", rule.Action.DivertTo))

			// Log to audit trail
			e.auditLogger.LogDivert(from, to, rule.Action.DivertTo, rule.Name, rule.Action.Reason)

			return true, rule.Action.DivertTo, rule.Action.Reason, nil
		}
	}

	return false, "", "", nil
}

// ProcessDivert creates a diverted message
func (e *Engine) ProcessDivert(ctx context.Context, from, originalTo, divertTo, reason string, originalData []byte) ([]byte, error) {
	// Compose diversion message
	divertedMsg, err := e.composer.ComposeDivertMessage(from, originalTo, divertTo, reason, originalData)
	if err != nil {
		return nil, fmt.Errorf("failed to compose divert message: %w", err)
	}

	e.logger.Info("Divert message created",
		zap.String("from", from),
		zap.String("original_to", originalTo),
		zap.String("diverted_to", divertTo),
		zap.Int("original_size", len(originalData)),
		zap.Int("diverted_size", len(divertedMsg)))

	return divertedMsg, nil
}

// matchesRule checks if a message matches a divert rule
func (e *Engine) matchesRule(ctx context.Context, rule *DivertRule, from, to string, data []byte) (bool, error) {
	// Check schedule if specified
	if rule.Match.Schedule != nil {
		if !e.matchesSchedule(rule.Match.Schedule) {
			return false, nil
		}
	}

	// Match based on type
	switch rule.Match.Type {
	case "recipient":
		return e.matchesRecipient(to, rule.Match.Value), nil

	case "sender":
		return e.matchesSender(from, rule.Match.Value), nil

	case "group":
		return e.matchesGroup(ctx, to, rule.Match.Value)

	case "domain":
		return e.matchesDomain(to, rule.Match.Value), nil

	case "content":
		return e.matchesContent(data, rule.Match.Pattern, rule.Match.CaseInsensitive), nil

	default:
		return false, fmt.Errorf("unknown match type: %s", rule.Match.Type)
	}
}

// matchesRecipient checks if recipient matches
func (e *Engine) matchesRecipient(to, value string) bool {
	return to == value
}

// matchesSender checks if sender matches
func (e *Engine) matchesSender(from, value string) bool {
	return from == value
}

// matchesGroup checks if recipient is in a group
func (e *Engine) matchesGroup(ctx context.Context, to, groupName string) (bool, error) {
	if e.groupManager == nil {
		return false, fmt.Errorf("group manager not available")
	}

	return e.groupManager.IsMember(ctx, groupName, to)
}

// matchesDomain checks if recipient domain matches
func (e *Engine) matchesDomain(to, domain string) bool {
	// Extract domain from email
	if idx := len(to) - 1; idx >= 0 {
		for i := idx; i >= 0; i-- {
			if to[i] == '@' {
				recipientDomain := to[i+1:]
				return recipientDomain == domain
			}
		}
	}
	return false
}

// matchesContent checks if message content matches pattern
func (e *Engine) matchesContent(data []byte, pattern string, caseInsensitive bool) bool {
	// TODO: Implement regex or substring matching
	// For now, simple contains check
	content := string(data)
	if caseInsensitive {
		content = toLower(content)
		pattern = toLower(pattern)
	}
	return contains(content, pattern)
}

// matchesSchedule checks if current time matches schedule
func (e *Engine) matchesSchedule(schedule *Schedule) bool {
	now := time.Now()
	weekday := now.Weekday().String()

	// Check day of week
	if len(schedule.Days) > 0 {
		dayMatches := false
		for _, day := range schedule.Days {
			if toLower(day) == toLower(weekday) {
				dayMatches = true
				break
			}
		}
		if !dayMatches {
			return false
		}
	}

	// Check hours
	if schedule.Hours == "all" {
		return true
	}

	// TODO: Implement business hours and time range checking

	return true
}

// Reload reloads the divert configuration
func (e *Engine) Reload() error {
	config, err := LoadDivertConfig(e.configPath)
	if err != nil {
		return fmt.Errorf("failed to reload divert config: %w", err)
	}

	e.mu.Lock()
	e.config = config
	e.mu.Unlock()

	e.logger.Info("Divert configuration reloaded",
		zap.Int("rules_count", len(config.DivertRules)))

	return nil
}

// Helper functions
func toLower(s string) string {
	// Simple lowercase conversion
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
