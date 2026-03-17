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

// InterfaceManager manages mail service interfaces (master.cf style)
type InterfaceManager struct {
	logger        *zap.Logger
	interfaces    map[string]*MailInterface
	listenerMgr   *ListenerManager
	filterMgr     *FilterManager
	policyMgr     *PolicyManager
	mu            sync.RWMutex
}

// MailInterface represents a mail service interface (like master.cf entries)
type MailInterface struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`

	// Service configuration (master.cf style)
	ServiceType ServiceType `json:"service_type"`  // smtp, smtps, submission, lmtp, etc.
	PrivateService bool     `json:"private"`        // private=y/n in master.cf
	Unprivileged   bool     `json:"unprivileged"`   // unprivileged=y/n
	Chroot         bool     `json:"chroot"`         // chroot=y/n
	WakeupTime     *int     `json:"wakeup_time,omitempty"`  // wakeup time in seconds
	MaxProcesses   int      `json:"max_processes"`  // maxproc in master.cf

	// Bindings
	ListenerIDs  []string `json:"listener_ids"`   // Bound listeners
	PolicyIDs    []string `json:"policy_ids"`     // Bound policies
	FilterChainID string  `json:"filter_chain_id,omitempty"` // Bound filter chain

	// Process management
	ProcessLimit int    `json:"process_limit"`   // Max concurrent processes
	ProcessIdle  int    `json:"process_idle"`    // Idle before shutdown (seconds)

	// Multi-tenant
	TenantID     string `json:"tenant_id,omitempty"`

	// Status
	Enabled      bool   `json:"enabled"`
	Active       bool   `json:"active"`  // Currently running
}

// ServiceType defines the type of mail service
type ServiceType string

const (
	ServiceTypeSMTP       ServiceType = "smtp"        // Port 25
	ServiceTypeSMTPS      ServiceType = "smtps"       // Port 465
	ServiceTypeSubmission ServiceType = "submission"  // Port 587
	ServiceTypeLMTP       ServiceType = "lmtp"        // Local delivery
	ServiceTypePickup     ServiceType = "pickup"      // Pickup queue
	ServiceTypeCleanup    ServiceType = "cleanup"     // Cleanup daemon
	ServiceTypeQmgr       ServiceType = "qmgr"        // Queue manager
	ServiceTypeBounce     ServiceType = "bounce"      // Bounce daemon
	ServiceTypeDefer      ServiceType = "defer"       // Defer daemon
	ServiceTypeRewrite    ServiceType = "rewrite"     // Address rewrite
	ServiceTypeLocal      ServiceType = "local"       // Local delivery
	ServiceTypeVirtual    ServiceType = "virtual"     // Virtual delivery
	ServiceTypeRelay      ServiceType = "relay"       // Relay delivery
	ServiceTypeProxymap   ServiceType = "proxymap"    // Proxymap service
	ServiceTypeCustom     ServiceType = "custom"      // Custom service
)

// PolicyManager manages policies (referenced but defined elsewhere)
type PolicyManager struct {
	logger   *zap.Logger
	policies map[string]*Policy
	mu       sync.RWMutex
}

// Policy represents a mail policy
type Policy struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        PolicyType             `json:"type"`
	Config      map[string]interface{} `json:"config"`
	Enabled     bool                   `json:"enabled"`
	Description string                 `json:"description,omitempty"`
}

// PolicyType defines policy types
type PolicyType string

const (
	PolicyTypeRestriction PolicyType = "restriction"  // SMTP restrictions
	PolicyTypeRateLimit   PolicyType = "rate_limit"   // Rate limiting
	PolicyTypeQuota       PolicyType = "quota"        // Quotas
	PolicyTypeRouting     PolicyType = "routing"      // Routing rules
	PolicyTypeRewrite     PolicyType = "rewrite"      // Address rewriting
	PolicyTypeAccess      PolicyType = "access"       // Access control
	PolicyTypeStarlark    PolicyType = "starlark"     // Starlark script
)

// NewInterfaceManager creates a new interface manager
func NewInterfaceManager(
	logger *zap.Logger,
	listenerMgr *ListenerManager,
	filterMgr *FilterManager,
	policyMgr *PolicyManager,
) *InterfaceManager {
	return &InterfaceManager{
		logger:      logger,
		interfaces:  make(map[string]*MailInterface),
		listenerMgr: listenerMgr,
		filterMgr:   filterMgr,
		policyMgr:   policyMgr,
	}
}

// CreateInterface creates a new mail interface
func (m *InterfaceManager) CreateInterface(iface *MailInterface) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.interfaces[iface.ID]; exists {
		return fmt.Errorf("interface with ID %s already exists", iface.ID)
	}

	if err := m.validateInterface(iface); err != nil {
		return fmt.Errorf("invalid interface: %w", err)
	}

	m.interfaces[iface.ID] = iface

	m.logger.Info("Created mail interface",
		zap.String("id", iface.ID),
		zap.String("type", string(iface.ServiceType)),
		zap.Int("listeners", len(iface.ListenerIDs)),
		zap.Int("policies", len(iface.PolicyIDs)))

	return nil
}

// GetInterface retrieves an interface by ID
func (m *InterfaceManager) GetInterface(id string) (*MailInterface, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	iface, exists := m.interfaces[id]
	if !exists {
		return nil, fmt.Errorf("interface not found: %s", id)
	}

	return iface, nil
}

// UpdateInterface updates an existing interface
func (m *InterfaceManager) UpdateInterface(id string, iface *MailInterface) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.interfaces[id]; !exists {
		return fmt.Errorf("interface not found: %s", id)
	}

	if err := m.validateInterface(iface); err != nil {
		return fmt.Errorf("invalid interface: %w", err)
	}

	iface.ID = id
	m.interfaces[id] = iface

	m.logger.Info("Updated mail interface", zap.String("id", id))

	return nil
}

// DeleteInterface removes an interface
func (m *InterfaceManager) DeleteInterface(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	iface, exists := m.interfaces[id]
	if !exists {
		return fmt.Errorf("interface not found: %s", id)
	}

	if iface.Active {
		return fmt.Errorf("cannot delete active interface")
	}

	delete(m.interfaces, id)

	m.logger.Info("Deleted mail interface", zap.String("id", id))

	return nil
}

// ListInterfaces returns all interfaces
func (m *InterfaceManager) ListInterfaces(tenantID string) []*MailInterface {
	m.mu.RLock()
	defer m.mu.RUnlock()

	interfaces := make([]*MailInterface, 0)
	for _, iface := range m.interfaces {
		if tenantID != "" && iface.TenantID != tenantID {
			continue
		}
		interfaces = append(interfaces, iface)
	}

	return interfaces
}

// Binding Management

// BindListener binds a listener to an interface
func (m *InterfaceManager) BindListener(interfaceID, listenerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	iface, exists := m.interfaces[interfaceID]
	if !exists {
		return fmt.Errorf("interface not found: %s", interfaceID)
	}

	// Verify listener exists
	if _, err := m.listenerMgr.GetListener(listenerID); err != nil {
		return fmt.Errorf("listener not found: %s", listenerID)
	}

	// Check if already bound
	for _, id := range iface.ListenerIDs {
		if id == listenerID {
			return fmt.Errorf("listener already bound")
		}
	}

	iface.ListenerIDs = append(iface.ListenerIDs, listenerID)

	m.logger.Info("Bound listener to interface",
		zap.String("interface_id", interfaceID),
		zap.String("listener_id", listenerID))

	return nil
}

// UnbindListener unbinds a listener from an interface
func (m *InterfaceManager) UnbindListener(interfaceID, listenerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	iface, exists := m.interfaces[interfaceID]
	if !exists {
		return fmt.Errorf("interface not found: %s", interfaceID)
	}

	// Remove listener
	newListeners := make([]string, 0)
	found := false
	for _, id := range iface.ListenerIDs {
		if id == listenerID {
			found = true
			continue
		}
		newListeners = append(newListeners, id)
	}

	if !found {
		return fmt.Errorf("listener not bound to interface")
	}

	iface.ListenerIDs = newListeners

	m.logger.Info("Unbound listener from interface",
		zap.String("interface_id", interfaceID),
		zap.String("listener_id", listenerID))

	return nil
}

// BindPolicy binds a policy to an interface
func (m *InterfaceManager) BindPolicy(interfaceID, policyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	iface, exists := m.interfaces[interfaceID]
	if !exists {
		return fmt.Errorf("interface not found: %s", interfaceID)
	}

	// Verify policy exists
	if _, err := m.policyMgr.GetPolicy(policyID); err != nil {
		return fmt.Errorf("policy not found: %s", policyID)
	}

	// Check if already bound
	for _, id := range iface.PolicyIDs {
		if id == policyID {
			return fmt.Errorf("policy already bound")
		}
	}

	iface.PolicyIDs = append(iface.PolicyIDs, policyID)

	m.logger.Info("Bound policy to interface",
		zap.String("interface_id", interfaceID),
		zap.String("policy_id", policyID))

	return nil
}

// UnbindPolicy unbinds a policy from an interface
func (m *InterfaceManager) UnbindPolicy(interfaceID, policyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	iface, exists := m.interfaces[interfaceID]
	if !exists {
		return fmt.Errorf("interface not found: %s", interfaceID)
	}

	// Remove policy
	newPolicies := make([]string, 0)
	found := false
	for _, id := range iface.PolicyIDs {
		if id == policyID {
			found = true
			continue
		}
		newPolicies = append(newPolicies, id)
	}

	if !found {
		return fmt.Errorf("policy not bound to interface")
	}

	iface.PolicyIDs = newPolicies

	m.logger.Info("Unbound policy from interface",
		zap.String("interface_id", interfaceID),
		zap.String("policy_id", policyID))

	return nil
}

// BindFilterChain binds a filter chain to an interface
func (m *InterfaceManager) BindFilterChain(interfaceID, filterChainID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	iface, exists := m.interfaces[interfaceID]
	if !exists {
		return fmt.Errorf("interface not found: %s", interfaceID)
	}

	// Verify filter chain exists
	if _, err := m.filterMgr.GetFilterChain(filterChainID); err != nil {
		return fmt.Errorf("filter chain not found: %s", filterChainID)
	}

	iface.FilterChainID = filterChainID

	m.logger.Info("Bound filter chain to interface",
		zap.String("interface_id", interfaceID),
		zap.String("filter_chain_id", filterChainID))

	return nil
}

// validateInterface validates interface configuration
func (m *InterfaceManager) validateInterface(iface *MailInterface) error {
	if iface.Name == "" {
		return fmt.Errorf("interface name is required")
	}

	if iface.ServiceType == "" {
		return fmt.Errorf("service type is required")
	}

	// Validate listeners exist
	for _, listenerID := range iface.ListenerIDs {
		if _, err := m.listenerMgr.GetListener(listenerID); err != nil {
			return fmt.Errorf("listener %s not found", listenerID)
		}
	}

	// Validate policies exist
	for _, policyID := range iface.PolicyIDs {
		if _, err := m.policyMgr.GetPolicy(policyID); err != nil {
			return fmt.Errorf("policy %s not found", policyID)
		}
	}

	// Validate filter chain exists
	if iface.FilterChainID != "" {
		if _, err := m.filterMgr.GetFilterChain(iface.FilterChainID); err != nil {
			return fmt.Errorf("filter chain %s not found", iface.FilterChainID)
		}
	}

	// Set defaults
	if iface.MaxProcesses == 0 {
		iface.MaxProcesses = 100
	}
	if iface.ProcessLimit == 0 {
		iface.ProcessLimit = 100
	}
	if iface.ProcessIdle == 0 {
		iface.ProcessIdle = 100
	}

	return nil
}

// GetInterfaceDetails returns interface with full details of bound resources
func (m *InterfaceManager) GetInterfaceDetails(id string) (map[string]interface{}, error) {
	iface, err := m.GetInterface(id)
	if err != nil {
		return nil, err
	}

	// Get bound listeners
	listeners := make([]*ListenerConfig, 0)
	for _, listenerID := range iface.ListenerIDs {
		if listener, err := m.listenerMgr.GetListener(listenerID); err == nil {
			listeners = append(listeners, listener)
		}
	}

	// Get bound policies
	policies := make([]*Policy, 0)
	for _, policyID := range iface.PolicyIDs {
		if policy, err := m.policyMgr.GetPolicy(policyID); err == nil {
			policies = append(policies, policy)
		}
	}

	// Get filter chain
	var filterChain *FilterChain
	if iface.FilterChainID != "" {
		filterChain, _ = m.filterMgr.GetFilterChain(iface.FilterChainID)
	}

	return map[string]interface{}{
		"interface":    iface,
		"listeners":    listeners,
		"policies":     policies,
		"filter_chain": filterChain,
	}, nil
}

// HTTP Handlers

// HandleCreateInterface handles POST /api/v1/admin/interfaces
func (m *InterfaceManager) HandleCreateInterface() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var iface MailInterface
		if err := json.NewDecoder(r.Body).Decode(&iface); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if iface.ID == "" {
			iface.ID = fmt.Sprintf("iface-%d", time.Now().Unix())
		}

		if err := m.CreateInterface(&iface); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(iface)
	}
}

// HandleGetInterface handles GET /api/v1/admin/interfaces/{id}
func (m *InterfaceManager) HandleGetInterface() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		// Check if detailed view is requested
		if r.URL.Query().Get("details") == "true" {
			details, err := m.GetInterfaceDetails(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(details)
			return
		}

		iface, err := m.GetInterface(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(iface)
	}
}

// HandleListInterfaces handles GET /api/v1/admin/interfaces
func (m *InterfaceManager) HandleListInterfaces() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.URL.Query().Get("tenant_id")
		interfaces := m.ListInterfaces(tenantID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(interfaces)
	}
}

// HandleUpdateInterface handles PUT /api/v1/admin/interfaces/{id}
func (m *InterfaceManager) HandleUpdateInterface() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		var iface MailInterface
		if err := json.NewDecoder(r.Body).Decode(&iface); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := m.UpdateInterface(id, &iface); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(iface)
	}
}

// HandleDeleteInterface handles DELETE /api/v1/admin/interfaces/{id}
func (m *InterfaceManager) HandleDeleteInterface() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		if err := m.DeleteInterface(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// Binding Handlers

// HandleBindListener handles POST /api/v1/admin/interfaces/{id}/listeners/{listener_id}
func (m *InterfaceManager) HandleBindListener() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		interfaceID := vars["id"]
		listenerID := vars["listener_id"]

		if err := m.BindListener(interfaceID, listenerID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleUnbindListener handles DELETE /api/v1/admin/interfaces/{id}/listeners/{listener_id}
func (m *InterfaceManager) HandleUnbindListener() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		interfaceID := vars["id"]
		listenerID := vars["listener_id"]

		if err := m.UnbindListener(interfaceID, listenerID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleBindPolicy handles POST /api/v1/admin/interfaces/{id}/policies/{policy_id}
func (m *InterfaceManager) HandleBindPolicy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		interfaceID := vars["id"]
		policyID := vars["policy_id"]

		if err := m.BindPolicy(interfaceID, policyID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleUnbindPolicy handles DELETE /api/v1/admin/interfaces/{id}/policies/{policy_id}
func (m *InterfaceManager) HandleUnbindPolicy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		interfaceID := vars["id"]
		policyID := vars["policy_id"]

		if err := m.UnbindPolicy(interfaceID, policyID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// PolicyManager methods (stub - implement fully in policies.go)

func NewPolicyManager(logger *zap.Logger) *PolicyManager {
	return &PolicyManager{
		logger:   logger,
		policies: make(map[string]*Policy),
	}
}

func (m *PolicyManager) GetPolicy(id string) (*Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	policy, exists := m.policies[id]
	if !exists {
		return nil, fmt.Errorf("policy not found: %s", id)
	}

	return policy, nil
}
