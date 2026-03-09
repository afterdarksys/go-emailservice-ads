package groups

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"os"
)

// GroupType defines the type of mail group
type GroupType string

const (
	GroupTypeStatic  GroupType = "static"
	GroupTypeLDAP    GroupType = "ldap"
	GroupTypeDynamic GroupType = "dynamic"
)

// GroupsConfig represents the groups.yaml configuration
type GroupsConfig struct {
	Groups map[string]*Group `yaml:"groups"`
}

// Group represents a mail group
type Group struct {
	Type        GroupType         `yaml:"type"`
	Members     []string          `yaml:"members,omitempty"`      // For static groups
	LDAPQuery   string            `yaml:"ldap_query,omitempty"`   // For LDAP groups
	LDAPServer  string            `yaml:"ldap_server,omitempty"`  // For LDAP groups
	Query       string            `yaml:"query,omitempty"`        // For dynamic groups
	Database    string            `yaml:"database,omitempty"`     // For dynamic groups
	Metadata    map[string]string `yaml:"metadata,omitempty"`

	// Internal cache
	cachedMembers []string
	cacheExpiry   time.Time
	cacheMu       sync.RWMutex
}

// Manager handles mail group operations
type Manager struct {
	config      *GroupsConfig
	logger      *zap.Logger
	configPath  string

	// Providers for different group types
	staticProvider  *StaticProvider
	ldapProvider    *LDAPProvider
	dynamicProvider *DynamicProvider

	mu sync.RWMutex
}

// NewManager creates a new groups manager
func NewManager(configPath string, logger *zap.Logger) (*Manager, error) {
	config, err := LoadGroupsConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load groups config: %w", err)
	}

	m := &Manager{
		config:          config,
		logger:          logger,
		configPath:      configPath,
		staticProvider:  NewStaticProvider(logger),
		ldapProvider:    NewLDAPProvider(logger),
		dynamicProvider: NewDynamicProvider(logger),
	}

	logger.Info("Groups manager initialized",
		zap.Int("groups_count", len(config.Groups)))

	return m, nil
}

// LoadGroupsConfig loads the groups configuration from file
func LoadGroupsConfig(path string) (*GroupsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read groups config: %w", err)
	}

	var config GroupsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse groups config: %w", err)
	}

	if config.Groups == nil {
		config.Groups = make(map[string]*Group)
	}

	return &config, nil
}

// GetMembers returns all members of a group
func (m *Manager) GetMembers(ctx context.Context, groupName string) ([]string, error) {
	m.mu.RLock()
	group, exists := m.config.Groups[groupName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("group not found: %s", groupName)
	}

	// Check cache first
	group.cacheMu.RLock()
	if time.Now().Before(group.cacheExpiry) && len(group.cachedMembers) > 0 {
		cached := make([]string, len(group.cachedMembers))
		copy(cached, group.cachedMembers)
		group.cacheMu.RUnlock()
		return cached, nil
	}
	group.cacheMu.RUnlock()

	// Fetch members based on group type
	var members []string
	var err error

	switch group.Type {
	case GroupTypeStatic:
		members, err = m.staticProvider.GetMembers(ctx, group)
	case GroupTypeLDAP:
		members, err = m.ldapProvider.GetMembers(ctx, group)
	case GroupTypeDynamic:
		members, err = m.dynamicProvider.GetMembers(ctx, group)
	default:
		return nil, fmt.Errorf("unknown group type: %s", group.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get members for group %s: %w", groupName, err)
	}

	// Update cache (cache for 5 minutes)
	group.cacheMu.Lock()
	group.cachedMembers = members
	group.cacheExpiry = time.Now().Add(5 * time.Minute)
	group.cacheMu.Unlock()

	return members, nil
}

// IsMember checks if an email address is a member of a group
func (m *Manager) IsMember(ctx context.Context, groupName, email string) (bool, error) {
	members, err := m.GetMembers(ctx, groupName)
	if err != nil {
		return false, err
	}

	emailLower := strings.ToLower(email)
	for _, member := range members {
		if strings.ToLower(member) == emailLower {
			return true, nil
		}
	}

	return false, nil
}

// ExpandRecipients expands group names to individual email addresses
// If a recipient is a group name (prefixed with @), expand it
func (m *Manager) ExpandRecipients(ctx context.Context, recipients []string) ([]string, error) {
	var expanded []string
	seen := make(map[string]bool)

	for _, rcpt := range recipients {
		// Check if recipient is a group reference (e.g., @executives)
		if strings.HasPrefix(rcpt, "@") {
			groupName := strings.TrimPrefix(rcpt, "@")
			members, err := m.GetMembers(ctx, groupName)
			if err != nil {
				m.logger.Warn("Failed to expand group",
					zap.String("group", groupName),
					zap.Error(err))
				continue
			}

			for _, member := range members {
				memberLower := strings.ToLower(member)
				if !seen[memberLower] {
					expanded = append(expanded, member)
					seen[memberLower] = true
				}
			}
		} else {
			// Regular email address
			rcptLower := strings.ToLower(rcpt)
			if !seen[rcptLower] {
				expanded = append(expanded, rcpt)
				seen[rcptLower] = true
			}
		}
	}

	return expanded, nil
}

// AddGroup adds a new group to the configuration
func (m *Manager) AddGroup(name string, group *Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.config.Groups[name]; exists {
		return fmt.Errorf("group already exists: %s", name)
	}

	m.config.Groups[name] = group

	m.logger.Info("Group added",
		zap.String("name", name),
		zap.String("type", string(group.Type)))

	return nil
}

// RemoveGroup removes a group from the configuration
func (m *Manager) RemoveGroup(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.config.Groups[name]; !exists {
		return fmt.Errorf("group not found: %s", name)
	}

	delete(m.config.Groups, name)

	m.logger.Info("Group removed", zap.String("name", name))

	return nil
}

// ListGroups returns all group names
func (m *Manager) ListGroups() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	groups := make([]string, 0, len(m.config.Groups))
	for name := range m.config.Groups {
		groups = append(groups, name)
	}

	return groups
}

// GetGroup returns group information
func (m *Manager) GetGroup(name string) (*Group, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	group, exists := m.config.Groups[name]
	if !exists {
		return nil, fmt.Errorf("group not found: %s", name)
	}

	return group, nil
}

// InvalidateCache clears the cache for a specific group or all groups
func (m *Manager) InvalidateCache(groupName string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if groupName == "" {
		// Clear all caches
		for _, group := range m.config.Groups {
			group.cacheMu.Lock()
			group.cachedMembers = nil
			group.cacheExpiry = time.Time{}
			group.cacheMu.Unlock()
		}
	} else {
		// Clear specific group cache
		if group, exists := m.config.Groups[groupName]; exists {
			group.cacheMu.Lock()
			group.cachedMembers = nil
			group.cacheExpiry = time.Time{}
			group.cacheMu.Unlock()
		}
	}

	m.logger.Debug("Cache invalidated",
		zap.String("group", groupName))
}

// Reload reloads the groups configuration
func (m *Manager) Reload() error {
	config, err := LoadGroupsConfig(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to reload groups config: %w", err)
	}

	m.mu.Lock()
	m.config = config
	m.mu.Unlock()

	// Invalidate all caches
	m.InvalidateCache("")

	m.logger.Info("Groups configuration reloaded",
		zap.Int("groups_count", len(config.Groups)))

	return nil
}
