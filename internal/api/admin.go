package api

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed admin/*
var adminFiles embed.FS

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.config.API.AdminEnabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "GET required", 405)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/admin/")
	if name == "" {
		name = "index.html"
	}
	contentType := ""
	switch name {
	case "index.html":
		contentType = "text/html; charset=utf-8"
	case "app.js":
		contentType = "text/javascript; charset=utf-8"
	case "style.css":
		contentType = "text/css; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	data, err := adminFiles.ReadFile("admin/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != "HEAD" {
		w.Write(data)
	}
}
