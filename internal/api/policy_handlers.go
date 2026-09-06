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

func (s *Server) policyAvailable(w http.ResponseWriter) bool {
	if s.policyMgr == nil {
		http.Error(w, "Policy manager unavailable", 503)
		return false
	}
	return true
}
func (s *Server) handlePolicyGet(w http.ResponseWriter, r *http.Request) {
	if !s.policyAvailable(w) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/policies/")
	p, err := s.policyMgr.GetPolicy(name)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	s.jsonResponse(w, 200, p)
}
func (s *Server) handlePolicyCreate(w http.ResponseWriter, r *http.Request) {
	s.savePolicy(w, r, false)
}
func (s *Server) handlePolicyUpdate(w http.ResponseWriter, r *http.Request) { s.savePolicy(w, r, true) }
func (s *Server) savePolicy(w http.ResponseWriter, r *http.Request, replace bool) {
	if !s.policyAvailable(w) {
		return
	}
	var p policy.PolicyConfig
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		http.Error(w, "Invalid policy", 400)
		return
	}
	if replace {
		p.Name = strings.TrimPrefix(r.URL.Path, "/api/v1/policies/")
	}
	if s.store != nil {
		if err := s.store.Audit(principal(r), "policy_save_requested", p.Name); err != nil {
			http.Error(w, "Audit unavailable", 503)
			return
		}
	}
	if err := s.policyMgr.SavePolicy(p, replace); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	status := 201
	if replace {
		status = 200
	}
	s.jsonResponse(w, status, map[string]string{"status": "saved", "name": p.Name})
}
func (s *Server) handlePolicyDelete(w http.ResponseWriter, r *http.Request) {
	if !s.policyAvailable(w) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/policies/")
	if s.store != nil {
		if err := s.store.Audit(principal(r), "policy_delete_requested", name); err != nil {
			http.Error(w, "Audit unavailable", 503)
			return
		}
	}
	if err := s.policyMgr.DeletePolicy(name); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) handlePolicyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", 405)
		return
	}
	if !s.policyAvailable(w) {
		return
	}
	var req struct {
		From    string
		To      []string
		Subject string
		Body    string
		Headers map[string]string
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&req); err != nil {
		http.Error(w, "Invalid test input", 400)
		return
	}
	if strings.ContainsAny(req.Subject, "\r\n") {
		http.Error(w, "Invalid subject", 400)
		return
	}
	raw := "Subject: " + req.Subject + "\r\n"
	for k, v := range req.Headers {
		if k == "" || strings.ContainsAny(k, ": \t\r\n") || strings.ContainsAny(v, "\r\n") {
			http.Error(w, "Invalid headers", 400)
			return
		}
		raw += k + ": " + v + "\r\n"
	}
	raw += "\r\n" + req.Body
	email, err := policy.NewEmailContext(req.From, req.To, "192.0.2.1", "test.invalid", []byte(raw))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/policies/"), "/test")
	action, err := s.policyMgr.TestPolicy(r.Context(), name, email)
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	s.jsonResponse(w, 200, map[string]interface{}{"policy": name, "action": action})
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
