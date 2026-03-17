package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// Tenant represents a customer instance
type Tenant struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Domain      string    `json:"domain"`
	Status      string    `json:"status"` // active, suspended, deleted
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Resource limits
	MaxUsers      int `json:"max_users"`
	MaxDomains    int `json:"max_domains"`
	MaxListeners  int `json:"max_listeners"`
	MaxStorage    int64 `json:"max_storage_bytes"`

	// Configuration
	Settings map[string]interface{} `json:"settings,omitempty"`
}

// TenantManager manages multi-tenant isolation
type TenantManager struct {
	logger  *zap.Logger
	tenants map[string]*Tenant
	mu      sync.RWMutex
}

// NewTenantManager creates a new tenant manager
func NewTenantManager(logger *zap.Logger) *TenantManager {
	return &TenantManager{
		logger:  logger,
		tenants: make(map[string]*Tenant),
	}
}

// CreateTenant creates a new tenant
func (m *TenantManager) CreateTenant(tenant *Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[tenant.ID]; exists {
		return fmt.Errorf("tenant with ID %s already exists", tenant.ID)
	}

	tenant.CreatedAt = time.Now()
	tenant.UpdatedAt = time.Now()
	tenant.Status = "active"

	m.tenants[tenant.ID] = tenant

	m.logger.Info("Created tenant",
		zap.String("tenant_id", tenant.ID),
		zap.String("name", tenant.Name))

	return nil
}

// GetTenant retrieves a tenant by ID
func (m *TenantManager) GetTenant(id string) (*Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tenant, exists := m.tenants[id]
	if !exists {
		return nil, fmt.Errorf("tenant not found: %s", id)
	}

	return tenant, nil
}

// UpdateTenant updates an existing tenant
func (m *TenantManager) UpdateTenant(id string, tenant *Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[id]; !exists {
		return fmt.Errorf("tenant not found: %s", id)
	}

	tenant.ID = id
	tenant.UpdatedAt = time.Now()
	m.tenants[id] = tenant

	m.logger.Info("Updated tenant", zap.String("tenant_id", id))

	return nil
}

// DeleteTenant marks a tenant as deleted
func (m *TenantManager) DeleteTenant(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tenant, exists := m.tenants[id]
	if !exists {
		return fmt.Errorf("tenant not found: %s", id)
	}

	tenant.Status = "deleted"
	tenant.UpdatedAt = time.Now()

	m.logger.Info("Deleted tenant", zap.String("tenant_id", id))

	return nil
}

// ListTenants returns all tenants
func (m *TenantManager) ListTenants() []*Tenant {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tenants := make([]*Tenant, 0, len(m.tenants))
	for _, tenant := range m.tenants {
		if tenant.Status != "deleted" {
			tenants = append(tenants, tenant)
		}
	}

	return tenants
}

// HTTP Handlers

// HandleCreateTenant handles POST /api/v1/admin/tenants
func (m *TenantManager) HandleCreateTenant() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var tenant Tenant
		if err := json.NewDecoder(r.Body).Decode(&tenant); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if tenant.ID == "" {
			tenant.ID = fmt.Sprintf("tenant-%d", time.Now().Unix())
		}

		if err := m.CreateTenant(&tenant); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(tenant)
	}
}

// HandleGetTenant handles GET /api/v1/admin/tenants/{id}
func (m *TenantManager) HandleGetTenant() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		tenant, err := m.GetTenant(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tenant)
	}
}

// HandleListTenants handles GET /api/v1/admin/tenants
func (m *TenantManager) HandleListTenants() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenants := m.ListTenants()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tenants)
	}
}

// HandleUpdateTenant handles PUT /api/v1/admin/tenants/{id}
func (m *TenantManager) HandleUpdateTenant() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		var tenant Tenant
		if err := json.NewDecoder(r.Body).Decode(&tenant); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := m.UpdateTenant(id, &tenant); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tenant)
	}
}

// HandleDeleteTenant handles DELETE /api/v1/admin/tenants/{id}
func (m *TenantManager) HandleDeleteTenant() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		if err := m.DeleteTenant(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// Middleware

type tenantContextKey struct{}

// TenantFromContext retrieves tenant ID from context
func TenantFromContext(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(tenantContextKey{}).(string)
	return tenantID, ok
}

// ContextWithTenant adds tenant ID to context
func ContextWithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// TenantIsolationMiddleware enforces tenant isolation
func TenantIsolationMiddleware(tenantManager *TenantManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract tenant ID from header or query param
			tenantID := r.Header.Get("X-Tenant-ID")
			if tenantID == "" {
				tenantID = r.URL.Query().Get("tenant_id")
			}

			// For admin users, allow accessing all tenants
			// For regular users, enforce their tenant
			_, hasAdminKey := AdminKeyFromContext(r.Context())
			if hasAdminKey {
				// Admin can specify tenant or access all
				if tenantID == "" {
					tenantID = "admin"
				}
			} else {
				// Regular users must have tenant specified
				if tenantID == "" {
					http.Error(w, "X-Tenant-ID header required", http.StatusBadRequest)
					return
				}
			}

			// Verify tenant exists and is active
			if tenantID != "admin" {
				tenant, err := tenantManager.GetTenant(tenantID)
				if err != nil {
					http.Error(w, "Invalid tenant", http.StatusForbidden)
					return
				}

				if tenant.Status != "active" {
					http.Error(w, "Tenant not active", http.StatusForbidden)
					return
				}
			}

			// Add tenant to context
			ctx := ContextWithTenant(r.Context(), tenantID)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}
