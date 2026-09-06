package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) handleBounceConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET only; update platform.bounce in server configuration", 405)
		return
	}
	cfg := s.config.Platform.Bounce.Defaults()
	if cfg.Postmaster == "" {
		cfg.Postmaster = "postmaster@" + s.config.Server.Domain
	}
	s.jsonResponse(w, 200, cfg)
}
func (s *Server) handleCompliance(w http.ResponseWriter, r *http.Request) {
	if s.store == nil || s.qm == nil {
		http.Error(w, "Compliance unavailable", 503)
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/compliance"), "/")
	if r.Method == "GET" && path == "config" {
		s.jsonResponse(w, 200, s.config.Platform.Compliance)
		return
	}
	if r.Method == "GET" && path == "" {
		if err := s.store.Audit(principal(r), "compliance_list", r.URL.Query().Get("domain")); err != nil {
			http.Error(w, "Audit unavailable", 503)
			return
		}
		s.jsonResponse(w, 200, s.store.ListCompliance(r.URL.Query().Get("domain")))
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, action := parts[0], parts[1]
	if action == "export" && r.Method == "GET" {
		e, err := s.store.Get(id)
		if err != nil || e.Metadata["compliance"] != "true" {
			http.NotFound(w, r)
			return
		}
		if err := s.store.Audit(principal(r), "compliance_export", id); err != nil {
			http.Error(w, "Audit unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "message/rfc822")
		w.Header().Set("Content-Disposition", "attachment; filename=message.eml")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(e.Data)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var request struct {
		Reason string `json:"reason"`
		Hold   bool   `json:"hold"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&request); err != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	var err error
	switch action {
	case "release":
		err = s.qm.ReleaseCompliance(id, principal(r), request.Reason)
	case "legal-hold":
		err = s.store.SetComplianceHold(id, principal(r), request.Reason, request.Hold)
	case "delete":
		err = s.store.DeleteCompliance(id, principal(r), request.Reason)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	s.jsonResponse(w, 200, map[string]string{"id": id, "action": action})
}
