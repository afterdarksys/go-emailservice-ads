package api

import (
	"net/http"
	"strings"
)

func (s *Server) handleOperationalStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	if s.qm == nil {
		http.Error(w, "Queue manager unavailable", 503)
		return
	}
	stats := s.qm.OperationalStatistics()
	resource := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")[0]
	if resource == "security" {
		if s.userStore != nil {
			stats["authentication"] = s.userStore.GetLockoutStats()
		}
	} else {
		selected := map[string]interface{}{}
		for listener, data := range stats["listeners"].(map[string]interface{}) {
			selected[listener] = data.(map[string]interface{})[resource]
		}
		stats = map[string]interface{}{"listeners": selected, "scope": stats["scope"]}
	}
	w.Header().Set("Cache-Control", "no-store")
	s.jsonResponse(w, 200, stats)
}
