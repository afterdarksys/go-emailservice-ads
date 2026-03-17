package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// Alias represents an email alias
type Alias struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`       // Source email address
	Destination string    `json:"destination"`  // Destination email address
	TenantID    string    `json:"tenant_id,omitempty"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Comment     string    `json:"comment,omitempty"`
}

// AliasManager manages email aliases
type AliasManager struct {
	logger  *zap.Logger
	aliases map[string]*Alias
	mu      sync.RWMutex
}

// NewAliasManager creates a new alias manager
func NewAliasManager(logger *zap.Logger) *AliasManager {
	return &AliasManager{
		logger:  logger,
		aliases: make(map[string]*Alias),
	}
}

// CreateAlias creates a new alias
func (m *AliasManager) CreateAlias(alias *Alias) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate email addresses
	if !isValidEmail(alias.Source) {
		return fmt.Errorf("invalid source email: %s", alias.Source)
	}
	if !isValidEmail(alias.Destination) {
		return fmt.Errorf("invalid destination email: %s", alias.Destination)
	}

	// Check if alias already exists
	if _, exists := m.aliases[alias.ID]; exists {
		return fmt.Errorf("alias with ID %s already exists", alias.ID)
	}

	// Check for duplicate source
	for _, existing := range m.aliases {
		if existing.Source == alias.Source && existing.TenantID == alias.TenantID {
			return fmt.Errorf("alias for %s already exists", alias.Source)
		}
	}

	alias.CreatedAt = time.Now()
	alias.UpdatedAt = time.Now()
	alias.Active = true

	m.aliases[alias.ID] = alias

	m.logger.Info("Created alias",
		zap.String("id", alias.ID),
		zap.String("source", alias.Source),
		zap.String("destination", alias.Destination))

	return nil
}

// GetAlias retrieves an alias by ID
func (m *AliasManager) GetAlias(id string) (*Alias, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	alias, exists := m.aliases[id]
	if !exists {
		return nil, fmt.Errorf("alias not found: %s", id)
	}

	return alias, nil
}

// UpdateAlias updates an existing alias
func (m *AliasManager) UpdateAlias(id string, alias *Alias) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.aliases[id]
	if !exists {
		return fmt.Errorf("alias not found: %s", id)
	}

	// Validate email addresses
	if !isValidEmail(alias.Source) {
		return fmt.Errorf("invalid source email: %s", alias.Source)
	}
	if !isValidEmail(alias.Destination) {
		return fmt.Errorf("invalid destination email: %s", alias.Destination)
	}

	// Preserve creation time and tenant
	alias.ID = id
	alias.TenantID = existing.TenantID
	alias.CreatedAt = existing.CreatedAt
	alias.UpdatedAt = time.Now()

	m.aliases[id] = alias

	m.logger.Info("Updated alias",
		zap.String("id", id),
		zap.String("source", alias.Source),
		zap.String("destination", alias.Destination))

	return nil
}

// DeleteAlias deletes an alias
func (m *AliasManager) DeleteAlias(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.aliases[id]; !exists {
		return fmt.Errorf("alias not found: %s", id)
	}

	delete(m.aliases, id)

	m.logger.Info("Deleted alias", zap.String("id", id))

	return nil
}

// ListAliases returns all aliases for a tenant
func (m *AliasManager) ListAliases(tenantID string) []*Alias {
	m.mu.RLock()
	defer m.mu.RUnlock()

	aliases := make([]*Alias, 0)
	for _, alias := range m.aliases {
		if tenantID == "" || alias.TenantID == tenantID {
			aliases = append(aliases, alias)
		}
	}

	return aliases
}

// ResolveAlias resolves an email address to its destination
func (m *AliasManager) ResolveAlias(email, tenantID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, alias := range m.aliases {
		if alias.Source == email && alias.TenantID == tenantID && alias.Active {
			return alias.Destination, nil
		}
	}

	return "", fmt.Errorf("no alias found for: %s", email)
}

// HTTP Handlers

// HandleCreateAlias handles POST /api/v1/admin/aliases
func (m *AliasManager) HandleCreateAlias() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var alias Alias
		if err := json.NewDecoder(r.Body).Decode(&alias); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Get tenant from context
		if tenantID, ok := TenantFromContext(r.Context()); ok {
			alias.TenantID = tenantID
		}

		if alias.ID == "" {
			alias.ID = fmt.Sprintf("alias-%d", time.Now().UnixNano())
		}

		if err := m.CreateAlias(&alias); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(alias)
	}
}

// HandleGetAlias handles GET /api/v1/admin/aliases/{id}
func (m *AliasManager) HandleGetAlias() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		alias, err := m.GetAlias(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(alias)
	}
}

// HandleListAliases handles GET /api/v1/admin/aliases
func (m *AliasManager) HandleListAliases() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get tenant from context
		tenantID := ""
		if tid, ok := TenantFromContext(r.Context()); ok {
			tenantID = tid
		}

		aliases := m.ListAliases(tenantID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(aliases)
	}
}

// HandleUpdateAlias handles PUT /api/v1/admin/aliases/{id}
func (m *AliasManager) HandleUpdateAlias() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		var alias Alias
		if err := json.NewDecoder(r.Body).Decode(&alias); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := m.UpdateAlias(id, &alias); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(alias)
	}
}

// HandleDeleteAlias handles DELETE /api/v1/admin/aliases/{id}
func (m *AliasManager) HandleDeleteAlias() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		if err := m.DeleteAlias(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// Helper functions

func isValidEmail(email string) bool {
	if email == "" {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	if parts[0] == "" || parts[1] == "" {
		return false
	}
	return true
}
