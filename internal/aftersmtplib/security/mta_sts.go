package security

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RFC 8461 - SMTP MTA Strict Transport Security (MTA-STS)
// This ensures SMTP connections use TLS and validates certificates
// Prevents downgrade attacks and man-in-the-middle attacks on SMTP

// MTASTSPolicy represents an MTA-STS policy
type MTASTSPolicy struct {
	Version string   `json:"version"`
	Mode    string   `json:"mode"` // "enforce", "testing", or "none"
	MX      []string `json:"mx"`
	MaxAge  int      `json:"max_age"` // In seconds
}

// MTASTSManager handles MTA-STS policy fetching and caching
type MTASTSManager struct {
	logger     *zap.Logger
	httpClient *http.Client
	cache      map[string]*MTASTSCacheEntry
	mu         sync.RWMutex
}

// MTASTSCacheEntry represents a cached policy
type MTASTSCacheEntry struct {
	Policy    *MTASTSPolicy
	FetchedAt time.Time
	ExpiresAt time.Time
}

// NewMTASTSManager creates a new MTA-STS manager
func NewMTASTSManager(logger *zap.Logger) *MTASTSManager {
	return &MTASTSManager{
		logger: logger,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 5 * time.Second,
			},
		},
		cache: make(map[string]*MTASTSCacheEntry),
	}
}

// GetPolicy retrieves the MTA-STS policy for a domain
// RFC 8461 Section 3.2 - Policy Discovery
func (m *MTASTSManager) GetPolicy(ctx context.Context, domain string) (*MTASTSPolicy, error) {
	// Check cache first
	m.mu.RLock()
	if entry, exists := m.cache[domain]; exists {
		if time.Now().Before(entry.ExpiresAt) {
			m.mu.RUnlock()
			m.logger.Debug("MTA-STS policy cache hit", zap.String("domain", domain))
			return entry.Policy, nil
		}
	}
	m.mu.RUnlock()

	// Fetch policy from well-known URL
	// RFC 8461 Section 3.2: https://mta-sts.example.com/.well-known/mta-sts.txt
	policyURL := fmt.Sprintf("https://mta-sts.%s/.well-known/mta-sts.txt", domain)

	m.logger.Debug("Fetching MTA-STS policy", zap.String("url", policyURL))

	req, err := http.NewRequestWithContext(ctx, "GET", policyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		m.logger.Warn("Failed to fetch MTA-STS policy", zap.String("domain", domain), zap.Error(err))
		return nil, fmt.Errorf("failed to fetch policy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read and parse policy
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read policy: %w", err)
	}

	policy, err := m.parsePolicy(string(body))
	if err != nil {
		return nil, fmt.Errorf("failed to parse policy: %w", err)
	}

	// Validate policy
	if err := m.validatePolicy(policy); err != nil {
		return nil, fmt.Errorf("invalid policy: %w", err)
	}

	// Cache policy
	m.mu.Lock()
	m.cache[domain] = &MTASTSCacheEntry{
		Policy:    policy,
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Duration(policy.MaxAge) * time.Second),
	}
	m.mu.Unlock()

	m.logger.Info("MTA-STS policy cached",
		zap.String("domain", domain),
		zap.String("mode", policy.Mode),
		zap.Int("max_age", policy.MaxAge))

	return policy, nil
}

// parsePolicy parses an MTA-STS policy file
// RFC 8461 Section 3.2 - Policy Format
func (m *MTASTSManager) parsePolicy(content string) (*MTASTSPolicy, error) {
	policy := &MTASTSPolicy{
		MX: make([]string, 0),
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "version":
			policy.Version = value
		case "mode":
			policy.Mode = value
		case "mx":
			policy.MX = append(policy.MX, value)
		case "max_age":
			var maxAge int
			if _, err := fmt.Sscanf(value, "%d", &maxAge); err == nil {
				policy.MaxAge = maxAge
			}
		}
	}

	return policy, nil
}

// validatePolicy validates an MTA-STS policy
func (m *MTASTSManager) validatePolicy(policy *MTASTSPolicy) error {
	if policy.Version != "STSv1" {
		return fmt.Errorf("unsupported version: %s", policy.Version)
	}

	validModes := map[string]bool{"enforce": true, "testing": true, "none": true}
	if !validModes[policy.Mode] {
		return fmt.Errorf("invalid mode: %s", policy.Mode)
	}

	if policy.Mode != "none" && len(policy.MX) == 0 {
		return fmt.Errorf("no MX patterns specified for non-none mode")
	}

	if policy.MaxAge < 0 {
		return fmt.Errorf("invalid max_age: %d", policy.MaxAge)
	}

	// RFC 8461 Section 3.2: max_age MUST be less than or equal to 31557600 (one year)
	if policy.MaxAge > 31557600 {
		return fmt.Errorf("max_age too large: %d (max 31557600)", policy.MaxAge)
	}

	return nil
}

// ShouldEnforceTLS checks if TLS should be enforced for a given MX host
// RFC 8461 Section 4 - Policy Application
func (m *MTASTSManager) ShouldEnforceTLS(ctx context.Context, domain, mxHost string) (bool, error) {
	policy, err := m.GetPolicy(ctx, domain)
	if err != nil {
		// No policy or error fetching - don't enforce
		m.logger.Debug("No MTA-STS policy available",
			zap.String("domain", domain),
			zap.Error(err))
		return false, nil
	}

	// Mode "none" means don't enforce
	if policy.Mode == "none" {
		return false, nil
	}

	// Check if MX matches any pattern
	for _, pattern := range policy.MX {
		if m.matchesMXPattern(mxHost, pattern) {
			// Mode "enforce" means enforce TLS, "testing" means log only
			if policy.Mode == "enforce" {
				m.logger.Info("MTA-STS enforcement active",
					zap.String("domain", domain),
					zap.String("mx", mxHost))
				return true, nil
			} else if policy.Mode == "testing" {
				m.logger.Info("MTA-STS testing mode",
					zap.String("domain", domain),
					zap.String("mx", mxHost))
				return false, nil
			}
		}
	}

	// MX doesn't match policy - enforcement depends on mode
	if policy.Mode == "enforce" {
		m.logger.Warn("MX host not in MTA-STS policy",
			zap.String("domain", domain),
			zap.String("mx", mxHost))
		return false, fmt.Errorf("MX host %s not authorized by MTA-STS policy", mxHost)
	}

	return false, nil
}

// matchesMXPattern checks if an MX host matches a pattern
// RFC 8461 Section 3.2 - MX patterns support wildcards
func (m *MTASTSManager) matchesMXPattern(mxHost, pattern string) bool {
	// Exact match
	if mxHost == pattern {
		return true
	}

	// Wildcard match (e.g., *.example.com)
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[2:]
		return strings.HasSuffix(mxHost, "."+suffix) || mxHost == suffix
	}

	return false
}

// ClearCache clears the MTA-STS policy cache
func (m *MTASTSManager) ClearCache() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = make(map[string]*MTASTSCacheEntry)
	m.logger.Info("MTA-STS cache cleared")
}

// GetCacheStats returns statistics about the cache
func (m *MTASTSManager) GetCacheStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	active := 0
	expired := 0
	now := time.Now()

	for _, entry := range m.cache {
		if now.Before(entry.ExpiresAt) {
			active++
		} else {
			expired++
		}
	}

	return map[string]interface{}{
		"total":   len(m.cache),
		"active":  active,
		"expired": expired,
	}
}
