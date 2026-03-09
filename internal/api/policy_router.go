package api

import (
	"net/http"
	"strings"
)

// handlePolicyRouter routes policy requests to the appropriate handler
func (s *Server) handlePolicyRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/policies/")

	// Handle special endpoints
	if path == "stats" {
		s.handlePolicyStats(w, r)
		return
	}

	if path == "reload" {
		s.handlePolicyReload(w, r)
		return
	}

	// Extract policy name and action
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		// List all policies
		s.handlePolicyList(w, r)
		return
	}

	policyName := parts[0]

	// Check for test endpoint
	if len(parts) > 1 && parts[1] == "test" {
		s.handlePolicyTest(w, r)
		return
	}

	// Route based on HTTP method
	switch r.Method {
	case http.MethodGet:
		s.handlePolicyGet(w, r)
	case http.MethodPost:
		s.handlePolicyCreate(w, r)
	case http.MethodPut:
		s.handlePolicyUpdate(w, r)
	case http.MethodDelete:
		s.handlePolicyDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}

	_ = policyName // Use the variable
}
