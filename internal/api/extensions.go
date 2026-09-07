package api

import (
	"context"
	"github.com/afterdarksys/go-emailservice-ads/internal/extensions"
	"go.uber.org/zap"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) startExtensions() error {
	if len(s.config.Platform.Webhooks) == 0 {
		return nil
	}
	o, e := extensions.OpenOutbox(filepath.Join(s.config.Platform.DataDir, "webhooks.db"), s.config.Platform.Webhooks)
	if e != nil {
		return e
	}
	s.outbox = o
	ctx, cancel := context.WithCancel(context.Background())
	s.extensionsCancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e := o.Dispatch(ctx); e != nil && ctx.Err() == nil {
					s.logger.Error("Webhook dispatch failed", zap.Error(e))
				}
			}
		}
	}()
	return nil
}
func (s *Server) recordMutation(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/extensions/webhooks/retry" || s.outbox == nil || (r.Method == "GET" || r.Method == "HEAD") && !strings.HasSuffix(requiredScope(r), ":write") {
			next(w, r)
			return
		}
		id, e := s.outbox.Begin(r.Context(), principal(r), r.Method, r.URL.Path)
		if e != nil {
			http.Error(w, "Cannot persist management event", 503)
			return
		}
		capture := &rpcResponse{header: make(http.Header)}
		next(capture, r)
		code := capture.code
		if code == 0 {
			code = 200
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e = s.outbox.Finish(ctx, id, code); e != nil {
			http.Error(w, "Operation outcome uncertain; inspect state before retrying", 503)
			return
		}
		if capture.overflow {
			http.Error(w, "Response too large; inspect operation state", 500)
			return
		}
		for k, values := range capture.header {
			w.Header()[k] = values
		}
		w.WriteHeader(code)
		w.Write(capture.body.Bytes())
	}
}
func (s *Server) handleExtensions(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" && r.URL.Path == "/api/v1/extensions/webhooks/retry" {
		if s.outbox == nil {
			http.Error(w, "Webhooks disabled", 409)
			return
		}
		if e := s.outbox.Retry(r.Context()); e != nil {
			http.Error(w, "Retry failed", 500)
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.Method != "GET" || r.URL.Path != "/api/v1/extensions" {
		http.Error(w, "Unsupported operation", 405)
		return
	}
	stats := map[string]int{}
	if s.outbox != nil {
		var e error
		stats, e = s.outbox.Stats(r.Context())
		if e != nil {
			http.Error(w, "Statistics unavailable", 500)
			return
		}
	}
	s.jsonResponse(w, 200, map[string]interface{}{"admission_plugin_count": len(s.config.Platform.AdmissionPlugins), "webhooks_enabled": s.outbox != nil, "deliveries": stats, "grpc_enabled": s.config.API.GRPCEnabled, "admin_enabled": s.config.API.AdminEnabled})
}
