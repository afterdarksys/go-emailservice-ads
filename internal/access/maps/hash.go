package maps

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// HashMap implements Berkeley DB hash table-style lookups
type HashMap struct {
	path   string
	data   map[string]string
	mu     sync.RWMutex
	logger *zap.Logger
}

// NewHashMap creates a new hash map
func NewHashMap(params map[string]string, logger *zap.Logger) (*HashMap, error) {
	path := getParam(params, "path", "")
	if path == "" {
		return nil, fmt.Errorf("hash map requires 'path' parameter")
	}

	hm := &HashMap{
		path:   path,
		data:   make(map[string]string),
		logger: logger,
	}

	if err := hm.load(); err != nil {
		return nil, fmt.Errorf("failed to load hash map: %w", err)
	}

	return hm, nil
}

// load loads the hash map from file
func (hm *HashMap) load() error {
	file, err := os.Open(hm.path)
	if err != nil {
		return err
	}
	defer file.Close()

	hm.mu.Lock()
	defer hm.mu.Unlock()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key value pairs (tab or space separated)
		parts := strings.Fields(line)
		if len(parts) < 2 {
			hm.logger.Warn("Invalid line in hash map",
				zap.String("file", hm.path),
				zap.Int("line", lineNum),
				zap.String("content", line))
			continue
		}

		key := parts[0]
		value := strings.Join(parts[1:], " ")
		hm.data[key] = value
	}

	hm.logger.Info("Loaded hash map",
		zap.String("path", hm.path),
		zap.Int("entries", len(hm.data)))

	return scanner.Err()
}

// Lookup performs a lookup in the hash map
func (hm *HashMap) Lookup(ctx context.Context, key string) (string, error) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	if val, ok := hm.data[key]; ok {
		return val, nil
	}

	return "", fmt.Errorf("key not found: %s", key)
}

// Type returns the map type
func (hm *HashMap) Type() string {
	return "hash"
}

// Close closes the hash map
func (hm *HashMap) Close() error {
	return nil
}

// Reload reloads the hash map from disk
func (hm *HashMap) Reload() error {
	return hm.load()
}
