package api

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strings"
)

func (s *Server) handleDMARC(w http.ResponseWriter, r *http.Request) {
	if s.qm == nil || s.qm.DMARCReports == nil {
		http.Error(w, "DMARC reporting disabled", 503)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/dmarc/reports")
	id = strings.TrimPrefix(id, "/")
	if r.Method == "POST" && id == "send" {
		if err := s.qm.DMARCReports.SendPending(r.Context()); err != nil {
			http.Error(w, "DMARC report delivery incomplete; inspect server logs and reporting DNS", 503)
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	reports, err := s.qm.DMARCReports.List()
	if err != nil {
		http.Error(w, "Unable to read reports", 500)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if id == "" {
		out := []map[string]interface{}{}
		for _, v := range reports {
			out = append(out, map[string]interface{}{"id": v.Metadata.ID, "domain": v.Published.Domain, "begin": v.Metadata.Range.Begin, "end": v.Metadata.Range.End, "rows": len(v.Records), "sent": v.Sent})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
		return
	}
	for _, v := range reports {
		if v.Metadata.ID == id {
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(xml.Header))
			xml.NewEncoder(w).Encode(v)
			return
		}
	}
	http.NotFound(w, r)
}
