package maps

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// CIDRMap implements CIDR network matching
type CIDRMap struct {
	path    string
	entries []*cidrEntry
	mu      sync.RWMutex
	logger  *zap.Logger
}

type cidrEntry struct {
	network *net.IPNet
	result  string
}

// NewCIDRMap creates a new CIDR map
func NewCIDRMap(params map[string]string, logger *zap.Logger) (*CIDRMap, error) {
	path := getParam(params, "path", "")
	if path == "" {
		return nil, fmt.Errorf("cidr map requires 'path' parameter")
	}

	cm := &CIDRMap{
		path:   path,
		logger: logger,
	}

	if err := cm.load(); err != nil {
		return nil, fmt.Errorf("failed to load cidr map: %w", err)
	}

	return cm, nil
}

// load loads the CIDR map from file
func (cm *CIDRMap) load() error {
	file, err := os.Open(cm.path)
	if err != nil {
		return err
	}
	defer file.Close()

	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.entries = nil

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse CIDR and result
		parts := strings.Fields(line)
		if len(parts) < 2 {
			cm.logger.Warn("Invalid line in cidr map",
				zap.String("file", cm.path),
				zap.Int("line", lineNum))
			continue
		}

		cidrStr := parts[0]
		result := strings.Join(parts[1:], " ")

		// Parse CIDR
		_, network, err := net.ParseCIDR(cidrStr)
		if err != nil {
			cm.logger.Error("Failed to parse CIDR",
				zap.String("cidr", cidrStr),
				zap.Error(err))
			continue
		}

		cm.entries = append(cm.entries, &cidrEntry{
			network: network,
			result:  result,
		})
	}

	cm.logger.Info("Loaded CIDR map",
		zap.String("path", cm.path),
		zap.Int("networks", len(cm.entries)))

	return scanner.Err()
}

// Lookup performs a lookup in the CIDR map
func (cm *CIDRMap) Lookup(ctx context.Context, key string) (string, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Parse key as IP address
	ip := net.ParseIP(key)
	if ip == nil {
		return "", fmt.Errorf("invalid IP address: %s", key)
	}

	// Check each network
	for _, entry := range cm.entries {
		if entry.network.Contains(ip) {
			return entry.result, nil
		}
	}

	return "", fmt.Errorf("IP not in any network: %s", key)
}

// Type returns the map type
func (cm *CIDRMap) Type() string {
	return "cidr"
}

// Close closes the CIDR map
func (cm *CIDRMap) Close() error {
	return nil
}
