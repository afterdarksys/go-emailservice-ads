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

// MapManager manages all types of mail maps (Postfix-style)
type MapManager struct {
	logger *zap.Logger
	maps   map[string]*MailMap
	mu     sync.RWMutex
}

// MailMap represents a generic mail map
type MailMap struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        MapType                `json:"type"`
	Entries     map[string]interface{} `json:"entries"`
	Description string                 `json:"description,omitempty"`
	TenantID    string                 `json:"tenant_id,omitempty"`
	Enabled     bool                   `json:"enabled"`
}

// MapType defines different map types (Postfix-compatible)
type MapType string

const (
	// Lookup map types
	MapTypeHash       MapType = "hash"        // key-value hash table
	MapTypeBtree      MapType = "btree"       // B-tree database
	MapTypeRegexp     MapType = "regexp"      // Regular expression
	MapTypePCRE       MapType = "pcre"        // Perl Compatible Regular Expressions
	MapTypeCIDR       MapType = "cidr"        // CIDR network blocks

	// Database maps
	MapTypeMySQL      MapType = "mysql"       // MySQL database
	MapTypePostgreSQL MapType = "pgsql"       // PostgreSQL database
	MapTypeSQLite     MapType = "sqlite"      // SQLite database
	MapTypeLDAP       MapType = "ldap"        // LDAP directory

	// Network maps
	MapTypeMemcache   MapType = "memcache"    // Memcached
	MapTypeTCP        MapType = "tcp"         // TCP socket
	MapTypeSocketmap  MapType = "socketmap"   // Socketmap protocol

	// Special maps
	MapTypeAlias      MapType = "alias"       // Email aliases
	MapTypeVirtual    MapType = "virtual"     // Virtual domain mappings
	MapTypeRelay      MapType = "relay"       // Relay domains
	MapTypeTransport  MapType = "transport"   // Transport mappings
	MapTypeCanonical  MapType = "canonical"   // Address rewriting
	MapTypeAccess     MapType = "access"      // Access control
	MapTypeSenderDependent MapType = "sender_dependent" // Sender-dependent routing
)

// MapEntry represents a single map entry
type MapEntry struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
	Comment string    `json:"comment,omitempty"`
}

// NewMapManager creates a new map manager
func NewMapManager(logger *zap.Logger) *MapManager {
	return &MapManager{
		logger: logger,
		maps:   make(map[string]*MailMap),
	}
}

// CreateMap creates a new mail map
func (m *MapManager) CreateMap(mailMap *MailMap) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.maps[mailMap.ID]; exists {
		return fmt.Errorf("map with ID %s already exists", mailMap.ID)
	}

	if err := m.validateMap(mailMap); err != nil {
		return fmt.Errorf("invalid map: %w", err)
	}

	if mailMap.Entries == nil {
		mailMap.Entries = make(map[string]interface{})
	}

	m.maps[mailMap.ID] = mailMap

	m.logger.Info("Created mail map",
		zap.String("id", mailMap.ID),
		zap.String("type", string(mailMap.Type)),
		zap.Int("entries", len(mailMap.Entries)))

	return nil
}

// GetMap retrieves a map by ID
func (m *MapManager) GetMap(id string) (*MailMap, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mailMap, exists := m.maps[id]
	if !exists {
		return nil, fmt.Errorf("map not found: %s", id)
	}

	return mailMap, nil
}

// UpdateMap updates an existing map
func (m *MapManager) UpdateMap(id string, mailMap *MailMap) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.maps[id]; !exists {
		return fmt.Errorf("map not found: %s", id)
	}

	if err := m.validateMap(mailMap); err != nil {
		return fmt.Errorf("invalid map: %w", err)
	}

	mailMap.ID = id
	m.maps[id] = mailMap

	m.logger.Info("Updated mail map", zap.String("id", id))

	return nil
}

// DeleteMap removes a map
func (m *MapManager) DeleteMap(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.maps[id]; !exists {
		return fmt.Errorf("map not found: %s", id)
	}

	delete(m.maps, id)

	m.logger.Info("Deleted mail map", zap.String("id", id))

	return nil
}

// ListMaps returns all maps
func (m *MapManager) ListMaps(tenantID string, mapType MapType) []*MailMap {
	m.mu.RLock()
	defer m.mu.RUnlock()

	maps := make([]*MailMap, 0)
	for _, mailMap := range m.maps {
		// Filter by tenant
		if tenantID != "" && mailMap.TenantID != tenantID {
			continue
		}
		// Filter by type
		if mapType != "" && mailMap.Type != mapType {
			continue
		}
		maps = append(maps, mailMap)
	}

	return maps
}

// Map Entry Management

// AddMapEntry adds an entry to a map
func (m *MapManager) AddMapEntry(mapID, key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mailMap, exists := m.maps[mapID]
	if !exists {
		return fmt.Errorf("map not found: %s", mapID)
	}

	if mailMap.Entries == nil {
		mailMap.Entries = make(map[string]interface{})
	}

	mailMap.Entries[key] = value

	m.logger.Info("Added map entry",
		zap.String("map_id", mapID),
		zap.String("key", key))

	return nil
}

// UpdateMapEntry updates an entry in a map
func (m *MapManager) UpdateMapEntry(mapID, key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mailMap, exists := m.maps[mapID]
	if !exists {
		return fmt.Errorf("map not found: %s", mapID)
	}

	if _, exists := mailMap.Entries[key]; !exists {
		return fmt.Errorf("entry not found: %s", key)
	}

	mailMap.Entries[key] = value

	m.logger.Info("Updated map entry",
		zap.String("map_id", mapID),
		zap.String("key", key))

	return nil
}

// DeleteMapEntry removes an entry from a map
func (m *MapManager) DeleteMapEntry(mapID, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mailMap, exists := m.maps[mapID]
	if !exists {
		return fmt.Errorf("map not found: %s", mapID)
	}

	if _, exists := mailMap.Entries[key]; !exists {
		return fmt.Errorf("entry not found: %s", key)
	}

	delete(mailMap.Entries, key)

	m.logger.Info("Deleted map entry",
		zap.String("map_id", mapID),
		zap.String("key", key))

	return nil
}

// GetMapEntry retrieves a single entry
func (m *MapManager) GetMapEntry(mapID, key string) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mailMap, exists := m.maps[mapID]
	if !exists {
		return nil, fmt.Errorf("map not found: %s", mapID)
	}

	value, exists := mailMap.Entries[key]
	if !exists {
		return nil, fmt.Errorf("entry not found: %s", key)
	}

	return value, nil
}

// LookupMap performs a lookup in a map (supports regexp and pattern matching)
func (m *MapManager) LookupMap(mapID, key string) (interface{}, error) {
	mailMap, err := m.GetMap(mapID)
	if err != nil {
		return nil, err
	}

	if !mailMap.Enabled {
		return nil, fmt.Errorf("map is disabled")
	}

	// Direct hash lookup
	if value, exists := mailMap.Entries[key]; exists {
		return value, nil
	}

	// For regexp maps, try pattern matching
	if mailMap.Type == MapTypeRegexp || mailMap.Type == MapTypePCRE {
		// TODO: Implement regexp matching
		return nil, fmt.Errorf("no match found")
	}

	return nil, fmt.Errorf("no match found")
}

// validateMap validates map configuration
func (m *MapManager) validateMap(mailMap *MailMap) error {
	if mailMap.Name == "" {
		return fmt.Errorf("map name is required")
	}

	if mailMap.Type == "" {
		return fmt.Errorf("map type is required")
	}

	// Type-specific validation
	switch mailMap.Type {
	case MapTypeMySQL, MapTypePostgreSQL, MapTypeSQLite:
		// Database maps should have connection config in entries
		// (We store connection details here for now)
	case MapTypeLDAP:
		// LDAP maps should have server config
	case MapTypeAlias, MapTypeVirtual:
		// Alias/virtual maps are simple key-value
	}

	return nil
}

// HTTP Handlers

// HandleCreateMap handles POST /api/v1/admin/maps
func (m *MapManager) HandleCreateMap() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var mailMap MailMap
		if err := json.NewDecoder(r.Body).Decode(&mailMap); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if mailMap.ID == "" {
			mailMap.ID = fmt.Sprintf("map-%d", time.Now().Unix())
		}

		if err := m.CreateMap(&mailMap); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(mailMap)
	}
}

// HandleGetMap handles GET /api/v1/admin/maps/{id}
func (m *MapManager) HandleGetMap() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		mailMap, err := m.GetMap(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mailMap)
	}
}

// HandleListMaps handles GET /api/v1/admin/maps
func (m *MapManager) HandleListMaps() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.URL.Query().Get("tenant_id")
		mapType := MapType(r.URL.Query().Get("type"))

		maps := m.ListMaps(tenantID, mapType)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(maps)
	}
}

// HandleUpdateMap handles PUT /api/v1/admin/maps/{id}
func (m *MapManager) HandleUpdateMap() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		var mailMap MailMap
		if err := json.NewDecoder(r.Body).Decode(&mailMap); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := m.UpdateMap(id, &mailMap); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mailMap)
	}
}

// HandleDeleteMap handles DELETE /api/v1/admin/maps/{id}
func (m *MapManager) HandleDeleteMap() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		if err := m.DeleteMap(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// Map Entry Handlers

// HandleAddMapEntry handles POST /api/v1/admin/maps/{id}/entries
func (m *MapManager) HandleAddMapEntry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		mapID := vars["id"]

		var entry MapEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := m.AddMapEntry(mapID, entry.Key, entry.Value); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(entry)
	}
}

// HandleUpdateMapEntry handles PUT /api/v1/admin/maps/{id}/entries/{key}
func (m *MapManager) HandleUpdateMapEntry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		mapID := vars["id"]
		key := vars["key"]

		var entry MapEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := m.UpdateMapEntry(mapID, key, entry.Value); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entry)
	}
}

// HandleDeleteMapEntry handles DELETE /api/v1/admin/maps/{id}/entries/{key}
func (m *MapManager) HandleDeleteMapEntry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		mapID := vars["id"]
		key := vars["key"]

		if err := m.DeleteMapEntry(mapID, key); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleLookupMap handles GET /api/v1/admin/maps/{id}/lookup/{key}
func (m *MapManager) HandleLookupMap() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		mapID := vars["id"]
		key := vars["key"]

		value, err := m.LookupMap(mapID, key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"key":   key,
			"value": value,
		})
	}
}
