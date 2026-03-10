package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/metrics"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"github.com/afterdarksys/go-emailservice-ads/internal/replication"
	"github.com/afterdarksys/go-emailservice-ads/internal/smtpd"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

// Server encapsulates the API servers
type Server struct {
	config     *config.Config
	logger     *zap.Logger
	store      *storage.MessageStore
	qm         *smtpd.QueueManager
	replicator *replication.Replicator
	metrics    *metrics.Metrics
	policyMgr  *policy.Manager

	httpServer *http.Server
	startTime  time.Time
	// grpcServer *grpc.Server

	wg sync.WaitGroup
}

// NewServer initializes the API layer
func NewServer(cfg *config.Config, logger *zap.Logger, store *storage.MessageStore, qm *smtpd.QueueManager, replicator *replication.Replicator, metricsCollector *metrics.Metrics, policyMgr *policy.Manager) *Server {
	return &Server{
		config:     cfg,
		logger:     logger,
		store:      store,
		qm:         qm,
		replicator: replicator,
		metrics:    metricsCollector,
		policyMgr:  policyMgr,
		startTime:  time.Now(),
	}
}

// Start begins serving the REST and gRPC endpoints
func (s *Server) Start() {
	s.wg.Add(1)
	go s.startREST()

	s.wg.Add(1)
	go s.startGRPC()
}

func (s *Server) startREST() {
	defer s.wg.Done()
	mux := http.NewServeMux()

	// Health and readiness endpoints (public)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReadiness)

	// Metrics endpoint (public - for Prometheus)
	if s.metrics != nil {
		mux.Handle("/metrics", s.metrics.Handler())
	}

	// Queue management (requires auth)
	mux.HandleFunc("/api/v1/queue/stats", s.authMiddleware(s.handleQueueStats))
	mux.HandleFunc("/api/v1/queue/pending", s.authMiddleware(s.handleQueuePending))

	// Policy management (requires auth)
	mux.HandleFunc("/api/v1/policies", s.authMiddleware(s.handlePolicyList))
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

	s.httpServer = &http.Server{
		Addr:    s.config.API.RESTAddr,
		Handler: mux,
	}

	s.logger.Info("Starting REST API server", zap.String("addr", s.config.API.RESTAddr))
	err := s.httpServer.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		s.logger.Fatal("REST API server crashed", zap.Error(err))
	}
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
			if s.validateAPIKey(apiKey) {
				next(w, r)
				return
			}
			// Invalid API key
			http.Error(w, "Invalid API key", http.StatusUnauthorized)
			return
		}

		// Fall back to Basic Auth
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Mail Service API", Bearer realm="Mail Service API"`)
			http.Error(w, "Unauthorized - provide API key or Basic Auth", http.StatusUnauthorized)
			return
		}

		// Validate against config users
		if s.validateBasicAuth(username, password) {
			next(w, r)
			return
		}

		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}
}

// getClientIP extracts the client IP from the request
func (s *Server) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxied requests)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		// Return the first IP (original client)
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
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
	health := map[string]interface{}{
		"status": "ok",
		"uptime": time.Since(s.startTime).String(),
	}
	s.jsonResponse(w, http.StatusOK, health)
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	// Check if critical components are ready
	ready := true
	checks := make(map[string]bool)

	// Check storage
	if s.store != nil {
		stats := s.store.Stats()
		checks["storage"] = stats != nil
	} else {
		checks["storage"] = false
		ready = false
	}

	// Check queue manager
	if s.qm != nil {
		metrics := s.qm.GetMetrics()
		checks["queue"] = metrics != nil
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
	metrics := s.qm.GetMetrics()
	storageStats := s.store.Stats()

	response := map[string]interface{}{
		"metrics": map[string]interface{}{
			"enqueued":   metrics.Enqueued,
			"processed":  metrics.Processed,
			"failed":     metrics.Failed,
			"duplicates": metrics.Duplicates,
			"last_update": metrics.LastUpdate,
		},
		"storage": storageStats,
	}

	s.jsonResponse(w, http.StatusOK, response)
}

func (s *Server) handleQueuePending(w http.ResponseWriter, r *http.Request) {
	tier := r.URL.Query().Get("tier")
	pending := s.store.ListPending(tier)
	s.jsonResponse(w, http.StatusOK, pending)
}

func (s *Server) handleDLQList(w http.ResponseWriter, r *http.Request) {
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

	switch r.Method {
	case http.MethodGet:
		entry, err := s.store.Get(messageID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.jsonResponse(w, http.StatusOK, entry)

	case http.MethodDelete:
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

func (s *Server) startGRPC() {
	defer s.wg.Done()

	addr := s.config.API.GRPCAddr
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		s.logger.Fatal("Failed to listen for gRPC", zap.Error(err))
	}

	s.logger.Info("Starting gRPC API server placeholder", zap.String("addr", addr))
	// Placeholder: when gRPC is implemented, we will call server.Serve(lis)
	// For now, accept connections and close them
	for {
		conn, err := lis.Accept()
		if err != nil {
			break
		}
		conn.Close() // Drop connections until properly implemented
	}
}

// Shutdown gracefully stops the API servers
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down API servers...")
	if s.httpServer != nil {
		s.httpServer.Shutdown(ctx)
	}
	// if s.grpcServer != nil { s.grpcServer.GracefulStop() }
	s.wg.Wait()
	return nil
}
