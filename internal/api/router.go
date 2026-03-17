package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// AdminAPI encapsulates all admin API components
type AdminAPI struct {
	Router          *mux.Router
	KeyManager      *AdminKeyManager
	TenantManager   *TenantManager
	AliasManager    *AliasManager
	UserManager     *UserManager
	ListenerManager *ListenerManager
	FilterManager   *FilterManager
	MapManager      *MapManager
	InterfaceManager *InterfaceManager
	PolicyManager   *PolicyManager
	NextMailHopHandler *NextMailHopHandler
	logger          *zap.Logger
}

// NewAdminAPI creates a new admin API
func NewAdminAPI(logger *zap.Logger, nextMailHopHandler *NextMailHopHandler) *AdminAPI {
	api := &AdminAPI{
		Router:          mux.NewRouter(),
		KeyManager:      NewAdminKeyManager(logger),
		TenantManager:   NewTenantManager(logger),
		AliasManager:    NewAliasManager(logger),
		ListenerManager: NewListenerManager(logger),
		FilterManager:   NewFilterManager(logger),
		MapManager:      NewMapManager(logger),
		PolicyManager:   NewPolicyManager(logger),
		NextMailHopHandler: nextMailHopHandler,
		logger:          logger,
	}

	// Initialize interface manager with dependencies
	api.InterfaceManager = NewInterfaceManager(
		logger,
		api.ListenerManager,
		api.FilterManager,
		api.PolicyManager,
	)

	// Setup routes
	api.setupRoutes()

	return api
}

// setupRoutes configures all API routes
func (api *AdminAPI) setupRoutes() {
	// API v1 router
	v1 := api.Router.PathPrefix("/api/v1").Subrouter()

	// Public endpoints (no auth required)
	v1.HandleFunc("/health", api.handleHealth()).Methods("GET")
	v1.HandleFunc("/version", api.handleVersion()).Methods("GET")

	// Admin endpoints (require authentication)
	admin := v1.PathPrefix("/admin").Subrouter()

	// Apply admin authentication middleware to all admin routes
	// Note: Generate initial admin key with: go run cmd/adsadmin/main.go generate-key
	admin.Use(AdminAuthMiddleware(api.KeyManager, PermissionAll))

	// Listener Management
	admin.HandleFunc("/listeners", api.ListenerManager.HandleListListeners()).Methods("GET")
	admin.HandleFunc("/listeners", api.ListenerManager.HandleCreateListener(api.KeyManager)).Methods("POST")
	admin.HandleFunc("/listeners/{id}", api.ListenerManager.HandleGetListener()).Methods("GET")
	admin.HandleFunc("/listeners/{id}", api.ListenerManager.HandleUpdateListener()).Methods("PUT")
	admin.HandleFunc("/listeners/{id}", api.ListenerManager.HandleDeleteListener()).Methods("DELETE")

	// Filter Management
	admin.HandleFunc("/filters", api.FilterManager.HandleListFilters()).Methods("GET")
	admin.HandleFunc("/filters", api.FilterManager.HandleCreateFilter()).Methods("POST")
	admin.HandleFunc("/filters/{id}", api.FilterManager.HandleGetFilter()).Methods("GET")
	admin.HandleFunc("/filters/{id}", api.FilterManager.HandleUpdateFilter()).Methods("PUT")
	admin.HandleFunc("/filters/{id}", api.FilterManager.HandleDeleteFilter()).Methods("DELETE")

	// Filter Chain Management
	admin.HandleFunc("/filter-chains", api.FilterManager.HandleListFilterChains()).Methods("GET")
	admin.HandleFunc("/filter-chains", api.FilterManager.HandleCreateFilterChain()).Methods("POST")
	admin.HandleFunc("/filter-chains/{id}", api.FilterManager.HandleGetFilterChain()).Methods("GET")
	admin.HandleFunc("/filter-chains/{id}", api.FilterManager.HandleUpdateFilterChain()).Methods("PUT")
	admin.HandleFunc("/filter-chains/{id}", api.FilterManager.HandleDeleteFilterChain()).Methods("DELETE")

	// Map Management
	admin.HandleFunc("/maps", api.MapManager.HandleListMaps()).Methods("GET")
	admin.HandleFunc("/maps", api.MapManager.HandleCreateMap()).Methods("POST")
	admin.HandleFunc("/maps/{id}", api.MapManager.HandleGetMap()).Methods("GET")
	admin.HandleFunc("/maps/{id}", api.MapManager.HandleUpdateMap()).Methods("PUT")
	admin.HandleFunc("/maps/{id}", api.MapManager.HandleDeleteMap()).Methods("DELETE")

	// Map Entry Management
	admin.HandleFunc("/maps/{id}/entries", api.MapManager.HandleAddMapEntry()).Methods("POST")
	admin.HandleFunc("/maps/{id}/entries/{key}", api.MapManager.HandleUpdateMapEntry()).Methods("PUT")
	admin.HandleFunc("/maps/{id}/entries/{key}", api.MapManager.HandleDeleteMapEntry()).Methods("DELETE")
	admin.HandleFunc("/maps/{id}/lookup/{key}", api.MapManager.HandleLookupMap()).Methods("GET")

	// Interface Management (master.cf style)
	admin.HandleFunc("/interfaces", api.InterfaceManager.HandleListInterfaces()).Methods("GET")
	admin.HandleFunc("/interfaces", api.InterfaceManager.HandleCreateInterface()).Methods("POST")
	admin.HandleFunc("/interfaces/{id}", api.InterfaceManager.HandleGetInterface()).Methods("GET")
	admin.HandleFunc("/interfaces/{id}", api.InterfaceManager.HandleUpdateInterface()).Methods("PUT")
	admin.HandleFunc("/interfaces/{id}", api.InterfaceManager.HandleDeleteInterface()).Methods("DELETE")

	// Interface Bindings
	admin.HandleFunc("/interfaces/{id}/listeners/{listener_id}", api.InterfaceManager.HandleBindListener()).Methods("POST")
	admin.HandleFunc("/interfaces/{id}/listeners/{listener_id}", api.InterfaceManager.HandleUnbindListener()).Methods("DELETE")
	admin.HandleFunc("/interfaces/{id}/policies/{policy_id}", api.InterfaceManager.HandleBindPolicy()).Methods("POST")
	admin.HandleFunc("/interfaces/{id}/policies/{policy_id}", api.InterfaceManager.HandleUnbindPolicy()).Methods("DELETE")

	// NextMailHop Management (LDAP Routing)
	admin.HandleFunc("/users/{email}/nextmailhop", api.NextMailHopHandler.HandleGetNextHop()).Methods("GET")
	admin.HandleFunc("/users/{email}/nextmailhop", api.NextMailHopHandler.HandleSetNextHop()).Methods("POST")
	admin.HandleFunc("/users/{email}/nextmailhop", api.NextMailHopHandler.HandleRemoveNextHop()).Methods("DELETE")
	admin.HandleFunc("/users/{email}/routing", api.NextMailHopHandler.HandleGetUserRoutingInfo()).Methods("GET")
	admin.HandleFunc("/nextmailhop", api.NextMailHopHandler.HandleListUsersWithNextHop()).Methods("GET")
	admin.HandleFunc("/nextmailhop/bulk", api.NextMailHopHandler.HandleBulkSetNextHop()).Methods("POST")

	// Tenant Management (Multi-tenancy)
	admin.HandleFunc("/tenants", api.TenantManager.HandleListTenants()).Methods("GET")
	admin.HandleFunc("/tenants", api.TenantManager.HandleCreateTenant()).Methods("POST")
	admin.HandleFunc("/tenants/{id}", api.TenantManager.HandleGetTenant()).Methods("GET")
	admin.HandleFunc("/tenants/{id}", api.TenantManager.HandleUpdateTenant()).Methods("PUT")
	admin.HandleFunc("/tenants/{id}", api.TenantManager.HandleDeleteTenant()).Methods("DELETE")

	// Alias Management (Email Forwarding)
	admin.HandleFunc("/aliases", api.AliasManager.HandleListAliases()).Methods("GET")
	admin.HandleFunc("/aliases", api.AliasManager.HandleCreateAlias()).Methods("POST")
	admin.HandleFunc("/aliases/{id}", api.AliasManager.HandleGetAlias()).Methods("GET")
	admin.HandleFunc("/aliases/{id}", api.AliasManager.HandleUpdateAlias()).Methods("PUT")
	admin.HandleFunc("/aliases/{id}", api.AliasManager.HandleDeleteAlias()).Methods("DELETE")

	// User Management (if UserManager is configured)
	if api.UserManager != nil {
		admin.HandleFunc("/users", api.UserManager.HandleListUsers()).Methods("GET")
		admin.HandleFunc("/users", api.UserManager.HandleCreateUser()).Methods("POST")
		admin.HandleFunc("/users/{username}", api.UserManager.HandleGetUser()).Methods("GET")
		admin.HandleFunc("/users/{username}", api.UserManager.HandleDeleteUser()).Methods("DELETE")

		// Domain entitlements
		admin.HandleFunc("/users/{username}/domains", api.UserManager.HandleListUserDomainEntitlements()).Methods("GET")
		admin.HandleFunc("/users/{username}/domains", api.UserManager.HandleGrantDomainEntitlement()).Methods("POST")
		admin.HandleFunc("/users/{username}/domains/{domain}", api.UserManager.HandleRevokeDomainEntitlement()).Methods("DELETE")
		admin.HandleFunc("/domain-entitlements", api.UserManager.HandleListAllDomainEntitlements()).Methods("GET")

		// User quotas
		admin.HandleFunc("/users/{username}/quota", api.UserManager.HandleGetUserQuota()).Methods("GET")
		admin.HandleFunc("/users/{username}/quota", api.UserManager.HandleSetUserQuota()).Methods("PUT")
	}

	// API Key Management (super admin only)
	// admin.HandleFunc("/api-keys", api.handleListAPIKeys()).Methods("GET")
	// admin.HandleFunc("/api-keys", api.handleCreateAPIKey()).Methods("POST")
	// admin.HandleFunc("/api-keys/{key}", api.handleRevokeAPIKey()).Methods("DELETE")

	api.logger.Info("Admin API routes configured")
}

// Health endpoint
func (api *AdminAPI) handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"admin-api"}`))
	}
}

// Version endpoint
func (api *AdminAPI) handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"2.3.0","service":"go-emailservice-ads"}`))
	}
}

// GenerateInitialAdminKey generates the first admin API key
func (api *AdminAPI) GenerateInitialAdminKey() (*AdminKey, error) {
	key, err := api.KeyManager.GenerateKey(
		"Initial Admin Key",
		[]Permission{PermissionAll},
		nil, // No expiration
	)
	if err != nil {
		return nil, err
	}

	api.logger.Info("Generated initial admin API key",
		zap.String("key_prefix", key.Key[:12]+"..."))

	return key, nil
}

// ListenAndServe starts the admin API server
func (api *AdminAPI) ListenAndServe(addr string) error {
	api.logger.Info("Starting Admin API server", zap.String("addr", addr))

	server := &http.Server{
		Addr:    addr,
		Handler: api.Router,
	}

	return server.ListenAndServe()
}
