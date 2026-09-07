package api

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/ha"
	"net/http"
)

func (s *Server) SetHAGuard(g *ha.Guard) { s.haGuard = g }
func (s *Server) handleHAStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	if s.haGuard == nil {
		s.jsonResponse(w, 200, ha.Status{Enabled: false})
		return
	}
	s.jsonResponse(w, 200, s.haGuard.Status())
}
