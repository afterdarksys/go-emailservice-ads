package api

import (
	"errors"
	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"net/http"
	"strings"
)

func (s *Server) handleRecipientLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	if s.userStore == nil {
		http.Error(w, "Directory unavailable", 503)
		return
	}
	address := strings.TrimPrefix(r.URL.Path, "/api/v1/recipients/")
	targets, err := s.userStore.ResolveAddress(address, s.config.Platform.Aliases)
	if errors.Is(err, auth.ErrUserNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Directory resolution failed", 503)
		return
	}
	s.jsonResponse(w, 200, map[string]any{"recipients": targets})
}
