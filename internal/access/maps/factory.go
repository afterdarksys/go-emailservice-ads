package maps

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// Map interface for lookups (to avoid import cycle)
type Map interface {
	Lookup(ctx context.Context, key string) (string, error)
	Type() string
	Close() error
}

// Factory creates lookup maps from configuration
type Factory struct {
	logger *zap.Logger
}

// NewFactory creates a new map factory
func NewFactory(logger *zap.Logger) *Factory {
	return &Factory{
		logger: logger,
	}
}

// Create creates a map from a map specification
// Format: "type:parameter" or "type:key=value,key=value"
func (f *Factory) Create(spec string) (Map, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid map spec: %s (expected type:params)", spec)
	}

	mapType := parts[0]
	params := parseParams(parts[1])

	switch mapType {
	// Database maps
	case "hash":
		return NewHashMap(params, f.logger)
	case "btree":
		return NewBTreeMap(params, f.logger)
	case "dbm":
		return NewDBMMap(params, f.logger)
	case "lmdb":
		return NewLMDBMap(params, f.logger)
	case "cdb":
		return NewCDBMap(params, f.logger)

	// SQL databases
	case "mysql":
		return NewMySQLMap(params, f.logger)
	case "pgsql", "postgresql":
		return NewPostgreSQLMap(params, f.logger)
	case "sqlite":
		return NewSQLiteMap(params, f.logger)

	// LDAP
	case "ldap":
		return NewLDAPMap(params, false, f.logger)
	case "ldaps":
		return NewLDAPMap(params, true, f.logger)

	// Network services
	case "memcache":
		return NewMemcacheMap(params, f.logger)
	case "tcp":
		return NewTCPMap(params, f.logger)
	case "socketmap":
		return NewSocketMap(params, f.logger)
	case "proxy":
		return NewProxyMap(params, f.logger)

	// File-based
	case "regexp":
		return NewRegexpMap(params, f.logger)
	case "pcre":
		return NewPCREMap(params, f.logger)
	case "cidr":
		return NewCIDRMap(params, f.logger)
	case "texthash":
		return NewTextHashMap(params, f.logger)
	case "inline":
		return NewInlineMap(params, f.logger)
	case "static":
		return NewStaticMap(params, f.logger)
	case "unionmap":
		return NewUnionMap(params, f)
	case "pipemap":
		return NewPipeMap(params, f.logger)

	// Special
	case "nis":
		return NewNISMap(params, f.logger)
	case "nisplus":
		return NewNISPlusMap(params, f.logger)
	case "sdbm":
		return NewSDBMMap(params, f.logger)
	case "fail":
		return NewFailMap(params, f.logger)
	case "environ":
		return NewEnvironMap(params, f.logger)

	default:
		return nil, fmt.Errorf("unknown map type: %s", mapType)
	}
}

// parseParams parses map parameters
// Supports: "key=value,key=value" or just "path"
func parseParams(paramStr string) map[string]string {
	params := make(map[string]string)

	// If no = sign, treat entire string as "path"
	if !strings.Contains(paramStr, "=") {
		params["path"] = paramStr
		return params
	}

	// Parse key=value pairs
	pairs := strings.Split(paramStr, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			params[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}

	return params
}

// getParam gets a parameter with a default value
func getParam(params map[string]string, key, defaultValue string) string {
	if val, ok := params[key]; ok {
		return val
	}
	return defaultValue
}
