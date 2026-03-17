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

// FilterManager manages SMTP filter chains
type FilterManager struct {
	logger *zap.Logger
	chains map[string]*FilterChain
	filters map[string]*Filter
	mu     sync.RWMutex
}

// FilterChain represents a sequence of filters
type FilterChain struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	FilterIDs   []string `json:"filter_ids"`
	TenantID    string   `json:"tenant_id,omitempty"`
	Enabled     bool     `json:"enabled"`
}

// Filter represents a single mail filter
type Filter struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        FilterType             `json:"type"`
	Config      map[string]interface{} `json:"config"`
	Order       int                    `json:"order"`
	Enabled     bool                   `json:"enabled"`
	Description string                 `json:"description,omitempty"`
}

// FilterType defines different filter types
type FilterType string

const (
	FilterTypeSPF         FilterType = "spf"
	FilterTypeDKIM        FilterType = "dkim"
	FilterTypeDMARC       FilterType = "dmarc"
	FilterTypeGreylist    FilterType = "greylist"
	FilterTypeRBL         FilterType = "rbl"
	FilterTypeSpamAssassin FilterType = "spamassassin"
	FilterTypeRspamd      FilterType = "rspamd"
	FilterTypeCustom      FilterType = "custom"
	FilterTypeStarlark    FilterType = "starlark"
	FilterTypeContentScan FilterType = "content_scan"
	FilterTypeAttachment  FilterType = "attachment"
	FilterTypeRateLimit   FilterType = "rate_limit"
	FilterTypeAccessControl FilterType = "access_control"
)

// NewFilterManager creates a new filter manager
func NewFilterManager(logger *zap.Logger) *FilterManager {
	return &FilterManager{
		logger:  logger,
		chains:  make(map[string]*FilterChain),
		filters: make(map[string]*Filter),
	}
}

// Filter Management

// CreateFilter creates a new filter
func (m *FilterManager) CreateFilter(filter *Filter) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filters[filter.ID]; exists {
		return fmt.Errorf("filter with ID %s already exists", filter.ID)
	}

	if err := m.validateFilter(filter); err != nil {
		return fmt.Errorf("invalid filter: %w", err)
	}

	m.filters[filter.ID] = filter

	m.logger.Info("Created filter",
		zap.String("id", filter.ID),
		zap.String("type", string(filter.Type)))

	return nil
}

// GetFilter retrieves a filter by ID
func (m *FilterManager) GetFilter(id string) (*Filter, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	filter, exists := m.filters[id]
	if !exists {
		return nil, fmt.Errorf("filter not found: %s", id)
	}

	return filter, nil
}

// UpdateFilter updates an existing filter
func (m *FilterManager) UpdateFilter(id string, filter *Filter) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filters[id]; !exists {
		return fmt.Errorf("filter not found: %s", id)
	}

	if err := m.validateFilter(filter); err != nil {
		return fmt.Errorf("invalid filter: %w", err)
	}

	filter.ID = id
	m.filters[id] = filter

	m.logger.Info("Updated filter", zap.String("id", id))

	return nil
}

// DeleteFilter removes a filter
func (m *FilterManager) DeleteFilter(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filters[id]; !exists {
		return fmt.Errorf("filter not found: %s", id)
	}

	// Check if filter is used in any chains
	for _, chain := range m.chains {
		for _, filterID := range chain.FilterIDs {
			if filterID == id {
				return fmt.Errorf("filter is used in chain %s", chain.ID)
			}
		}
	}

	delete(m.filters, id)

	m.logger.Info("Deleted filter", zap.String("id", id))

	return nil
}

// ListFilters returns all filters
func (m *FilterManager) ListFilters() []*Filter {
	m.mu.RLock()
	defer m.mu.RUnlock()

	filters := make([]*Filter, 0, len(m.filters))
	for _, filter := range m.filters {
		filters = append(filters, filter)
	}

	return filters
}

// Filter Chain Management

// CreateFilterChain creates a new filter chain
func (m *FilterManager) CreateFilterChain(chain *FilterChain) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.chains[chain.ID]; exists {
		return fmt.Errorf("filter chain with ID %s already exists", chain.ID)
	}

	// Validate all filters exist
	for _, filterID := range chain.FilterIDs {
		if _, exists := m.filters[filterID]; !exists {
			return fmt.Errorf("filter not found: %s", filterID)
		}
	}

	m.chains[chain.ID] = chain

	m.logger.Info("Created filter chain",
		zap.String("id", chain.ID),
		zap.Int("filters", len(chain.FilterIDs)))

	return nil
}

// GetFilterChain retrieves a filter chain by ID
func (m *FilterManager) GetFilterChain(id string) (*FilterChain, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	chain, exists := m.chains[id]
	if !exists {
		return nil, fmt.Errorf("filter chain not found: %s", id)
	}

	return chain, nil
}

// UpdateFilterChain updates an existing filter chain
func (m *FilterManager) UpdateFilterChain(id string, chain *FilterChain) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.chains[id]; !exists {
		return fmt.Errorf("filter chain not found: %s", id)
	}

	// Validate all filters exist
	for _, filterID := range chain.FilterIDs {
		if _, exists := m.filters[filterID]; !exists {
			return fmt.Errorf("filter not found: %s", filterID)
		}
	}

	chain.ID = id
	m.chains[id] = chain

	m.logger.Info("Updated filter chain", zap.String("id", id))

	return nil
}

// DeleteFilterChain removes a filter chain
func (m *FilterManager) DeleteFilterChain(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.chains[id]; !exists {
		return fmt.Errorf("filter chain not found: %s", id)
	}

	delete(m.chains, id)

	m.logger.Info("Deleted filter chain", zap.String("id", id))

	return nil
}

// ListFilterChains returns all filter chains
func (m *FilterManager) ListFilterChains(tenantID string) []*FilterChain {
	m.mu.RLock()
	defer m.mu.RUnlock()

	chains := make([]*FilterChain, 0)
	for _, chain := range m.chains {
		if tenantID != "" && chain.TenantID != tenantID {
			continue
		}
		chains = append(chains, chain)
	}

	return chains
}

// validateFilter validates filter configuration
func (m *FilterManager) validateFilter(filter *Filter) error {
	if filter.Name == "" {
		return fmt.Errorf("filter name is required")
	}

	if filter.Type == "" {
		return fmt.Errorf("filter type is required")
	}

	// Type-specific validation
	switch filter.Type {
	case FilterTypeSPF:
		// SPF should have action config
		if _, ok := filter.Config["action"]; !ok {
			filter.Config["action"] = "reject" // default
		}
	case FilterTypeDKIM:
		// DKIM validation config
		if _, ok := filter.Config["verify"]; !ok {
			filter.Config["verify"] = true
		}
	case FilterTypeRBL:
		// RBL needs server list
		if _, ok := filter.Config["servers"]; !ok {
			return fmt.Errorf("RBL filter requires 'servers' config")
		}
	case FilterTypeStarlark:
		// Starlark needs script
		if _, ok := filter.Config["script"]; !ok {
			return fmt.Errorf("Starlark filter requires 'script' config")
		}
	}

	return nil
}

// HTTP Handlers for Filters

// HandleCreateFilter handles POST /api/v1/admin/filters
func (m *FilterManager) HandleCreateFilter() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var filter Filter
		if err := json.NewDecoder(r.Body).Decode(&filter); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if filter.ID == "" {
			filter.ID = fmt.Sprintf("filter-%d", time.Now().Unix())
		}

		if err := m.CreateFilter(&filter); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(filter)
	}
}

// HandleGetFilter handles GET /api/v1/admin/filters/{id}
func (m *FilterManager) HandleGetFilter() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		filter, err := m.GetFilter(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(filter)
	}
}

// HandleListFilters handles GET /api/v1/admin/filters
func (m *FilterManager) HandleListFilters() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filters := m.ListFilters()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(filters)
	}
}

// HandleUpdateFilter handles PUT /api/v1/admin/filters/{id}
func (m *FilterManager) HandleUpdateFilter() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		var filter Filter
		if err := json.NewDecoder(r.Body).Decode(&filter); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := m.UpdateFilter(id, &filter); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(filter)
	}
}

// HandleDeleteFilter handles DELETE /api/v1/admin/filters/{id}
func (m *FilterManager) HandleDeleteFilter() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		if err := m.DeleteFilter(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// HTTP Handlers for Filter Chains

// HandleCreateFilterChain handles POST /api/v1/admin/filter-chains
func (m *FilterManager) HandleCreateFilterChain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var chain FilterChain
		if err := json.NewDecoder(r.Body).Decode(&chain); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if chain.ID == "" {
			chain.ID = fmt.Sprintf("chain-%d", time.Now().Unix())
		}

		if err := m.CreateFilterChain(&chain); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(chain)
	}
}

// HandleGetFilterChain handles GET /api/v1/admin/filter-chains/{id}
func (m *FilterManager) HandleGetFilterChain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		chain, err := m.GetFilterChain(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chain)
	}
}

// HandleListFilterChains handles GET /api/v1/admin/filter-chains
func (m *FilterManager) HandleListFilterChains() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.URL.Query().Get("tenant_id")
		chains := m.ListFilterChains(tenantID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chains)
	}
}

// HandleUpdateFilterChain handles PUT /api/v1/admin/filter-chains/{id}
func (m *FilterManager) HandleUpdateFilterChain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		var chain FilterChain
		if err := json.NewDecoder(r.Body).Decode(&chain); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := m.UpdateFilterChain(id, &chain); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chain)
	}
}

// HandleDeleteFilterChain handles DELETE /api/v1/admin/filter-chains/{id}
func (m *FilterManager) HandleDeleteFilterChain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		if err := m.DeleteFilterChain(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
