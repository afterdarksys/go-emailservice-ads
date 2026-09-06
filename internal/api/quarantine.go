package api

import (
	"go.uber.org/zap"
	"net/http"
	"strings"
)

func (s *Server) handleQuarantine(w http.ResponseWriter, r *http.Request) {
	if s.store == nil || s.qm == nil {
		http.Error(w, "Quarantine unavailable", 503)
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/quarantine"), "/")
	if path == "" && r.Method == "GET" {
		rows := []map[string]any{}
		for _, e := range s.store.ListByStatus("held", "") {
			rows = append(rows, map[string]any{"id": e.MessageID, "from": e.From, "to": e.To, "created_at": e.CreatedAt, "bytes": len(e.Data)})
		}
		s.jsonResponse(w, 200, rows)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || r.Method != "POST" {
		http.Error(w, "Use POST /quarantine/{id}/release or /delete", 405)
		return
	}
	id := parts[0]
	if parts[1] != "release" && parts[1] != "delete" {
		http.NotFound(w, r)
		return
	}
	if err := s.store.Audit(principal(r), "quarantine_"+parts[1]+"_requested", id); err != nil {
		http.Error(w, "Audit storage unavailable", 503)
		return
	}
	var err error
	switch parts[1] {
	case "release":
		err = s.qm.ReleaseHeld(r.Context(), id, principal(r))
	case "delete":
		var ok bool
		ok, err = s.store.Transition(id, "held", "deleted")
		if !ok && err == nil {
			http.Error(w, "Held message not found", 404)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	s.logger.Info("Quarantine action", zap.String("actor", principal(r)), zap.String("id", id), zap.String("action", parts[1]))
	s.jsonResponse(w, 200, map[string]string{"status": parts[1], "id": id})
}
