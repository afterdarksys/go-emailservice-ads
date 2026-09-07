package api

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"net/http"
	"time"
)

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	if s.metrics != nil {
		s.metrics.Handler().ServeHTTP(w, r)
	}
	if s.store != nil {
		v := s.store.Telemetry()
		for _, state := range []string{"pending", "queued", "processing", "scheduled", "held", "failed", "stored"} {
			fmt.Fprintf(w, "mailhub_messages{state=%q} %d\n", state, v.States[state])
		}
		fmt.Fprintf(w, "mailhub_queue_bytes %d\nmailhub_oldest_queued_seconds %g\nmailhub_disk_free_bytes %d\n", v.QueueBytes, v.OldestSeconds, v.FreeBytes)
	}
	if s.qm != nil {
		s.qm.WriteOperationalMetrics(r.Context(), w)
	}
	for role, c := range map[string]*config.TLSConfig{"api": s.config.API.TLS, "smtp": s.config.Server.TLS} {
		if c == nil {
			continue
		}
		remaining := -1.0
		if pair, err := tls.LoadX509KeyPair(c.Cert, c.Key); err == nil {
			if cert, err := x509.ParseCertificate(pair.Certificate[0]); err == nil {
				remaining = time.Until(cert.NotAfter).Seconds()
			}
		}
		fmt.Fprintf(w, "mailhub_certificate_remaining_seconds{role=%q} %g\n", role, remaining)
	}
}
