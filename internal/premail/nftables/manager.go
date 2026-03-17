package nftables

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// setNameRegex validates nftables set names (alphanumeric, underscore, dash only)
var setNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Manager handles nftables integration for packet marking and filtering
type Manager struct {
	logger      *zap.Logger
	config      *Config
	sets        *Sets
	initialized bool
	mu          sync.Mutex
}

// Config holds nftables configuration
type Config struct {
	Enabled         bool
	TableName       string
	BlacklistSet    string
	RatelimitSet    string
	MonitorSet      string
	UseNftCommand   bool   // If true, use nft command; if false, use library
}

// Sets holds set names for different threat levels
type Sets struct {
	Blacklist string
	Ratelimit string
	Monitor   string
}

// validateSetName validates that a set name only contains safe characters
func validateSetName(name string) error {
	if !setNameRegex.MatchString(name) {
		return fmt.Errorf("invalid set name: %s (must be alphanumeric, underscore, or dash)", name)
	}
	if len(name) == 0 || len(name) > 64 {
		return fmt.Errorf("invalid set name length: %s (must be 1-64 characters)", name)
	}
	return nil
}

// validateIP validates that an IP address is valid
func validateIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("nil IP address")
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified IP address")
	}
	return nil
}

// NewManager creates a new nftables manager
func NewManager(logger *zap.Logger, config *Config) *Manager {
	// Validate set names during initialization
	if err := validateSetName(config.BlacklistSet); err != nil {
		logger.Warn("Invalid blacklist set name, using default", zap.Error(err))
		config.BlacklistSet = "ads_blacklist"
	}
	if err := validateSetName(config.RatelimitSet); err != nil {
		logger.Warn("Invalid ratelimit set name, using default", zap.Error(err))
		config.RatelimitSet = "ads_ratelimit"
	}
	if err := validateSetName(config.MonitorSet); err != nil {
		logger.Warn("Invalid monitor set name, using default", zap.Error(err))
		config.MonitorSet = "ads_monitor"
	}

	return &Manager{
		logger: logger,
		config: config,
		sets: &Sets{
			Blacklist: config.BlacklistSet,
			Ratelimit: config.RatelimitSet,
			Monitor:   config.MonitorSet,
		},
	}
}

// Initialize sets up nftables table and sets
func (m *Manager) Initialize() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled {
		m.logger.Info("nftables integration disabled")
		return nil
	}

	m.logger.Info("Initializing nftables integration")

	// Create table if not exists
	if err := m.execNft("add table inet filter"); err != nil {
		// Ignore error if table already exists
		m.logger.Debug("Table may already exist", zap.Error(err))
	}

	// Create sets
	if err := m.createSets(); err != nil {
		return fmt.Errorf("failed to create nftables sets: %w", err)
	}

	// Create chains
	if err := m.createChains(); err != nil {
		return fmt.Errorf("failed to create nftables chains: %w", err)
	}

	// Create rules
	if err := m.createRules(); err != nil {
		return fmt.Errorf("failed to create nftables rules: %w", err)
	}

	m.initialized = true
	m.logger.Info("nftables integration initialized successfully")

	return nil
}

// createSets creates nftables sets for IP tracking
func (m *Manager) createSets() error {
	sets := []struct {
		name    string
		timeout string
	}{
		{m.sets.Blacklist, "24h"},
		{m.sets.Ratelimit, "1h"},
		{m.sets.Monitor, "30m"},
	}

	for _, set := range sets {
		cmd := fmt.Sprintf(
			"add set inet filter %s { type ipv4_addr; flags timeout; timeout %s; }",
			set.name,
			set.timeout,
		)
		if err := m.execNft(cmd); err != nil {
			// Try without timeout first (set might exist)
			m.logger.Debug("Set may already exist", zap.String("set", set.name), zap.Error(err))
		}
		m.logger.Info("Created nftables set", zap.String("set", set.name))
	}

	return nil
}

// createChains creates nftables chains for filtering
func (m *Manager) createChains() error {
	chains := []string{
		"add chain inet filter adspremail_prerouting { type filter hook prerouting priority 0; }",
		"add chain inet filter adspremail_input { type filter hook input priority 0; }",
	}

	for _, chain := range chains {
		if err := m.execNft(chain); err != nil {
			m.logger.Debug("Chain may already exist", zap.Error(err))
		}
	}

	return nil
}

// createRules creates filtering rules
func (m *Manager) createRules() error {
	rules := []string{
		// Prerouting: Drop blacklisted IPs immediately
		fmt.Sprintf("add rule inet filter adspremail_prerouting ip saddr @%s drop", m.sets.Blacklist),

		// Prerouting: Mark packets for different threat levels
		fmt.Sprintf("add rule inet filter adspremail_prerouting ip saddr @%s meta mark set 0x3", m.sets.Ratelimit),
		fmt.Sprintf("add rule inet filter adspremail_prerouting ip saddr @%s meta mark set 0x1", m.sets.Monitor),

		// Input: Rate limit tarpitted IPs
		"add rule inet filter adspremail_input meta mark 0x3 limit rate 1/minute accept",
		"add rule inet filter adspremail_input meta mark 0x3 drop",
	}

	for _, rule := range rules {
		if err := m.execNft(rule); err != nil {
			// Rules might already exist
			m.logger.Debug("Rule may already exist", zap.Error(err))
		}
	}

	return nil
}

// AddToBlacklist adds an IP to the blacklist set
func (m *Manager) AddToBlacklist(ip net.IP, duration time.Duration) error {
	if !m.config.Enabled {
		return nil
	}

	// Validate IP address
	if err := validateIP(ip); err != nil {
		return fmt.Errorf("invalid IP address: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ipStr := ip.String()
	timeout := formatDuration(duration)

	// Build command with validated inputs
	// Note: set name was validated in NewManager, IP was validated above
	cmd := []string{
		"add", "element", "inet", "filter", m.sets.Blacklist,
		"{", ipStr, "timeout", timeout, "}",
	}

	if err := m.execNftArgs(cmd); err != nil {
		return fmt.Errorf("failed to add %s to blacklist: %w", ipStr, err)
	}

	m.logger.Info("Added IP to blacklist",
		zap.String("ip", ipStr),
		zap.Duration("duration", duration))

	return nil
}

// AddToRatelimit adds an IP to the rate limit set
func (m *Manager) AddToRatelimit(ip net.IP, duration time.Duration) error {
	if !m.config.Enabled {
		return nil
	}

	// Validate IP address
	if err := validateIP(ip); err != nil {
		return fmt.Errorf("invalid IP address: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ipStr := ip.String()
	timeout := formatDuration(duration)

	// Build command with validated inputs
	cmd := []string{
		"add", "element", "inet", "filter", m.sets.Ratelimit,
		"{", ipStr, "timeout", timeout, "}",
	}

	if err := m.execNftArgs(cmd); err != nil {
		return fmt.Errorf("failed to add %s to ratelimit: %w", ipStr, err)
	}

	m.logger.Info("Added IP to ratelimit",
		zap.String("ip", ipStr),
		zap.Duration("duration", duration))

	return nil
}

// AddToMonitor adds an IP to the monitor set
func (m *Manager) AddToMonitor(ip net.IP, duration time.Duration) error {
	if !m.config.Enabled {
		return nil
	}

	// Validate IP address
	if err := validateIP(ip); err != nil {
		return fmt.Errorf("invalid IP address: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ipStr := ip.String()
	timeout := formatDuration(duration)

	// Build command with validated inputs
	cmd := []string{
		"add", "element", "inet", "filter", m.sets.Monitor,
		"{", ipStr, "timeout", timeout, "}",
	}

	if err := m.execNftArgs(cmd); err != nil {
		return fmt.Errorf("failed to add %s to monitor: %w", ipStr, err)
	}

	m.logger.Debug("Added IP to monitor",
		zap.String("ip", ipStr),
		zap.Duration("duration", duration))

	return nil
}

// MarkPacket marks packets from an IP (not directly supported, handled by sets)
func (m *Manager) MarkPacket(ip net.IP, mark uint32) error {
	// Packet marking is handled automatically by the rules when IPs are in sets
	// This is a no-op but kept for interface compatibility
	return nil
}

// RemoveFromBlacklist removes an IP from the blacklist
func (m *Manager) RemoveFromBlacklist(ip net.IP) error {
	if !m.config.Enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ipStr := ip.String()

	cmd := fmt.Sprintf("delete element inet filter %s { %s }",
		m.sets.Blacklist, ipStr)

	if err := m.execNft(cmd); err != nil {
		return fmt.Errorf("failed to remove %s from blacklist: %w", ipStr, err)
	}

	m.logger.Info("Removed IP from blacklist", zap.String("ip", ipStr))

	return nil
}

// ListBlacklist returns all IPs in the blacklist
func (m *Manager) ListBlacklist() ([]net.IP, error) {
	if !m.config.Enabled {
		return []net.IP{}, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	cmd := fmt.Sprintf("list set inet filter %s", m.sets.Blacklist)

	output, err := m.execNftOutput(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list blacklist: %w", err)
	}

	// Parse output to extract IPs
	ips := parseNftSetOutput(output)

	return ips, nil
}

// GetStats returns statistics about the sets
func (m *Manager) GetStats() (*Stats, error) {
	if !m.config.Enabled {
		return &Stats{}, nil
	}

	blacklist, _ := m.ListBlacklist()
	// Could also list ratelimit and monitor sets

	return &Stats{
		BlacklistCount: len(blacklist),
		// Add more stats as needed
	}, nil
}

// Stats holds nftables statistics
type Stats struct {
	BlacklistCount int
	RatelimitCount int
	MonitorCount   int
}

// Helper functions

// execNftArgs executes nftables command with separate arguments (prevents injection)
func (m *Manager) execNftArgs(args []string) error {
	m.logger.Debug("Executing nftables command", zap.Strings("args", args))

	out, err := exec.Command("nft", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft command failed: %w, output: %s", err, string(out))
	}

	return nil
}

func (m *Manager) execNft(cmd string) error {
	fullCmd := fmt.Sprintf("nft %s", cmd)
	m.logger.Debug("Executing nftables command", zap.String("cmd", fullCmd))

	out, err := exec.Command("nft", strings.Fields(cmd)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft command failed: %w, output: %s", err, string(out))
	}

	return nil
}

func (m *Manager) execNftOutput(cmd string) (string, error) {
	m.logger.Debug("Executing nftables command", zap.String("cmd", cmd))

	out, err := exec.Command("nft", strings.Fields(cmd)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("nft command failed: %w, output: %s", err, string(out))
	}

	return string(out), nil
}

func formatDuration(d time.Duration) string {
	// Convert to nftables-friendly format
	hours := int(d.Hours())
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}

	minutes := int(d.Minutes())
	if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}

	seconds := int(d.Seconds())
	return fmt.Sprintf("%ds", seconds)
}

func parseNftSetOutput(output string) []net.IP {
	// Parse nft list set output to extract IP addresses
	// Format: "elements = { 1.2.3.4, 5.6.7.8 }"

	ips := []net.IP{}

	// Simple parsing - look for IP patterns
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "elements") {
			// Extract IPs from elements line
			start := strings.Index(line, "{")
			end := strings.Index(line, "}")
			if start > 0 && end > start {
				elements := line[start+1 : end]
				parts := strings.Split(elements, ",")
				for _, part := range parts {
					ipStr := strings.TrimSpace(part)
					if ip := net.ParseIP(ipStr); ip != nil {
						ips = append(ips, ip)
					}
				}
			}
		}
	}

	return ips
}

// Cleanup removes all ADS PreMail nftables rules and sets
func (m *Manager) Cleanup() error {
	if !m.config.Enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Cleaning up nftables configuration")

	// Delete chains
	m.execNft("delete chain inet filter adspremail_prerouting")
	m.execNft("delete chain inet filter adspremail_input")

	// Delete sets
	m.execNft(fmt.Sprintf("delete set inet filter %s", m.sets.Blacklist))
	m.execNft(fmt.Sprintf("delete set inet filter %s", m.sets.Ratelimit))
	m.execNft(fmt.Sprintf("delete set inet filter %s", m.sets.Monitor))

	m.logger.Info("nftables cleanup complete")

	return nil
}

// ExportConfig exports current nftables configuration
func (m *Manager) ExportConfig() (string, error) {
	if !m.config.Enabled {
		return "", nil
	}

	output, err := exec.Command("nft", "list", "table", "inet", "filter").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to export nftables config: %w", err)
	}

	return string(output), nil
}
