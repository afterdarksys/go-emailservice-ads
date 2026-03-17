package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// ListenerManager manages SMTP listeners dynamically
type ListenerManager struct {
	logger    *zap.Logger
	listeners map[string]*ListenerConfig
	mu        sync.RWMutex
}

// ListenerConfig defines a dynamic SMTP listener
type ListenerConfig struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Address     string   `json:"address"`           // e.g., "0.0.0.0:25", "0.0.0.0:587"
	Port        int      `json:"port"`
	TLSRequired bool     `json:"tls_required"`
	TLSCert     string   `json:"tls_cert,omitempty"`
	TLSKey      string   `json:"tls_key,omitempty"`
	AuthRequired bool    `json:"auth_required"`
	MaxConnections int   `json:"max_connections"`
	Enabled     bool     `json:"enabled"`
	TenantID    string   `json:"tenant_id,omitempty"` // Multi-tenant support
	FilterChain []string `json:"filter_chain,omitempty"` // List of filter IDs
}

// NewListenerManager creates a new listener manager
func NewListenerManager(logger *zap.Logger) *ListenerManager {
	return &ListenerManager{
		logger:    logger,
		listeners: make(map[string]*ListenerConfig),
	}
}

// CreateListener creates a new SMTP listener
func (m *ListenerManager) CreateListener(config *ListenerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.listeners[config.ID]; exists {
		return fmt.Errorf("listener with ID %s already exists", config.ID)
	}

	// Validate configuration
	if err := m.validateListenerConfig(config); err != nil {
		return fmt.Errorf("invalid listener configuration: %w", err)
	}

	m.listeners[config.ID] = config

	m.logger.Info("Created listener",
		zap.String("id", config.ID),
		zap.String("address", config.Address),
		zap.Bool("enabled", config.Enabled))

	return nil
}

// GetListener retrieves a listener by ID
func (m *ListenerManager) GetListener(id string) (*ListenerConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	listener, exists := m.listeners[id]
	if !exists {
		return nil, fmt.Errorf("listener not found: %s", id)
	}

	return listener, nil
}

// UpdateListener updates an existing listener
func (m *ListenerManager) UpdateListener(id string, config *ListenerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.listeners[id]; !exists {
		return fmt.Errorf("listener not found: %s", id)
	}

	// Validate configuration
	if err := m.validateListenerConfig(config); err != nil {
		return fmt.Errorf("invalid listener configuration: %w", err)
	}

	config.ID = id // Ensure ID doesn't change
	m.listeners[id] = config

	m.logger.Info("Updated listener", zap.String("id", id))

	return nil
}

// DeleteListener removes a listener
func (m *ListenerManager) DeleteListener(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.listeners[id]; !exists {
		return fmt.Errorf("listener not found: %s", id)
	}

	delete(m.listeners, id)

	m.logger.Info("Deleted listener", zap.String("id", id))

	return nil
}

// ListListeners returns all listeners
func (m *ListenerManager) ListListeners(tenantID string) []*ListenerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	listeners := make([]*ListenerConfig, 0)
	for _, listener := range m.listeners {
		// Filter by tenant if specified
		if tenantID != "" && listener.TenantID != tenantID {
			continue
		}
		listeners = append(listeners, listener)
	}

	return listeners
}

// validateListenerConfig validates listener configuration
func (m *ListenerManager) validateListenerConfig(config *ListenerConfig) error {
	if config.Address == "" {
		return fmt.Errorf("address is required")
	}

	if config.Port < 1 || config.Port > 65535 {
		return fmt.Errorf("invalid port: %d", config.Port)
	}

	if config.TLSRequired && (config.TLSCert == "" || config.TLSKey == "") {
		return fmt.Errorf("TLS cert and key required when TLS is enabled")
	}

	if config.MaxConnections <= 0 {
		config.MaxConnections = 1000 // Default
	}

	return nil
}

// HTTP Handlers

// HandleCreateListener handles POST /api/v1/admin/listeners
func (m *ListenerManager) HandleCreateListener(keyMgr *AdminKeyManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var config ListenerConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Generate ID if not provided
		if config.ID == "" {
			config.ID = fmt.Sprintf("listener-%d", time.Now().Unix())
		}

		if err := m.CreateListener(&config); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(config)
	}
}

// HandleGetListener handles GET /api/v1/admin/listeners/{id}
func (m *ListenerManager) HandleGetListener() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		listener, err := m.GetListener(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listener)
	}
}

// HandleListListeners handles GET /api/v1/admin/listeners
func (m *ListenerManager) HandleListListeners() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.URL.Query().Get("tenant_id")
		listeners := m.ListListeners(tenantID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listeners)
	}
}

// HandleUpdateListener handles PUT /api/v1/admin/listeners/{id}
func (m *ListenerManager) HandleUpdateListener() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		var config ListenerConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := m.UpdateListener(id, &config); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config)
	}
}

// HandleDeleteListener handles DELETE /api/v1/admin/listeners/{id}
func (m *ListenerManager) HandleDeleteListener() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		if err := m.DeleteListener(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
