package api

import "net/http"

// SetConfigReload installs the lifecycle controller before Start.
func (s *Server) SetConfigReload(reload func() error) { s.configReload = reload }
func (s *Server) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	if s.configReload == nil {
		http.Error(w, "Reload controller unavailable", 503)
		return
	}
	if err := s.configReload(); err != nil {
		s.logger.Warn("Configuration reload rejected")
		http.Error(w, "Configuration rejected or reload pending; validate configuration on host", 409)
		return
	}
	s.jsonResponse(w, 202, map[string]string{"status": "accepted", "mode": "graceful-process-replacement", "verification": "wait for readiness and verify new settings"})
}
