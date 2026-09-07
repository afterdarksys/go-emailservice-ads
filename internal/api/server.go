package api

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/oauthaccess"
	"github.com/afterdarksys/go-emailservice-ads/internal/tlsutil"
	"github.com/afterdarksys/go-emailservice-ads/internal/version"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/metrics"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"github.com/afterdarksys/go-emailservice-ads/internal/replication"
	"github.com/afterdarksys/go-emailservice-ads/internal/smtpd"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

// Server encapsulates the API servers
type Server struct {
	configReload func() error
	config       *config.Config
	logger       *zap.Logger
	store        *storage.MessageStore
	qm           *smtpd.QueueManager
	replicator   *replication.Replicator
	metrics      *metrics.Metrics
	policyMgr    *policy.Manager
	userStore    *auth.UserStore

	lifecycleMu sync.Mutex
	listener    net.Listener
	stopped     bool
	httpServer  *http.Server
	startTime   time.Time
	// grpcServer *grpc.Server

	wg sync.WaitGroup
}

// NewServer initializes the API layer
func NewServer(cfg *config.Config, logger *zap.Logger, store *storage.MessageStore, qm *smtpd.QueueManager, replicator *replication.Replicator, metricsCollector *metrics.Metrics, policyMgr *policy.Manager, userStore *auth.UserStore) *Server {
	return &Server{
		config:     cfg,
		logger:     logger,
		store:      store,
		qm:         qm,
		replicator: replicator,
		metrics:    metricsCollector,
		policyMgr:  policyMgr,
		userStore:  userStore,
		startTime:  time.Now(),
	}
}

// Start binds synchronously, so startup errors are reported before readiness.
// gRPC remains unimplemented and intentionally has no listener.
func (s *Server) Start() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.stopped || s.httpServer != nil {
		return fmt.Errorf("API server already started or stopped")
	}
	listener, err := net.Listen("tcp", s.config.API.RESTAddr)
	if err != nil {
		return err
	}
	if c := s.config.API.TLS; c != nil {
		tlsConfig, err := tlsutil.ServerConfig(c.Cert, c.Key, c.ClientCAFile, c.RequireClientCert)
		if err != nil {
			listener.Close()
			return err
		}
		listener = tls.NewListener(listener, tlsConfig)
	}
	s.listener = listener
	s.httpServer = &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, Handler: s.buildMux()}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("API serve failed", zap.Error(err))
		}
	}()
	return nil
}

// buildMux assembles the REST routing table. Extracted from startREST so
// tests can exercise the real routes and middleware without binding a port.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Health and readiness endpoints (public)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReadiness)
	// Versioned public discovery endpoints are part of the msgs.global control
	// plane contract. Keep the legacy paths above for existing probes.
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/version", s.handleVersion)

	// Metrics endpoint (public - for Prometheus)
	mux.HandleFunc("/metrics", s.handleMetrics)

	// Queue management (requires auth)
	mux.HandleFunc("/api/v1/queue/stats", s.authMiddleware(s.handleQueueStats))
	mux.HandleFunc("/api/v1/queue/pending", s.authMiddleware(s.handleQueuePending))

	// Policy management (requires auth)
	mux.HandleFunc("/api/v1/policies", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.handlePolicyCreate(w, r)
		} else {
			s.handlePolicyList(w, r)
		}
	}))
	mux.HandleFunc("/api/v1/policies/", s.authMiddleware(s.handlePolicyRouter))
	mux.HandleFunc("/api/v1/policies/stats", s.authMiddleware(s.handlePolicyStats))
	mux.HandleFunc("/api/v1/policies/reload", s.authMiddleware(s.handlePolicyReload))

	// DLQ management
	mux.HandleFunc("/api/v1/dlq/list", s.authMiddleware(s.handleDLQList))
	mux.HandleFunc("/api/v1/dlq/retry/", s.authMiddleware(s.handleDLQRetry))

	// Message management
	mux.HandleFunc("/api/v1/message/", s.authMiddleware(s.handleMessage))

	// Replication management
	mux.HandleFunc("/api/v1/replication/status", s.authMiddleware(s.handleReplicationStatus))
	mux.HandleFunc("/api/v1/replication/promote", s.authMiddleware(s.handleReplicationPromote))

	// Mailbox management (requires auth)
	mux.HandleFunc("/api/v1/mailboxes", s.authMiddleware(s.handleMailboxes))
	mux.HandleFunc("/api/v1/mailboxes/", s.authMiddleware(s.handleMailbox))

	mux.HandleFunc("/api/v1/mailstorm", s.authMiddleware(s.handleMailstorm))
	mux.HandleFunc("/api/v1/mailstorm/", s.authMiddleware(s.handleMailstorm))
	mux.HandleFunc("/api/v1/quarantine", s.authMiddleware(s.handleQuarantine))
	mux.HandleFunc("/api/v1/quarantine/", s.authMiddleware(s.handleQuarantine))
	mux.HandleFunc("/api/v1/compliance", s.authMiddleware(s.handleCompliance))
	mux.HandleFunc("/api/v1/compliance/", s.authMiddleware(s.handleCompliance))
	mux.HandleFunc("/api/v1/bounce/config", s.authMiddleware(s.handleBounceConfig))
	mux.HandleFunc("/api/v1/bounce/reports", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "GET required", 405)
			return
		}
		s.jsonResponse(w, 200, s.store.ListByStatus("bounce_report", "bounce_reports"))
	}))
	mux.HandleFunc("/api/v1/recipients/", s.authMiddleware(s.handleRecipientLookup))
	mux.HandleFunc("/api/v1/scim/v2/", s.authMiddleware(s.handleSCIM))
	mux.HandleFunc("/api/v1/dmarc/reports", s.authMiddleware(s.handleDMARC))
	mux.HandleFunc("/api/v1/dmarc/reports/", s.authMiddleware(s.handleDMARC))
	mux.HandleFunc("/api/v1/security/stats", s.authMiddleware(s.handleOperationalStats))
	mux.HandleFunc("/api/v1/dns/stats", s.authMiddleware(s.handleOperationalStats))
	mux.HandleFunc("/api/v1/greylisting/stats", s.authMiddleware(s.handleOperationalStats))
	mux.HandleFunc("/api/v1/config/reload", s.authMiddleware(s.handleConfigReload))
	return mux
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"service": "go-emailservice-ads",
		"version": version.Version,
	})
}

// authMiddleware provides authentication via API key (Bearer token) or Basic Auth
// with optional IP whitelist enforcement
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check IP whitelist first if enabled
		if s.config.API.RequireIPAuth {
			clientIP := s.getClientIP(r)
			if !s.isIPAllowed(clientIP) {
				s.logger.Warn("API access denied - IP not whitelisted",
					zap.String("ip", clientIP),
					zap.String("path", r.URL.Path))
				http.Error(w, "Access denied - IP not authorized", http.StatusForbidden)
				return
			}
		}

		// Try API key authentication first (Bearer token)
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			apiKey := strings.TrimPrefix(authHeader, "Bearer ")
			compliancePath := strings.HasPrefix(r.URL.Path, "/api/v1/compliance")
			oauthConfig := s.config.API.OAuth
			requireOAuth := compliancePath && oauthConfig.RequireForCompliance
			if oauthConfig.Enabled && (!oauthConfig.ComplianceOnly || compliancePath) && r.TLS != nil {
				if name, err := oauthaccess.ValidateToken(r.Context(), oauthConfig, apiKey, requiredScope(r)); err == nil {
					next(w, withPrincipal(r, name))
					return
				}
			}
			if requireOAuth {
				http.Error(w, "OAuth access token over TLS required", http.StatusUnauthorized)
				return
			}
			if name, ok := s.authorizeKey(apiKey, requiredScope(r)); ok {
				next(w, withPrincipal(r, name))
				return
			}
			if s.validateAPIKey(apiKey) {
				http.Error(w, "Insufficient API permissions", http.StatusForbidden)
				return
			}

			// Invalid API key
			http.Error(w, "Invalid API key", http.StatusUnauthorized)
			return
		}

		http.Error(w, "Bearer API key required", http.StatusUnauthorized)
	}
}

// getClientIP extracts the client IP from the request for allowlist checks.
//
// Threats: this feeds the require_ip_auth allowlist, so it must not be
// attacker-influenced. X-Forwarded-For and X-Real-IP are client-supplied
// headers — the REST port is published directly (no trusted reverse proxy
// strips them), so honoring them let any caller spoof an allowlisted address
// with a single header. Only the TCP peer address is trusted.
func (s *Server) getClientIP(r *http.Request) string {
	ip := r.RemoteAddr
	// Strip port if present
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// isIPAllowed checks if an IP is in the whitelist
func (s *Server) isIPAllowed(clientIP string) bool {
	// If no whitelist configured, allow all
	if len(s.config.API.AllowedIPs) == 0 {
		return true
	}

	// Normalize IP (strip brackets from IPv6)
	clientIP = strings.Trim(clientIP, "[]")

	for _, allowedIP := range s.config.API.AllowedIPs {
		if clientIP == allowedIP {
			return true
		}
	}

	return false
}

// validateAPIKey checks if the provided API key is valid
func (s *Server) validateAPIKey(apiKey string) bool {
	if s.config.API.APIKeys == nil || len(s.config.API.APIKeys) == 0 {
		return false
	}

	for _, key := range s.config.API.APIKeys {
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(key.Key)) == 1 {
			s.logger.Debug("API key authenticated", zap.String("name", key.Name))
			return true
		}
	}
	return false
}

// validateBasicAuth checks username/password against config users
func (s *Server) validateBasicAuth(username, password string) bool {
	if s.config.Auth.DefaultUsers == nil || len(s.config.Auth.DefaultUsers) == 0 {
		return false
	}

	for _, user := range s.config.Auth.DefaultUsers {
		usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(user.Username)) == 1
		passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(user.Password)) == 1

		if usernameMatch && passwordMatch {
			s.logger.Debug("Basic auth authenticated", zap.String("username", username))
			return true
		}
	}
	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	health := map[string]interface{}{
		"status": "ok",
		"uptime": time.Since(s.startTime).String(),
	}
	s.jsonResponse(w, http.StatusOK, health)
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	// Check if critical components are ready
	ready := true
	checks := make(map[string]bool)

	// Check storage
	if s.store != nil {
		checks["storage"] = s.store.Health() == nil
		if !checks["storage"] {
			ready = false
		}
	} else {
		checks["storage"] = false
		ready = false
	}

	// Check queue manager
	if s.qm != nil {
		checks["queue"] = s.qm.Ready(r.Context())
		if !checks["queue"] {
			ready = false
		}
	} else {
		checks["queue"] = false
		ready = false
	}

	status := "ready"
	httpStatus := http.StatusOK
	if !ready {
		status = "not_ready"
		httpStatus = http.StatusServiceUnavailable
	}

	response := map[string]interface{}{
		"status": status,
		"checks": checks,
	}

	s.jsonResponse(w, httpStatus, response)
}

func (s *Server) handleQueueStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	metrics := s.qm.GetMetrics()
	storageStats := s.store.Stats()

	response := map[string]interface{}{
		"metrics": map[string]interface{}{
			"enqueued":    metrics.Enqueued,
			"processed":   metrics.Processed,
			"failed":      metrics.Failed,
			"duplicates":  metrics.Duplicates,
			"last_update": metrics.LastUpdate,
		},
		"storage": storageStats,
	}

	s.jsonResponse(w, http.StatusOK, response)
}

func (s *Server) handleQueuePending(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	tier := r.URL.Query().Get("tier")
	pending := s.store.ListPending(tier)
	s.jsonResponse(w, http.StatusOK, pending)
}

func (s *Server) handleDLQList(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	dlq := s.store.GetDLQ()
	s.jsonResponse(w, http.StatusOK, dlq)
}

func (s *Server) handleDLQRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	messageID := strings.TrimPrefix(r.URL.Path, "/api/v1/dlq/retry/")
	if messageID == "" {
		http.Error(w, "Message ID required", http.StatusBadRequest)
		return
	}

	if err := s.store.RetryFromDLQ(messageID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "ok", "message_id": messageID})
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	messageID := strings.TrimPrefix(r.URL.Path, "/api/v1/message/")
	if messageID == "" {
		http.Error(w, "Message ID required", http.StatusBadRequest)
		return
	}
	if e, err := s.store.Get(messageID); err == nil && e.Metadata["compliance"] == "true" {
		http.Error(w, "Use the compliance API", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodGet:
		entry, err := s.store.Get(messageID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.jsonResponse(w, http.StatusOK, entry)

	case http.MethodDelete:
		if entry, err := s.store.Get(messageID); err == nil && entry.Status == "stored" {
			http.Error(w, "Use IMAP EXPUNGE to delete mailbox messages", 409)
			return
		}
		if err := s.store.UpdateStatus(messageID, "deleted", "manual deletion"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleReplicationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if s.replicator == nil {
		http.Error(w, "Replication not configured", http.StatusNotImplemented)
		return
	}

	status := map[string]interface{}{
		"mode":  s.replicator.GetMode(),
		"peers": s.replicator.GetPeerStatus(),
	}

	s.jsonResponse(w, http.StatusOK, status)
}

func (s *Server) handleReplicationPromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.replicator == nil {
		http.Error(w, "Replication not configured", http.StatusNotImplemented)
		return
	}

	if err := s.replicator.PromoteToPrimary(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "promoted"})
}

func (s *Server) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Shutdown is bounded by the caller deadline and safe before Start or on repeat.
func (s *Server) Shutdown(ctx context.Context) error {
	s.lifecycleMu.Lock()
	s.stopped = true
	server := s.httpServer
	s.lifecycleMu.Unlock()
	if server == nil {
		return nil
	}
	err := server.Shutdown(ctx)
	if err != nil {
		server.Close()
	}
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
