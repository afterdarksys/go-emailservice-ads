package master

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// MasterConfig represents the master control configuration (master.yaml)
type MasterConfig struct {
	Version  string              `yaml:"version"`
	Services map[string]*Service `yaml:"services"`
	Resources ResourceLimits     `yaml:"resource_limits"`
	HotReload HotReloadConfig    `yaml:"hot_reload"`
}

// Service defines a service configuration (SMTP, IMAP, JMAP, etc.)
type Service struct {
	Type     string          `yaml:"type"` // smtp, imap, jmap
	Enabled  bool            `yaml:"enabled"`
	Listen   string          `yaml:"listen"`
	Workers  int             `yaml:"workers"`
	TLSMode  string          `yaml:"tls_mode,omitempty"` // implicit, required, optional
	Settings ServiceSettings `yaml:"settings,omitempty"`
}

// ServiceSettings contains per-service configuration
type ServiceSettings struct {
	RequireAuth      bool     `yaml:"require_auth,omitempty"`
	RequireTLS       bool     `yaml:"require_tls,omitempty"`
	AllowRelay       bool     `yaml:"allow_relay,omitempty"`
	MaxMessageSize   int64    `yaml:"max_message_size,omitempty"`
	Filters          []string `yaml:"filters,omitempty"`
	MaxConnections   int      `yaml:"max_connections,omitempty"`
	ReadTimeout      int      `yaml:"read_timeout,omitempty"`  // seconds
	WriteTimeout     int      `yaml:"write_timeout,omitempty"` // seconds
}

// ResourceLimits defines global resource constraints
type ResourceLimits struct {
	MaxMemoryMB        int `yaml:"max_memory_mb"`
	MaxCPUPercent      int `yaml:"max_cpu_percent"`
	MaxConnections     int `yaml:"max_connections"`
	MaxFileDescriptors int `yaml:"max_file_descriptors"`
}

// HotReloadConfig controls hot-reload behavior
type HotReloadConfig struct {
	Enabled            bool   `yaml:"enabled"`
	CheckInterval      string `yaml:"check_interval"` // duration string
	ValidateBeforeApply bool  `yaml:"validate_before_apply"`
	BackupOnChange     bool   `yaml:"backup_on_change"`
}

// LoadMasterConfig loads and validates the master configuration
func LoadMasterConfig(path string) (*MasterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read master config: %w", err)
	}

	var cfg MasterConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse master config: %w", err)
	}

	// Set defaults
	if cfg.Version == "" {
		cfg.Version = "1.0"
	}

	if cfg.Resources.MaxConnections == 0 {
		cfg.Resources.MaxConnections = 10000
	}

	if cfg.Resources.MaxFileDescriptors == 0 {
		cfg.Resources.MaxFileDescriptors = 65536
	}

	if cfg.HotReload.CheckInterval == "" {
		cfg.HotReload.CheckInterval = "5s"
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid master config: %w", err)
	}

	return &cfg, nil
}

// Validate checks the configuration for errors
func (mc *MasterConfig) Validate() error {
	if mc.Version == "" {
		return fmt.Errorf("version is required")
	}

	if len(mc.Services) == 0 {
		return fmt.Errorf("at least one service must be defined")
	}

	// Validate each service
	for name, svc := range mc.Services {
		if err := svc.Validate(); err != nil {
			return fmt.Errorf("service %s: %w", name, err)
		}
	}

	// Validate resource limits
	if mc.Resources.MaxMemoryMB < 0 {
		return fmt.Errorf("max_memory_mb must be positive")
	}

	if mc.Resources.MaxCPUPercent < 0 || mc.Resources.MaxCPUPercent > 100 {
		return fmt.Errorf("max_cpu_percent must be between 0 and 100")
	}

	if mc.Resources.MaxConnections <= 0 {
		return fmt.Errorf("max_connections must be positive")
	}

	// Validate hot reload config
	if mc.HotReload.Enabled {
		if _, err := time.ParseDuration(mc.HotReload.CheckInterval); err != nil {
			return fmt.Errorf("invalid check_interval: %w", err)
		}
	}

	return nil
}

// Validate checks a service configuration
func (s *Service) Validate() error {
	if s.Type == "" {
		return fmt.Errorf("type is required")
	}

	validTypes := map[string]bool{
		"smtp": true,
		"imap": true,
		"jmap": true,
		"pop3": true,
	}

	if !validTypes[s.Type] {
		return fmt.Errorf("invalid service type: %s (must be smtp, imap, jmap, or pop3)", s.Type)
	}

	if s.Listen == "" {
		return fmt.Errorf("listen address is required")
	}

	if s.Workers <= 0 {
		return fmt.Errorf("workers must be positive")
	}

	if s.TLSMode != "" {
		validTLSModes := map[string]bool{
			"implicit": true,
			"required": true,
			"optional": true,
		}
		if !validTLSModes[s.TLSMode] {
			return fmt.Errorf("invalid tls_mode: %s (must be implicit, required, or optional)", s.TLSMode)
		}
	}

	return nil
}

// GetEnabledServices returns all enabled services
func (mc *MasterConfig) GetEnabledServices() map[string]*Service {
	enabled := make(map[string]*Service)
	for name, svc := range mc.Services {
		if svc.Enabled {
			enabled[name] = svc
		}
	}
	return enabled
}

// Clone creates a deep copy of the configuration
func (mc *MasterConfig) Clone() *MasterConfig {
	clone := &MasterConfig{
		Version:  mc.Version,
		Services: make(map[string]*Service),
		Resources: mc.Resources,
		HotReload: mc.HotReload,
	}

	for name, svc := range mc.Services {
		svcCopy := *svc
		if svc.Settings.Filters != nil {
			svcCopy.Settings.Filters = make([]string, len(svc.Settings.Filters))
			copy(svcCopy.Settings.Filters, svc.Settings.Filters)
		}
		clone.Services[name] = &svcCopy
	}

	return clone
}
