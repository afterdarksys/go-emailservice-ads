package screen

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/afterdarksys/go-emailservice-ads/internal/routing/groups"
)

// ScreenConfig represents the screen.yaml configuration
type ScreenConfig struct {
	ScreenRules []ScreenRule    `yaml:"screen_rules"`
	Settings    ScreenSettings  `yaml:"settings"`
}

// ScreenRule defines a mail screening rule
type ScreenRule struct {
	Name    string         `yaml:"name"`
	Enabled bool           `yaml:"enabled"`
	Match   MatchCriteria  `yaml:"match"`
	Action  ScreenAction   `yaml:"action"`
}

// MatchCriteria defines when a rule should trigger
type MatchCriteria struct {
	Type            string   `yaml:"type"` // user, group, sender, domain, content
	Value           string   `yaml:"value"`
	Direction       string   `yaml:"direction,omitempty"` // both, inbound, outbound
	Domain          string   `yaml:"domain,omitempty"`
	RecipientDomain string   `yaml:"recipient_domain,omitempty"`
	Group           string   `yaml:"group,omitempty"`
	Keywords        []string `yaml:"keywords,omitempty"`
	CaseInsensitive bool     `yaml:"case_insensitive,omitempty"`
}

// ScreenAction defines what to do when rule matches
type ScreenAction struct {
	ScreenTo    []string `yaml:"screen_to"`
	AddHeader   bool     `yaml:"add_header"`
	HeaderName  string   `yaml:"header_name,omitempty"`
	Encrypt     bool     `yaml:"encrypt"`
	RetentionDays int    `yaml:"retention_days,omitempty"`
	Priority    string   `yaml:"priority,omitempty"` // high, normal, low
	Alert       bool     `yaml:"alert,omitempty"`
	SampleRate  float64  `yaml:"sample_rate,omitempty"` // 0.0 to 1.0
}

// ScreenSettings contains global screening settings
type ScreenSettings struct {
	AuditLog             string `yaml:"audit_log"`
	MaxWatchers          int    `yaml:"max_watchers"`
	EncryptionRequired   bool   `yaml:"encryption_required"`
	DefaultRetentionDays int    `yaml:"default_retention_days"`
	PreserveOriginal     bool   `yaml:"preserve_original"`
	NotifyWatchers       bool   `yaml:"notify_watchers"`
}

// Engine handles mail screening logic
type Engine struct {
	config       *ScreenConfig
	logger       *zap.Logger
	groupManager *groups.Manager
	auditLogger  *AuditLogger
	copier       *Copier
	configPath   string

	mu sync.RWMutex
}

// NewEngine creates a new screen engine
func NewEngine(configPath string, logger *zap.Logger, groupManager *groups.Manager) (*Engine, error) {
	config, err := LoadScreenConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load screen config: %w", err)
	}

	// Initialize audit logger
	auditLogger, err := NewAuditLogger(config.Settings.AuditLog, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit logger: %w", err)
	}

	// Initialize copier
	copier := NewCopier(logger, &config.Settings)

	e := &Engine{
		config:       config,
		logger:       logger,
		groupManager: groupManager,
		auditLogger:  auditLogger,
		copier:       copier,
		configPath:   configPath,
	}

	logger.Info("Screen engine initialized",
		zap.Int("rules_count", len(config.ScreenRules)))

	return e, nil
}

// LoadScreenConfig loads screen configuration from file
func LoadScreenConfig(path string) (*ScreenConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read screen config: %w", err)
	}

	var config ScreenConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse screen config: %w", err)
	}

	// Set defaults
	if config.Settings.MaxWatchers == 0 {
		config.Settings.MaxWatchers = 5
	}
	if config.Settings.DefaultRetentionDays == 0 {
		config.Settings.DefaultRetentionDays = 90
	}

	return &config, nil
}

// CheckScreen checks if a message should be screened
// Returns: shouldScreen, watchers, error
func (e *Engine) CheckScreen(ctx context.Context, from, to string, data []byte) (bool, []string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	allWatchers := make(map[string]bool)

	// Check each enabled rule
	for _, rule := range e.config.ScreenRules {
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
			// Check sample rate if specified
			if rule.Action.SampleRate > 0 && rule.Action.SampleRate < 1.0 {
				// TODO: Implement sampling logic
				// For now, screen everything
			}

			e.logger.Info("Screen rule matched",
				zap.String("rule", rule.Name),
				zap.String("from", from),
				zap.String("to", to),
				zap.Int("watchers", len(rule.Action.ScreenTo)))

			// Collect watchers
			for _, watcher := range rule.Action.ScreenTo {
				allWatchers[watcher] = true
			}

			// Log to audit trail
			e.auditLogger.LogScreen(from, to, rule.Action.ScreenTo, rule.Name)
		}
	}

	if len(allWatchers) == 0 {
		return false, nil, nil
	}

	// Convert map to slice
	watchers := make([]string, 0, len(allWatchers))
	for watcher := range allWatchers {
		watchers = append(watchers, watcher)
	}

	// Enforce max watchers limit
	if len(watchers) > e.config.Settings.MaxWatchers {
		e.logger.Warn("Too many watchers, truncating",
			zap.Int("count", len(watchers)),
			zap.Int("max", e.config.Settings.MaxWatchers))
		watchers = watchers[:e.config.Settings.MaxWatchers]
	}

	return true, watchers, nil
}

// ProcessScreen creates screened copies of the message
func (e *Engine) ProcessScreen(ctx context.Context, from, to string, watchers []string, originalData []byte) error {
	for _, watcher := range watchers {
		screenedMsg, err := e.copier.CreateScreenedCopy(from, to, watcher, originalData)
		if err != nil {
			e.logger.Error("Failed to create screened copy",
				zap.String("watcher", watcher),
				zap.Error(err))
			continue
		}

		// TODO: Enqueue screened message for delivery
		// This should integrate with the queue manager

		e.logger.Info("Screened copy created",
			zap.String("from", from),
			zap.String("to", to),
			zap.String("watcher", watcher),
			zap.Int("size", len(screenedMsg)))
	}

	return nil
}

// matchesRule checks if a message matches a screen rule
func (e *Engine) matchesRule(ctx context.Context, rule *ScreenRule, from, to string, data []byte) (bool, error) {
	switch rule.Match.Type {
	case "user":
		return e.matchesUser(from, to, rule.Match.Value, rule.Match.Direction), nil

	case "group":
		return e.matchesGroup(ctx, from, to, rule.Match.Value, rule.Match.Direction)

	case "sender":
		return e.matchesSender(from, rule.Match.Value), nil

	case "domain":
		return e.matchesDomain(to, rule.Match.Value), nil

	case "content":
		return e.matchesContent(data, rule.Match.Keywords, rule.Match.CaseInsensitive), nil

	default:
		return false, fmt.Errorf("unknown match type: %s", rule.Match.Type)
	}
}

// matchesUser checks if user matches
func (e *Engine) matchesUser(from, to, value, direction string) bool {
	switch direction {
	case "both":
		return from == value || to == value
	case "inbound":
		return to == value
	case "outbound":
		return from == value
	default:
		return from == value || to == value
	}
}

// matchesGroup checks if user is in a group
func (e *Engine) matchesGroup(ctx context.Context, from, to, groupName, direction string) (bool, error) {
	if e.groupManager == nil {
		return false, fmt.Errorf("group manager not available")
	}

	switch direction {
	case "inbound":
		return e.groupManager.IsMember(ctx, groupName, to)
	case "outbound":
		return e.groupManager.IsMember(ctx, groupName, from)
	case "both":
		isFromMember, _ := e.groupManager.IsMember(ctx, groupName, from)
		isToMember, _ := e.groupManager.IsMember(ctx, groupName, to)
		return isFromMember || isToMember, nil
	default:
		isFromMember, _ := e.groupManager.IsMember(ctx, groupName, from)
		isToMember, _ := e.groupManager.IsMember(ctx, groupName, to)
		return isFromMember || isToMember, nil
	}
}

// matchesSender checks if sender matches
func (e *Engine) matchesSender(from, value string) bool {
	return from == value
}

// matchesDomain checks if recipient domain matches
func (e *Engine) matchesDomain(to, domain string) bool {
	if idx := strings.IndexByte(to, '@'); idx >= 0 {
		recipientDomain := to[idx+1:]
		return recipientDomain == domain
	}
	return false
}

// matchesContent checks if message content matches keywords
func (e *Engine) matchesContent(data []byte, keywords []string, caseInsensitive bool) bool {
	if len(keywords) == 0 {
		return false
	}

	content := string(data)
	if caseInsensitive {
		content = strings.ToLower(content)
	}

	for _, keyword := range keywords {
		searchTerm := keyword
		if caseInsensitive {
			searchTerm = strings.ToLower(keyword)
		}

		if strings.Contains(content, searchTerm) {
			return true
		}
	}

	return false
}

// Reload reloads the screen configuration
func (e *Engine) Reload() error {
	config, err := LoadScreenConfig(e.configPath)
	if err != nil {
		return fmt.Errorf("failed to reload screen config: %w", err)
	}

	e.mu.Lock()
	e.config = config
	e.mu.Unlock()

	e.logger.Info("Screen configuration reloaded",
		zap.Int("rules_count", len(config.ScreenRules)))

	return nil
}
