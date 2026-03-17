package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/directory"
)

// NextMailHopHandler handles nextmailhop API requests
type NextMailHopHandler struct {
	logger   *zap.Logger
	resolver *directory.NextMailHopResolver
}

// NewNextMailHopHandler creates a new nextmailhop handler
func NewNextMailHopHandler(logger *zap.Logger, resolver *directory.NextMailHopResolver) *NextMailHopHandler {
	return &NextMailHopHandler{
		logger:   logger,
		resolver: resolver,
	}
}

// SetNextHopRequest represents a request to set nextmailhop
type SetNextHopRequest struct {
	Email   string `json:"email"`
	NextHop string `json:"next_hop"`
}

// BulkSetNextHopRequest represents a bulk set request
type BulkSetNextHopRequest struct {
	Mappings map[string]string `json:"mappings"`
}

// HandleGetNextHop handles GET /api/v1/admin/users/{email}/nextmailhop
func (h *NextMailHopHandler) HandleGetNextHop() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		email := vars["email"]

		nextHop, err := h.resolver.ResolveNextHop(email)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"email":    email,
			"next_hop": nextHop,
		})
	}
}

// HandleSetNextHop handles POST /api/v1/admin/users/{email}/nextmailhop
func (h *NextMailHopHandler) HandleSetNextHop() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		email := vars["email"]

		var req SetNextHopRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Use email from URL if not in body
		if req.Email == "" {
			req.Email = email
		}

		// Validate next hop format
		if err := h.resolver.ValidateNextHop(req.NextHop); err != nil {
			http.Error(w, "Invalid next_hop format: "+err.Error(), http.StatusBadRequest)
			return
		}

		if err := h.resolver.SetNextHop(req.Email, req.NextHop); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"email":    req.Email,
			"next_hop": req.NextHop,
			"status":   "set",
		})
	}
}

// HandleRemoveNextHop handles DELETE /api/v1/admin/users/{email}/nextmailhop
func (h *NextMailHopHandler) HandleRemoveNextHop() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		email := vars["email"]

		if err := h.resolver.RemoveNextHop(email); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleListUsersWithNextHop handles GET /api/v1/admin/nextmailhop
func (h *NextMailHopHandler) HandleListUsersWithNextHop() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := h.resolver.ListUsersWithNextHop()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
	}
}

// HandleBulkSetNextHop handles POST /api/v1/admin/nextmailhop/bulk
func (h *NextMailHopHandler) HandleBulkSetNextHop() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req BulkSetNextHopRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate all next hops first
		for _, nextHop := range req.Mappings {
			if err := h.resolver.ValidateNextHop(nextHop); err != nil {
				http.Error(w, "Invalid next_hop format: "+err.Error(), http.StatusBadRequest)
				return
			}
		}

		if err := h.resolver.BulkSetNextHop(req.Mappings); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"count":  len(req.Mappings),
			"status": "set",
		})
	}
}

// HandleGetUserRoutingInfo handles GET /api/v1/admin/users/{email}/routing
func (h *NextMailHopHandler) HandleGetUserRoutingInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		email := vars["email"]

		info, err := h.resolver.GetUserRoutingInfo(email)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}
