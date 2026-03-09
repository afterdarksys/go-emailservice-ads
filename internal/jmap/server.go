package jmap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

// RFC 8620 - JSON Meta Application Protocol (JMAP)
// RFC 8621 - JMAP for Mail
// Modern alternative to IMAP with better performance and simpler API

// JMAPServer implements a JMAP server
type JMAPServer struct {
	logger     *zap.Logger
	config     *config.Config
	validator  *auth.Validator
	httpServer *http.Server
}

// NewJMAPServer creates a new JMAP server
func NewJMAPServer(logger *zap.Logger, cfg *config.Config, validator *auth.Validator) *JMAPServer {
	return &JMAPServer{
		logger:    logger,
		config:    cfg,
		validator: validator,
	}
}

// Start begins serving JMAP requests
func (j *JMAPServer) Start(addr string) error {
	mux := http.NewServeMux()

	// RFC 8620 Section 2 - Session Resource
	mux.HandleFunc("/.well-known/jmap", j.handleSession)

	// RFC 8620 Section 3 - JMAP API endpoint
	mux.HandleFunc("/jmap/", j.handleJMAPAPI)

	// RFC 8621 - Download endpoint for binary data
	mux.HandleFunc("/jmap/download/", j.handleDownload)

	// RFC 8621 - Upload endpoint for binary data
	mux.HandleFunc("/jmap/upload/", j.handleUpload)

	j.httpServer = &http.Server{
		Addr:         addr,
		Handler:      j.authMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	j.logger.Info("Starting JMAP server", zap.String("addr", addr))

	go func() {
		if err := j.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			j.logger.Error("JMAP server error", zap.Error(err))
		}
	}()

	return nil
}

// Shutdown gracefully stops the JMAP server
func (j *JMAPServer) Shutdown(ctx context.Context) error {
	j.logger.Info("Stopping JMAP server...")
	if j.httpServer != nil {
		return j.httpServer.Shutdown(ctx)
	}
	return nil
}

// authMiddleware provides authentication for JMAP requests
func (j *JMAPServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RFC 8620 Section 3.1 - Authentication
		// Support both Basic Auth and Bearer tokens

		username, password, ok := r.BasicAuth()
		if ok {
			if _, err := j.validator.Authenticate(username, password); err != nil {
				http.Error(w, "Authentication failed", http.StatusUnauthorized)
				return
			}
		} else {
			// Check for Bearer token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !j.validateBearerToken(authHeader) {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// handleSession returns the JMAP session resource
// RFC 8620 Section 2 - Session Resource
func (j *JMAPServer) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session := Session{
		Capabilities: map[string]interface{}{
			"urn:ietf:params:jmap:core": CoreCapability{
				MaxSizeUpload:           50 * 1024 * 1024, // 50MB
				MaxConcurrentUpload:     4,
				MaxSizeRequest:          10 * 1024 * 1024, // 10MB
				MaxConcurrentRequests:   4,
				MaxCallsInRequest:       16,
				MaxObjectsInGet:         500,
				MaxObjectsInSet:         500,
				CollationAlgorithms:     []string{"i;ascii-numeric", "i;ascii-casemap"},
			},
			"urn:ietf:params:jmap:mail": MailCapability{
				MaxMailboxesPerEmail:    nil, // unlimited
				MaxMailboxDepth:         10,
				MaxSizeMailboxName:      255,
				MaxSizeAttachmentsPerEmail: 50 * 1024 * 1024,
				EmailQuerySortOptions:   []string{"receivedAt", "from", "to", "subject"},
				MayCreateTopLevelMailbox: true,
			},
		},
		Accounts: map[string]Account{
			"primary": {
				Name:            "Primary Account",
				IsPersonal:      true,
				IsReadOnly:      false,
				AccountCapabilities: map[string]interface{}{
					"urn:ietf:params:jmap:mail": map[string]interface{}{},
				},
			},
		},
		PrimaryAccounts: map[string]string{
			"urn:ietf:params:jmap:mail": "primary",
		},
		Username: "user@example.com", // Would come from auth
		APIUrl:   fmt.Sprintf("https://%s/jmap/api/", r.Host),
		DownloadUrl: fmt.Sprintf("https://%s/jmap/download/{accountId}/{blobId}/{name}?accept={type}", r.Host),
		UploadUrl:   fmt.Sprintf("https://%s/jmap/upload/{accountId}/", r.Host),
		EventSourceUrl: fmt.Sprintf("https://%s/jmap/eventsource/?types={types}&closeafter={closeafter}&ping={ping}", r.Host),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

// handleJMAPAPI handles JMAP API requests
// RFC 8620 Section 3.3 - Making an API Request
func (j *JMAPServer) handleJMAPAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		j.logger.Warn("Invalid JMAP request", zap.Error(err))
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Process request
	resp := j.processRequest(&req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// processRequest processes a JMAP request and returns a response
func (j *JMAPServer) processRequest(req *Request) *Response {
	resp := &Response{
		MethodResponses: make([]MethodResponse, 0),
		SessionState:    req.Using,
	}

	// Process each method call
	for _, call := range req.MethodCalls {
		methodResp := j.processMethodCall(call)
		resp.MethodResponses = append(resp.MethodResponses, methodResp)
	}

	return resp
}

// processMethodCall processes a single method call
func (j *JMAPServer) processMethodCall(call MethodCall) MethodResponse {
	methodName := call.Name
	args := call.Arguments
	callID := call.ID

	j.logger.Debug("Processing JMAP method call",
		zap.String("method", methodName),
		zap.String("call_id", callID))

	switch methodName {
	case "Mailbox/get":
		return j.handleMailboxGet(args, callID)
	case "Mailbox/set":
		return j.handleMailboxSet(args, callID)
	case "Email/get":
		return j.handleEmailGet(args, callID)
	case "Email/set":
		return j.handleEmailSet(args, callID)
	case "Email/query":
		return j.handleEmailQuery(args, callID)
	case "Email/changes":
		return j.handleEmailChanges(args, callID)
	default:
		return MethodResponse{
			Name: "error",
			Arguments: map[string]interface{}{
				"type":        "unknownMethod",
				"description": fmt.Sprintf("Unknown method: %s", methodName),
			},
			CallID: callID,
		}
	}
}

// JMAP method handlers (simplified implementations)

func (j *JMAPServer) handleMailboxGet(args map[string]interface{}, callID string) MethodResponse {
	// Return standard mailboxes
	mailboxes := []map[string]interface{}{
		{
			"id":            "inbox",
			"name":          "Inbox",
			"role":          "inbox",
			"sortOrder":     0,
			"totalEmails":   0,
			"unreadEmails":  0,
			"totalThreads":  0,
			"unreadThreads": 0,
		},
		{
			"id":            "sent",
			"name":          "Sent",
			"role":          "sent",
			"sortOrder":     1,
			"totalEmails":   0,
			"unreadEmails":  0,
			"totalThreads":  0,
			"unreadThreads": 0,
		},
	}

	return MethodResponse{
		Name: "Mailbox/get",
		Arguments: map[string]interface{}{
			"accountId": "primary",
			"state":     "0",
			"list":      mailboxes,
			"notFound":  []string{},
		},
		CallID: callID,
	}
}

func (j *JMAPServer) handleMailboxSet(args map[string]interface{}, callID string) MethodResponse {
	return MethodResponse{
		Name: "Mailbox/set",
		Arguments: map[string]interface{}{
			"accountId":  "primary",
			"oldState":   "0",
			"newState":   "1",
			"created":    map[string]interface{}{},
			"updated":    map[string]interface{}{},
			"destroyed":  []string{},
			"notCreated": map[string]interface{}{},
			"notUpdated": map[string]interface{}{},
			"notDestroyed": map[string]interface{}{},
		},
		CallID: callID,
	}
}

func (j *JMAPServer) handleEmailGet(args map[string]interface{}, callID string) MethodResponse {
	return MethodResponse{
		Name: "Email/get",
		Arguments: map[string]interface{}{
			"accountId": "primary",
			"state":     "0",
			"list":      []map[string]interface{}{},
			"notFound":  []string{},
		},
		CallID: callID,
	}
}

func (j *JMAPServer) handleEmailSet(args map[string]interface{}, callID string) MethodResponse {
	return MethodResponse{
		Name: "Email/set",
		Arguments: map[string]interface{}{
			"accountId": "primary",
			"oldState":  "0",
			"newState":  "1",
			"created":   map[string]interface{}{},
			"updated":   map[string]interface{}{},
			"destroyed": []string{},
		},
		CallID: callID,
	}
}

func (j *JMAPServer) handleEmailQuery(args map[string]interface{}, callID string) MethodResponse {
	return MethodResponse{
		Name: "Email/query",
		Arguments: map[string]interface{}{
			"accountId":  "primary",
			"queryState": "0",
			"canCalculateChanges": true,
			"position":   0,
			"total":      0,
			"ids":        []string{},
		},
		CallID: callID,
	}
}

func (j *JMAPServer) handleEmailChanges(args map[string]interface{}, callID string) MethodResponse {
	return MethodResponse{
		Name: "Email/changes",
		Arguments: map[string]interface{}{
			"accountId":  "primary",
			"oldState":   "0",
			"newState":   "0",
			"hasMoreChanges": false,
			"created":    []string{},
			"updated":    []string{},
			"destroyed":  []string{},
		},
		CallID: callID,
	}
}

func (j *JMAPServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	// RFC 8621 Section 6 - Binary Data
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

func (j *JMAPServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	// RFC 8621 Section 6 - Binary Data
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

func (j *JMAPServer) validateBearerToken(authHeader string) bool {
	// Simplified token validation
	// In production, validate JWT or OAuth tokens
	return false
}

// JMAP data structures per RFC 8620

type Session struct {
	Capabilities    map[string]interface{} `json:"capabilities"`
	Accounts        map[string]Account     `json:"accounts"`
	PrimaryAccounts map[string]string      `json:"primaryAccounts"`
	Username        string                 `json:"username"`
	APIUrl          string                 `json:"apiUrl"`
	DownloadUrl     string                 `json:"downloadUrl"`
	UploadUrl       string                 `json:"uploadUrl"`
	EventSourceUrl  string                 `json:"eventSourceUrl"`
	State           string                 `json:"state,omitempty"`
}

type CoreCapability struct {
	MaxSizeUpload           int      `json:"maxSizeUpload"`
	MaxConcurrentUpload     int      `json:"maxConcurrentUpload"`
	MaxSizeRequest          int      `json:"maxSizeRequest"`
	MaxConcurrentRequests   int      `json:"maxConcurrentRequests"`
	MaxCallsInRequest       int      `json:"maxCallsInRequest"`
	MaxObjectsInGet         int      `json:"maxObjectsInGet"`
	MaxObjectsInSet         int      `json:"maxObjectsInSet"`
	CollationAlgorithms     []string `json:"collationAlgorithms"`
}

type MailCapability struct {
	MaxMailboxesPerEmail       *int     `json:"maxMailboxesPerEmail"`
	MaxMailboxDepth            int      `json:"maxMailboxDepth"`
	MaxSizeMailboxName         int      `json:"maxSizeMailboxName"`
	MaxSizeAttachmentsPerEmail int      `json:"maxSizeAttachmentsPerEmail"`
	EmailQuerySortOptions      []string `json:"emailQuerySortOptions"`
	MayCreateTopLevelMailbox   bool     `json:"mayCreateTopLevelMailbox"`
}

type Account struct {
	Name                string                 `json:"name"`
	IsPersonal          bool                   `json:"isPersonal"`
	IsReadOnly          bool                   `json:"isReadOnly"`
	AccountCapabilities map[string]interface{} `json:"accountCapabilities"`
}

type Request struct {
	Using       []string     `json:"using"`
	MethodCalls []MethodCall `json:"methodCalls"`
	CreatedIds  map[string]string `json:"createdIds,omitempty"`
}

type MethodCall struct {
	Name      string                 `json:"0"`
	Arguments map[string]interface{} `json:"1"`
	ID        string                 `json:"2"`
}

type Response struct {
	MethodResponses []MethodResponse `json:"methodResponses"`
	CreatedIds      map[string]string `json:"createdIds,omitempty"`
	SessionState    []string         `json:"sessionState"`
}

type MethodResponse struct {
	Name      string                 `json:"0"`
	Arguments map[string]interface{} `json:"1"`
	CallID    string                 `json:"2"`
}
