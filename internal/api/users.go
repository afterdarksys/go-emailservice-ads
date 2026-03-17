package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
)

// UserManager manages user accounts and entitlements
type UserManager struct {
	logger     *zap.Logger
	userStore  *auth.UserStore
	repository *auth.UserRepository
}

// NewUserManager creates a new user manager
func NewUserManager(logger *zap.Logger, userStore *auth.UserStore, repository *auth.UserRepository) *UserManager {
	return &UserManager{
		logger:     logger,
		userStore:  userStore,
		repository: repository,
	}
}

// UserRequest represents a user creation/update request
type UserRequest struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	Email    string `json:"email"`
	Enabled  bool   `json:"enabled"`
}

// UserResponse represents a user in API responses
type UserResponse struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Enabled  bool   `json:"enabled"`
}

// DomainEntitlementRequest represents a domain entitlement grant/revoke request
type DomainEntitlementRequest struct {
	Domain    string `json:"domain"`
	GrantedBy string `json:"granted_by,omitempty"`
	Notes     string `json:"notes,omitempty"`
}

// HandleCreateUser handles POST /api/v1/admin/users
func (m *UserManager) HandleCreateUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Username == "" || req.Password == "" || req.Email == "" {
			http.Error(w, "username, password, and email are required", http.StatusBadRequest)
			return
		}

		if err := m.userStore.AddUser(req.Username, req.Password, req.Email); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(UserResponse{
			Username: req.Username,
			Email:    req.Email,
			Enabled:  true,
		})
	}
}

// HandleListUsers handles GET /api/v1/admin/users
func (m *UserManager) HandleListUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		users, err := m.repository.ListUsers(ctx)
		if err != nil {
			m.logger.Error("Failed to list users", zap.Error(err))
			http.Error(w, "Failed to list users", http.StatusInternalServerError)
			return
		}

		responses := make([]UserResponse, len(users))
		for i, user := range users {
			responses[i] = UserResponse{
				Username: user.Username,
				Email:    user.Email,
				Enabled:  user.Enabled,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responses)
	}
}

// HandleGetUser handles GET /api/v1/admin/users/{username}
func (m *UserManager) HandleGetUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		username := vars["username"]
		ctx := r.Context()

		user, err := m.repository.GetUser(ctx, username)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UserResponse{
			Username: user.Username,
			Email:    user.Email,
			Enabled:  user.Enabled,
		})
	}
}

// HandleDeleteUser handles DELETE /api/v1/admin/users/{username}
func (m *UserManager) HandleDeleteUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		username := vars["username"]
		ctx := r.Context()

		if err := m.repository.DeleteUser(ctx, username); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleGrantDomainEntitlement handles POST /api/v1/admin/users/{username}/domains
func (m *UserManager) HandleGrantDomainEntitlement() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		username := vars["username"]
		ctx := r.Context()

		var req DomainEntitlementRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Domain == "" {
			http.Error(w, "domain is required", http.StatusBadRequest)
			return
		}

		// Get admin user who is granting this entitlement
		grantedBy := req.GrantedBy
		if grantedBy == "" {
			if key, ok := AdminKeyFromContext(r.Context()); ok {
				grantedBy = key.Name
			}
		}

		if err := m.repository.GrantDomainEntitlement(ctx, username, req.Domain, grantedBy, req.Notes); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{
			"username": username,
			"domain":   req.Domain,
			"status":   "granted",
		})
	}
}

// HandleRevokeDomainEntitlement handles DELETE /api/v1/admin/users/{username}/domains/{domain}
func (m *UserManager) HandleRevokeDomainEntitlement() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		username := vars["username"]
		domain := vars["domain"]
		ctx := r.Context()

		if err := m.repository.RevokeDomainEntitlement(ctx, username, domain); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleListUserDomainEntitlements handles GET /api/v1/admin/users/{username}/domains
func (m *UserManager) HandleListUserDomainEntitlements() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		username := vars["username"]
		ctx := r.Context()

		domains, err := m.repository.GetUserDomainEntitlements(ctx, username)
		if err != nil {
			m.logger.Error("Failed to get domain entitlements", zap.Error(err))
			http.Error(w, "Failed to get domain entitlements", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"username": username,
			"domains":  domains,
		})
	}
}

// HandleListAllDomainEntitlements handles GET /api/v1/admin/domain-entitlements
func (m *UserManager) HandleListAllDomainEntitlements() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		entitlements, err := m.repository.ListDomainEntitlements(ctx)
		if err != nil {
			m.logger.Error("Failed to list domain entitlements", zap.Error(err))
			http.Error(w, "Failed to list domain entitlements", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entitlements)
	}
}

// HandleSetUserQuota handles PUT /api/v1/admin/users/{username}/quota
func (m *UserManager) HandleSetUserQuota() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		username := vars["username"]
		ctx := r.Context()

		var quota auth.UserQuota
		if err := json.NewDecoder(r.Body).Decode(&quota); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := m.repository.SetUserQuota(ctx, username, &quota); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"username": username,
			"status":   "quota updated",
		})
	}
}

// HandleGetUserQuota handles GET /api/v1/admin/users/{username}/quota
func (m *UserManager) HandleGetUserQuota() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		username := vars["username"]
		ctx := r.Context()

		quota, err := m.repository.GetUserQuota(ctx, username)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(quota)
	}
}
