package jmap

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

// RFC 8620 - JSON Meta Application Protocol (JMAP)
// RFC 8621 - JMAP for Mail
// Modern alternative to IMAP with better performance and simpler API

// JMAPServer implements a JMAP server
type JMAPServer struct {
	logger       *zap.Logger
	config       *config.Config
	validator    *auth.Validator
	store        *storage.IMAPAdapter
	jwtPublicKey crypto.PublicKey
	httpServer   *http.Server
}

// NewJMAPServer creates a new JMAP server.
// store may be nil if JMAP Email/get is not required.
func NewJMAPServer(logger *zap.Logger, cfg *config.Config, validator *auth.Validator, store *storage.IMAPAdapter) *JMAPServer {
	s := &JMAPServer{
		logger:    logger,
		config:    cfg,
		validator: validator,
		store:     store,
	}

	if cfg.JMAP.JWTPublicKeyPath != "" {
		key, err := loadPublicKey(cfg.JMAP.JWTPublicKeyPath)
		if err != nil {
			logger.Warn("JMAP JWT public key load failed — Bearer auth disabled",
				zap.String("path", cfg.JMAP.JWTPublicKeyPath),
				zap.Error(err))
		} else {
			s.jwtPublicKey = key
			logger.Info("JMAP JWT public key loaded", zap.String("path", cfg.JMAP.JWTPublicKeyPath))
		}
	}

	return s
}

// loadPublicKey reads a PEM-encoded RSA or ECDSA public key from disk.
func loadPublicKey(path string) (crypto.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}
	switch key.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey:
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported public key type: %T", key)
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

// authUserKey is the context key under which the authenticated account name is
// stored after authMiddleware succeeds. Handlers MUST use this value — never a
// client-supplied accountId — when accessing mailbox data.
type authUserKeyType struct{}

var authUserKey = authUserKeyType{}

// authUserFromContext returns the authenticated account name bound by
// authMiddleware, or "" if the request was somehow not authenticated.
func authUserFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(authUserKey).(string); ok {
		return v
	}
	return ""
}

// authMiddleware provides authentication for JMAP requests
func (j *JMAPServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RFC 8620 Section 3.1 - Authentication
		// Support both Basic Auth and Bearer tokens

		var authUser string
		username, password, ok := r.BasicAuth()
		if ok {
			if _, err := j.validator.Authenticate(username, password); err != nil {
				http.Error(w, "Authentication failed", http.StatusUnauthorized)
				return
			}
			authUser = username
		} else {
			// Check for Bearer token
			authHeader := r.Header.Get("Authorization")
			subject, valid := j.bearerSubject(authHeader)
			if !valid {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}
			authUser = subject
		}

		// Bind the authenticated identity to the request so downstream handlers
		// scope every mailbox access to this account and cannot be tricked by a
		// client-supplied accountId (broken object-level authorization / IDOR).
		ctx := context.WithValue(r.Context(), authUserKey, authUser)
		next.ServeHTTP(w, r.WithContext(ctx))
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
		Username: authUserFromContext(r.Context()),
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

	// Process request scoped to the authenticated account.
	resp := j.processRequest(r.Context(), &req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// processRequest processes a JMAP request and returns a response
func (j *JMAPServer) processRequest(ctx context.Context, req *Request) *Response {
	resp := &Response{
		MethodResponses: make([]MethodResponse, 0),
		SessionState:    req.Using,
	}

	authUser := authUserFromContext(ctx)

	// Process each method call
	for _, call := range req.MethodCalls {
		methodResp := j.processMethodCall(ctx, authUser, call)
		resp.MethodResponses = append(resp.MethodResponses, methodResp)
	}

	return resp
}

// processMethodCall processes a single method call
func (j *JMAPServer) processMethodCall(ctx context.Context, authUser string, call MethodCall) MethodResponse {
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
		return j.handleEmailGet(ctx, authUser, args, callID)
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

func (j *JMAPServer) handleEmailGet(ctx context.Context, authUser string, args map[string]interface{}, callID string) MethodResponse {
	notFound := []string{}
	list := []map[string]interface{}{}

	// The account a JMAP client addresses is identified by accountId, but the
	// data it may reach is determined solely by the authenticated identity. We
	// echo back the client's accountId only after confirming it maps to the
	// authenticated user; any other value is rejected so one user cannot read
	// another user's mailbox by spoofing accountId.
	reqAccountID, _ := args["accountId"].(string)
	if reqAccountID != "" && reqAccountID != "primary" && reqAccountID != authUser {
		return MethodResponse{
			Name: "error",
			Arguments: map[string]interface{}{
				"type":        "accountNotFound",
				"description": "requested account is not accessible to the authenticated user",
			},
			CallID: callID,
		}
	}
	accountID := "primary"

	if j.store == nil || authUser == "" {
		return MethodResponse{
			Name:      "Email/get",
			Arguments: map[string]interface{}{"accountId": accountID, "state": "0", "list": list, "notFound": notFound},
			CallID:    callID,
		}
	}

	// Build the set of message IDs the authenticated user actually owns. Every
	// read — whether by explicit id or "return all" — is filtered through this
	// set so a guessed/enumerated message id from another mailbox cannot be
	// fetched (object-level authorization).
	owned := map[string]MessageOwnedSummary{}
	if summaries, err := j.store.GetMessages(ctx, authUser, "INBOX"); err != nil {
		j.logger.Warn("Email/get store error", zap.String("user", authUser), zap.Error(err))
	} else {
		for _, s := range summaries {
			owned[s.ID] = MessageOwnedSummary{Size: s.Size, Flags: s.Flags}
		}
	}

	if rawIDs, ok := args["ids"]; ok && rawIDs != nil {
		// Specific IDs requested: only return those the user owns.
		for _, id := range toStringSlice(rawIDs) {
			if _, ok := owned[id]; !ok {
				notFound = append(notFound, id)
				continue
			}
			data, err := j.store.FetchMessage(ctx, id)
			if err != nil {
				notFound = append(notFound, id)
				continue
			}
			list = append(list, emailObjectFromRaw(id, data))
		}
	} else {
		// No IDs specified — return all messages the user owns.
		for id, meta := range owned {
			list = append(list, map[string]interface{}{
				"id":    id,
				"size":  meta.Size,
				"flags": meta.Flags,
			})
		}
	}

	return MethodResponse{
		Name: "Email/get",
		Arguments: map[string]interface{}{
			"accountId": accountID,
			"state":     "0",
			"list":      list,
			"notFound":  notFound,
		},
		CallID: callID,
	}
}

// MessageOwnedSummary captures the minimal metadata needed to answer Email/get
// for a message confirmed to belong to the authenticated user.
type MessageOwnedSummary struct {
	Size  int64
	Flags []string
}

// emailObjectFromRaw parses raw RFC 5322 bytes into a minimal JMAP Email object.
func emailObjectFromRaw(id string, data []byte) map[string]interface{} {
	obj := map[string]interface{}{
		"id":   id,
		"size": len(data),
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		return obj
	}
	h := msg.Header
	obj["subject"] = h.Get("Subject")
	obj["from"] = h.Get("From")
	obj["to"] = h.Get("To")
	obj["date"] = h.Get("Date")
	obj["messageId"] = h.Get("Message-ID")
	return obj
}

// toStringSlice converts an interface{} that is []interface{} of strings to []string.
func toStringSlice(v interface{}) []string {
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
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

// bearerSubject validates a JWT Bearer token from an Authorization header and,
// on success, returns the token subject (sub claim) as the authenticated account
// name. The boolean is false if the token is missing, malformed, or invalid.
func (j *JMAPServer) bearerSubject(authHeader string) (string, bool) {
	if j.jwtPublicKey == nil {
		return "", false
	}

	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr == authHeader {
		return "", false // prefix not present
	}

	keyFunc := func(t *jwt.Token) (interface{}, error) {
		switch j.jwtPublicKey.(type) {
		case *rsa.PublicKey:
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
		case *ecdsa.PublicKey:
			if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
		}
		return j.jwtPublicKey, nil
	}

	opts := []jwt.ParserOption{jwt.WithExpirationRequired()}
	if j.config.JMAP.JWTIssuer != "" {
		opts = append(opts, jwt.WithIssuer(j.config.JMAP.JWTIssuer))
	}

	token, err := jwt.Parse(tokenStr, keyFunc, opts...)
	if err != nil || !token.Valid {
		j.logger.Debug("JMAP Bearer token invalid", zap.Error(err))
		return "", false
	}

	subject, err := token.Claims.GetSubject()
	if err != nil || subject == "" {
		j.logger.Debug("JMAP Bearer token missing subject claim")
		return "", false
	}
	return subject, true
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
