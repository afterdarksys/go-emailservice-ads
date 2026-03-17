package policy

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// Manager manages policy loading, evaluation, and caching
type Manager struct {
	logger *zap.Logger

	// Policy storage
	policies   []*PolicyConfig
	policiesMu sync.RWMutex

	// Engine instances
	sieveEngine    Engine
	starlarkEngine Engine

	// Compiled script cache
	compiledCache   map[string]interface{} // script hash -> compiled
	compiledCacheMu sync.RWMutex

	// Configuration
	configPath string

	// Metrics
	evaluations int64
	errors      int64
	metricsMu   sync.Mutex
}

// ManagerConfig configures the policy manager
type ManagerConfig struct {
	ConfigPath string
	Logger     *zap.Logger
}

// NewManager creates a new policy manager
func NewManager(config *ManagerConfig) (*Manager, error) {
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}

	// Create engines
	sieveEngine, err := NewEngine(PolicyTypeSieve)
	if err != nil {
		return nil, fmt.Errorf("failed to create sieve engine: %w", err)
	}

	starlarkEngine, err := NewEngine(PolicyTypeStarlark)
	if err != nil {
		return nil, fmt.Errorf("failed to create starlark engine: %w", err)
	}

	m := &Manager{
		logger:         config.Logger,
		configPath:     config.ConfigPath,
		sieveEngine:    sieveEngine,
		starlarkEngine: starlarkEngine,
		compiledCache:  make(map[string]interface{}),
		policies:       make([]*PolicyConfig, 0),
	}

	// Load policies from config
	if config.ConfigPath != "" {
		if err := m.LoadPolicies(config.ConfigPath); err != nil {
			return nil, fmt.Errorf("failed to load policies: %w", err)
		}
	}

	return m, nil
}

// PoliciesConfig represents the structure of policies.yaml
type PoliciesConfig struct {
	Policies []PolicyConfig `yaml:"policies"`
}

// LoadPolicies loads policy configurations from a YAML file
func (m *Manager) LoadPolicies(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read policies config: %w", err)
	}

	var config PoliciesConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse policies config: %w", err)
	}

	// Load script content from files
	for i := range config.Policies {
		policy := &config.Policies[i]

		// Set defaults
		if policy.MaxExecutionTime == 0 {
			policy.MaxExecutionTime = 10 * time.Second
		}
		if policy.MaxMemory == 0 {
			policy.MaxMemory = 128 * 1024 * 1024 // 128MB
		}

		// Load script from file if ScriptPath is specified
		if policy.ScriptPath != "" && policy.Script == "" {
			scriptData, err := os.ReadFile(policy.ScriptPath)
			if err != nil {
				m.logger.Warn("Failed to load policy script",
					zap.String("policy", policy.Name),
					zap.String("path", policy.ScriptPath),
					zap.Error(err))
				continue
			}
			policy.Script = string(scriptData)
		}

		// Validate script
		engine, err := m.getEngine(policy.Type)
		if err != nil {
			m.logger.Warn("Unknown policy engine type",
				zap.String("policy", policy.Name),
				zap.String("type", string(policy.Type)))
			continue
		}

		if err := engine.Validate(policy.Script); err != nil {
			m.logger.Warn("Policy script validation failed",
				zap.String("policy", policy.Name),
				zap.Error(err))
			continue
		}
	}

	// Sort policies by priority (lower number = higher priority)
	sort.Slice(config.Policies, func(i, j int) bool {
		return config.Policies[i].Priority < config.Policies[j].Priority
	})

	m.policiesMu.Lock()
	m.policies = make([]*PolicyConfig, len(config.Policies))
	for i := range config.Policies {
		m.policies[i] = &config.Policies[i]
	}
	m.policiesMu.Unlock()

	m.logger.Info("Loaded policies",
		zap.Int("count", len(config.Policies)))

	return nil
}

// Reload reloads policies from the configuration file
func (m *Manager) Reload() error {
	if m.configPath == "" {
		return fmt.Errorf("no config path set")
	}

	// Clear compiled cache
	m.compiledCacheMu.Lock()
	m.compiledCache = make(map[string]interface{})
	m.compiledCacheMu.Unlock()

	return m.LoadPolicies(m.configPath)
}

// Evaluate evaluates all applicable policies against an email context
// Returns the first action that matches, or nil if no policies match
func (m *Manager) Evaluate(ctx context.Context, emailCtx *EmailContext) (*Action, error) {
	m.policiesMu.RLock()
	defer m.policiesMu.RUnlock()

	m.metricsMu.Lock()
	m.evaluations++
	m.metricsMu.Unlock()

	for _, policy := range m.policies {
		if !policy.Enabled {
			continue
		}

		// Check if policy scope matches
		if !m.matchesScope(policy, emailCtx) {
			continue
		}

		// Execute policy with timeout
		evalCtx, cancel := context.WithTimeout(ctx, policy.MaxExecutionTime)
		defer cancel()

		action, err := m.evaluatePolicy(evalCtx, policy, emailCtx)
		if err != nil {
			m.logger.Error("Policy evaluation failed",
				zap.String("policy", policy.Name),
				zap.Error(err))

			m.metricsMu.Lock()
			m.errors++
			m.metricsMu.Unlock()


			return action, nil
		}
	}

	// No policies matched - default to accept/keep
	return &Action{Type: ActionKeep}, nil
}

// evaluatePolicy executes a single policy
func (m *Manager) evaluatePolicy(ctx context.Context, policy *PolicyConfig, emailCtx *EmailContext) (*Action, error) {
	engine, err := m.getEngine(policy.Type)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		m.logger.Debug("Policy executed",
			zap.String("policy", policy.Name),
			zap.Duration("duration", duration))
	}()

	// Try to use compiled version if available
	cacheKey := policy.Name
	m.compiledCacheMu.RLock()
	compiled, cached := m.compiledCache[cacheKey]
	m.compiledCacheMu.RUnlock()

	if cached {
		return engine.ExecuteCompiled(ctx, emailCtx, compiled)
	}

	// Compile and cache
	compiled, err = engine.Compile(policy.Script)
	if err != nil {
		return nil, fmt.Errorf("compilation failed: %w", err)
	}

	m.compiledCacheMu.Lock()
	m.compiledCache[cacheKey] = compiled
	m.compiledCacheMu.Unlock()

	return engine.ExecuteCompiled(ctx, emailCtx, compiled)
}

// matchesScope checks if a policy scope matches the email context
func (m *Manager) matchesScope(policy *PolicyConfig, emailCtx *EmailContext) bool {
	scope := policy.Scope

	switch scope.Type {
	case "global", "":
		return true

	case "user":
		// Check if sender or any recipient matches user list
		for _, user := range scope.Users {
			if emailCtx.From == user {
				return true
			}
			for _, recipient := range emailCtx.To {
				if recipient == user {
					return true
				}
			}
		}
		return false

	case "group":
		// Check if sender or recipient is in any of the groups
		for _, group := range scope.Groups {
			for _, senderGroup := range emailCtx.SenderGroups {
				if senderGroup == group {
					return true
				}
			}
			for _, recipientGroup := range emailCtx.RecipientGroups {
				if recipientGroup == group {
					return true
				}
			}
		}
		return false

	case "domain":
		// Check if sender or recipient domain matches
		fromDomain := emailCtx.GetFromDomain()
		for _, domain := range scope.Domains {
			if fromDomain == domain {
				return true
			}
		}
		// Check recipient domains
		// TODO: Extract domain from recipient and compare
		return false

	case "direction":
		switch scope.Direction {
		case "inbound":
			return emailCtx.IsInbound
		case "outbound":
			return emailCtx.IsOutbound
		case "internal":
			return emailCtx.IsInternal
		}
		return false

	default:
		m.logger.Warn("Unknown policy scope type",
			zap.String("type", scope.Type))
		return false
	}
}

// getEngine returns the appropriate engine for a policy type
func (m *Manager) getEngine(policyType PolicyType) (Engine, error) {
	switch policyType {
	case PolicyTypeSieve:
		return m.sieveEngine, nil
	case PolicyTypeStarlark:
		return m.starlarkEngine, nil
	default:
		return nil, fmt.Errorf("unknown policy type: %s", policyType)
	}
}

// GetStats returns policy manager statistics
func (m *Manager) GetStats() map[string]interface{} {
	m.metricsMu.Lock()
	defer m.metricsMu.Unlock()

	m.policiesMu.RLock()
	defer m.policiesMu.RUnlock()

	return map[string]interface{}{
		"policies":    len(m.policies),
		"evaluations": m.evaluations,
		"errors":      m.errors,
		"cache_size":  len(m.compiledCache),
	}
}

// ListPolicies returns a list of all configured policies
func (m *Manager) ListPolicies() []*PolicyConfig {
	m.policiesMu.RLock()
	defer m.policiesMu.RUnlock()

	result := make([]*PolicyConfig, len(m.policies))
	copy(result, m.policies)
	return result
}

// AddPolicy adds a new policy
func (m *Manager) AddPolicy(policy *PolicyConfig) error {
	// Validate
	engine, err := m.getEngine(policy.Type)
	if err != nil {
		return err
	}

	if err := engine.Validate(policy.Script); err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	m.policiesMu.Lock()
	defer m.policiesMu.Unlock()

	m.policies = append(m.policies, policy)

	// Re-sort by priority
	sort.Slice(m.policies, func(i, j int) bool {
		return m.policies[i].Priority < m.policies[j].Priority
	})

	m.logger.Info("Added policy", zap.String("name", policy.Name))
	return nil
}

// RemovePolicy removes a policy by name
func (m *Manager) RemovePolicy(name string) error {
	m.policiesMu.Lock()
	defer m.policiesMu.Unlock()

	for i, policy := range m.policies {
		if policy.Name == name {
			m.policies = append(m.policies[:i], m.policies[i+1:]...)
			m.logger.Info("Removed policy", zap.String("name", name))
			return nil
		}
	}

	return fmt.Errorf("policy not found: %s", name)
}
