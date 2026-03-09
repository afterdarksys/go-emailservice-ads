package maps

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"
)

// StaticMap always returns a static value
type StaticMap struct {
	value  string
	logger *zap.Logger
}

func NewStaticMap(params map[string]string, logger *zap.Logger) (*StaticMap, error) {
	value := getParam(params, "value", "OK")
	return &StaticMap{value: value, logger: logger}, nil
}

func (sm *StaticMap) Lookup(ctx context.Context, key string) (string, error) {
	return sm.value, nil
}

func (sm *StaticMap) Type() string { return "static" }
func (sm *StaticMap) Close() error { return nil }

// InlineMap uses inline data (key=value pairs in params)
type InlineMap struct {
	data   map[string]string
	logger *zap.Logger
}

func NewInlineMap(params map[string]string, logger *zap.Logger) (*InlineMap, error) {
	// All params except special ones become the inline data
	data := make(map[string]string)
	for k, v := range params {
		if k != "type" {
			data[k] = v
		}
	}
	return &InlineMap{data: data, logger: logger}, nil
}

func (im *InlineMap) Lookup(ctx context.Context, key string) (string, error) {
	if val, ok := im.data[key]; ok {
		return val, nil
	}
	return "", fmt.Errorf("key not found: %s", key)
}

func (im *InlineMap) Type() string { return "inline" }
func (im *InlineMap) Close() error { return nil }

// FailMap always fails (for testing)
type FailMap struct {
	message string
	logger  *zap.Logger
}

func NewFailMap(params map[string]string, logger *zap.Logger) (*FailMap, error) {
	message := getParam(params, "message", "map lookup failed")
	return &FailMap{message: message, logger: logger}, nil
}

func (fm *FailMap) Lookup(ctx context.Context, key string) (string, error) {
	return "", fmt.Errorf(fm.message)
}

func (fm *FailMap) Type() string { return "fail" }
func (fm *FailMap) Close() error { return nil }

// EnvironMap looks up environment variables
type EnvironMap struct {
	prefix string
	logger *zap.Logger
}

func NewEnvironMap(params map[string]string, logger *zap.Logger) (*EnvironMap, error) {
	prefix := getParam(params, "prefix", "")
	return &EnvironMap{prefix: prefix, logger: logger}, nil
}

func (em *EnvironMap) Lookup(ctx context.Context, key string) (string, error) {
	envKey := em.prefix + key
	val := os.Getenv(envKey)
	if val == "" {
		return "", fmt.Errorf("environment variable not found: %s", envKey)
	}
	return val, nil
}

func (em *EnvironMap) Type() string { return "environ" }
func (em *EnvironMap) Close() error { return nil }

// TextHashMap is a read-only plain text hash
type TextHashMap struct {
	*HashMap
}

func NewTextHashMap(params map[string]string, logger *zap.Logger) (*TextHashMap, error) {
	hm, err := NewHashMap(params, logger)
	if err != nil {
		return nil, err
	}
	return &TextHashMap{HashMap: hm}, nil
}

func (thm *TextHashMap) Type() string { return "texthash" }
