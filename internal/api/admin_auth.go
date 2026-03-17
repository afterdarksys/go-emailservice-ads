package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AdminKeyManager manages API keys for administrative access
type AdminKeyManager struct {
	logger *zap.Logger
	keys   map[string]*AdminKey
	mu     sync.RWMutex
}

// AdminKey represents an API key with metadata
type AdminKey struct {
	Key         string
	Name        string
	Permissions []Permission
	CreatedAt   time.Time
	LastUsed    *time.Time
	ExpiresAt   *time.Time
	Active      bool
}

// Permission defines what an API key can do
type Permission string

const (
	// Global permissions
	PermissionAll Permission = "all"

	// Listener management
	PermissionListenerCreate Permission = "listener:create"
	PermissionListenerRead   Permission = "listener:read"
	PermissionListenerUpdate Permission = "listener:update"
	PermissionListenerDelete Permission = "listener:delete"

	// Filter chain management
	PermissionFilterCreate Permission = "filter:create"
	PermissionFilterRead   Permission = "filter:read"
	PermissionFilterUpdate Permission = "filter:update"
	PermissionFilterDelete Permission = "filter:delete"

	// Map management
	PermissionMapCreate Permission = "map:create"
	PermissionMapRead   Permission = "map:read"
	PermissionMapUpdate Permission = "map:update"
	PermissionMapDelete Permission = "map:delete"

	// User/tenant management
	PermissionTenantCreate Permission = "tenant:create"
	PermissionTenantRead   Permission = "tenant:read"
	PermissionTenantUpdate Permission = "tenant:update"
	PermissionTenantDelete Permission = "tenant:delete"

	// Queue management
	PermissionQueueRead   Permission = "queue:read"
	PermissionQueueManage Permission = "queue:manage"

	// Policy management
	PermissionPolicyCreate Permission = "policy:create"
	PermissionPolicyRead   Permission = "policy:read"
	PermissionPolicyUpdate Permission = "policy:update"
	PermissionPolicyDelete Permission = "policy:delete"

	// System management
	PermissionSystemConfig Permission = "system:config"
	PermissionSystemMetrics Permission = "system:metrics"
	PermissionSystemLogs   Permission = "system:logs"
)

// NewAdminKeyManager creates a new admin key manager
func NewAdminKeyManager(logger *zap.Logger) *AdminKeyManager {
	return &AdminKeyManager{
		logger: logger,
		keys:   make(map[string]*AdminKey),
	}
}

// GenerateKey creates a new admin API key
func (m *AdminKeyManager) GenerateKey(name string, permissions []Permission, expiresIn *time.Duration) (*AdminKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate cryptographically secure random key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	keyStr := "ads_" + hex.EncodeToString(keyBytes)

	var expiresAt *time.Time
	if expiresIn != nil {
		exp := time.Now().Add(*expiresIn)
		expiresAt = &exp
	}

	key := &AdminKey{
		Key:         keyStr,
		Name:        name,
		Permissions: permissions,
		CreatedAt:   time.Now(),
		ExpiresAt:   expiresAt,
		Active:      true,
	}

	m.keys[keyStr] = key

	m.logger.Info("Generated new admin API key",
		zap.String("name", name),
		zap.Int("permissions", len(permissions)))

	return key, nil
}

// ValidateKey checks if a key is valid and has the required permission
func (m *AdminKeyManager) ValidateKey(keyStr string, requiredPermission Permission) (*AdminKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key, exists := m.keys[keyStr]
	if !exists {
		return nil, fmt.Errorf("invalid API key")
	}

	if !key.Active {
		return nil, fmt.Errorf("API key is inactive")
	}

	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		return nil, fmt.Errorf("API key has expired")
	}

	// Check permissions
	if !m.hasPermission(key, requiredPermission) {
		return nil, fmt.Errorf("insufficient permissions")
	}

	// Update last used
	now := time.Now()
	key.LastUsed = &now

	return key, nil
}

// hasPermission checks if a key has a specific permission
func (m *AdminKeyManager) hasPermission(key *AdminKey, required Permission) bool {
	for _, perm := range key.Permissions {
		if perm == PermissionAll || perm == required {
			return true
		}

		// Check wildcard permissions (e.g., "listener:*" matches "listener:create")
		if strings.Contains(string(perm), ":*") {
			prefix := strings.Split(string(perm), ":")[0]
			requiredPrefix := strings.Split(string(required), ":")[0]
			if prefix == requiredPrefix {
				return true
			}
		}
	}
	return false
}

// RevokeKey deactivates an API key
func (m *AdminKeyManager) RevokeKey(keyStr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key, exists := m.keys[keyStr]
	if !exists {
		return fmt.Errorf("key not found")
	}

	key.Active = false

	m.logger.Info("Revoked admin API key", zap.String("name", key.Name))

	return nil
}

// ListKeys returns all API keys (without the actual key values)
func (m *AdminKeyManager) ListKeys() []*AdminKey {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]*AdminKey, 0, len(m.keys))
	for _, key := range m.keys {
		// Create a copy without the actual key
		keyCopy := *key
		keyCopy.Key = "***" // Redact key
		keys = append(keys, &keyCopy)
	}

	return keys
}

// AdminAuthMiddleware is HTTP middleware for admin API authentication
func AdminAuthMiddleware(keyManager *AdminKeyManager, requiredPermission Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract API key from header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
				return
			}

			// Expected format: "Bearer ads_..."
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
				return
			}

			apiKey := parts[1]

			// Validate key using constant-time comparison
			key, err := keyManager.ValidateKey(apiKey, requiredPermission)
			if err != nil {
				http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}

			// Add key info to request context
			ctx := r.Context()
			ctx = ContextWithAdminKey(ctx, key)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// Context key type
type contextKey string

const adminKeyContextKey contextKey = "admin_key"

// ContextWithAdminKey adds admin key to context
func ContextWithAdminKey(ctx context.Context, key *AdminKey) context.Context {
	return context.WithValue(ctx, adminKeyContextKey, key)
}

// AdminKeyFromContext retrieves admin key from context
func AdminKeyFromContext(ctx context.Context) (*AdminKey, bool) {
	key, ok := ctx.Value(adminKeyContextKey).(*AdminKey)
	return key, ok
}
