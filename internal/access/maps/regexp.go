package maps

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// RegexpMap implements regular expression table lookups
type RegexpMap struct {
	path    string
	entries []*regexpEntry
	mu      sync.RWMutex
	logger  *zap.Logger
}

type regexpEntry struct {
	pattern *regexp.Regexp
	result  string
}

// NewRegexpMap creates a new regexp map
func NewRegexpMap(params map[string]string, logger *zap.Logger) (*RegexpMap, error) {
	path := getParam(params, "path", "")
	if path == "" {
		return nil, fmt.Errorf("regexp map requires 'path' parameter")
	}

	rm := &RegexpMap{
		path:   path,
		logger: logger,
	}

	if err := rm.load(); err != nil {
		return nil, fmt.Errorf("failed to load regexp map: %w", err)
	}

	return rm, nil
}

// load loads the regexp map from file
func (rm *RegexpMap) load() error {
	file, err := os.Open(rm.path)
	if err != nil {
		return err
	}
	defer file.Close()

	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.entries = nil

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse pattern and result
		parts := strings.Fields(line)
		if len(parts) < 2 {
			rm.logger.Warn("Invalid line in regexp map",
				zap.String("file", rm.path),
				zap.Int("line", lineNum))
			continue
		}

		patternStr := parts[0]
		result := strings.Join(parts[1:], " ")

		// Compile regexp
		pattern, err := regexp.Compile(patternStr)
		if err != nil {
			rm.logger.Error("Failed to compile regexp",
				zap.String("pattern", patternStr),
				zap.Error(err))
			continue
		}

		rm.entries = append(rm.entries, &regexpEntry{
			pattern: pattern,
			result:  result,
		})
	}

	rm.logger.Info("Loaded regexp map",
		zap.String("path", rm.path),
		zap.Int("patterns", len(rm.entries)))

	return scanner.Err()
}

// Lookup performs a lookup in the regexp map
func (rm *RegexpMap) Lookup(ctx context.Context, key string) (string, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Try each pattern in order
	for _, entry := range rm.entries {
		if entry.pattern.MatchString(key) {
			// Support capture group substitution
			result := entry.pattern.ReplaceAllString(key, entry.result)
			return result, nil
		}
	}

	return "", fmt.Errorf("no pattern matched: %s", key)
}

// Type returns the map type
func (rm *RegexpMap) Type() string {
	return "regexp"
}

// Close closes the regexp map
func (rm *RegexpMap) Close() error {
	return nil
}
