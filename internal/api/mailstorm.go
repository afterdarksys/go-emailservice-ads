package api

import (
	"encoding/json"
	"go.uber.org/zap"
	"net/http"
	"time"
)

func (s *Server) handleMailstorm(w http.ResponseWriter, r *http.Request) {
	if s.qm == nil || s.qm.StormGuard == nil {
		http.Error(w, "Mailstorm guard unavailable", 503)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/mailstorm" {
		s.jsonResponse(w, 200, s.qm.StormGuard.Status())
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Use GET status or POST pause/resume", 405)
		return
	}
	var req struct {
		Key      string `json:"key"`
		Reason   string `json:"reason"`
		Duration string `json:"duration"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	if r.URL.Path != "/api/v1/mailstorm/pause" && r.URL.Path != "/api/v1/mailstorm/resume" {
		http.NotFound(w, r)
		return
	}
	if s.store == nil {
		http.Error(w, "Audit storage unavailable", 503)
		return
	}
	if err := s.store.Audit(principal(r), r.URL.Path+"_requested", req.Key); err != nil {
		http.Error(w, "Audit storage unavailable", 503)
		return
	}
	var err error
	switch r.URL.Path {
	case "/api/v1/mailstorm/pause":
		var duration time.Duration
		duration, err = time.ParseDuration(req.Duration)
		if err == nil {
			err = s.qm.StormGuard.Pause(req.Key, req.Reason, principal(r), duration)
		}
	case "/api/v1/mailstorm/resume":
		if req.Key == "" {
			http.Error(w, "key required", 400)
			return
		}
		err = s.qm.StormGuard.Resume(req.Key)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	s.logger.Info("Mailstorm operator action", zap.String("actor", principal(r)), zap.String("key", req.Key), zap.String("action", r.URL.Path), zap.String("reason", req.Reason))
	s.jsonResponse(w, 200, s.qm.StormGuard.Status())
}
