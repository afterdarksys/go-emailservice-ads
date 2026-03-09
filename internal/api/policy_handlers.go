package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
)

// PolicyManager must be added to Server struct
// Add this field to the Server struct in api/server.go

// handlePolicyList returns all configured policies
func (s *Server) handlePolicyList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.policyMgr == nil {
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"policies": []policy.PolicyConfig{},
			"count":    0,
		})
		return
	}

	policies := s.policyMgr.ListPolicies()

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"policies": policies,
		"count":    len(policies),
	})
}

// handlePolicyGet returns details of a specific policy
func (s *Server) handlePolicyGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract policy name from URL path
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/policies/")

	// TODO: Get policy by name from policy manager
	// policy, err := s.policyManager.GetPolicy(name)

	_ = name // Use the name variable

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"name":    name,
		"message": "Policy details not yet implemented",
	})
}

// handlePolicyCreate creates a new policy
func (s *Server) handlePolicyCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var policyConfig policy.PolicyConfig
	if err := json.NewDecoder(r.Body).Decode(&policyConfig); err != nil {
		s.logger.Warn("Invalid policy request", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// TODO: Add policy to policy manager
	// err := s.policyManager.AddPolicy(&policyConfig)
	// if err != nil {
	//     http.Error(w, err.Error(), http.StatusBadRequest)
	//     return
	// }

	s.logger.Info("Policy created", zap.String("name", policyConfig.Name))
	s.jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"status":  "created",
		"policy":  policyConfig.Name,
		"message": "Policy creation not yet fully implemented",
	})
}

// handlePolicyUpdate updates an existing policy
func (s *Server) handlePolicyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/v1/policies/")

	var policyConfig policy.PolicyConfig
	if err := json.NewDecoder(r.Body).Decode(&policyConfig); err != nil {
		s.logger.Warn("Invalid policy update request", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Ensure name matches URL
	policyConfig.Name = name

	// TODO: Update policy in policy manager
	// err := s.policyManager.UpdatePolicy(&policyConfig)

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":  "updated",
		"policy":  name,
		"message": "Policy update not yet fully implemented",
	})
}

// handlePolicyDelete deletes a policy
func (s *Server) handlePolicyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/v1/policies/")

	// TODO: Delete policy from policy manager
	// err := s.policyManager.RemovePolicy(name)

	s.logger.Info("Policy deleted", zap.String("name", name))
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":  "deleted",
		"policy":  name,
		"message": "Policy deletion not yet fully implemented",
	})
}

// handlePolicyTest tests a policy against a sample email
func (s *Server) handlePolicyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/v1/policies/")
	name = strings.TrimSuffix(name, "/test")

	type TestRequest struct {
		From    string            `json:"from"`
		To      []string          `json:"to"`
		Subject string            `json:"subject"`
		Body    string            `json:"body"`
		Headers map[string]string `json:"headers"`
	}

	var testReq TestRequest
	if err := json.NewDecoder(r.Body).Decode(&testReq); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// TODO: Test policy against sample email
	// result, err := s.policyManager.TestPolicy(name, emailContext)

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"policy": name,
		"result": "Policy testing not yet fully implemented",
		"input": map[string]interface{}{
			"from":    testReq.From,
			"to":      testReq.To,
			"subject": testReq.Subject,
		},
	})
}

// handlePolicyReload reloads all policies from configuration
func (s *Server) handlePolicyReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.policyMgr == nil {
		http.Error(w, "Policy manager not configured", http.StatusNotImplemented)
		return
	}

	if err := s.policyMgr.Reload(); err != nil {
		s.logger.Error("Failed to reload policies", zap.Error(err))
		http.Error(w, "Failed to reload policies: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.logger.Info("Policies reloaded successfully")
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":  "reloaded",
		"message": "Policies reloaded successfully",
	})
}

// handlePolicyStats returns policy engine statistics
func (s *Server) handlePolicyStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.policyMgr == nil {
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"policies":    0,
			"evaluations": 0,
			"errors":      0,
			"cache_size":  0,
		})
		return
	}

	stats := s.policyMgr.GetStats()

	s.jsonResponse(w, http.StatusOK, stats)
}
