// Mailbox management endpoints — runtime create/list/update/delete of mail
// users without a server restart, replacing the config-file-only
// auth.default_users workflow.
//
// Threats: these endpoints mint and destroy mail credentials, so they are a
// direct account-takeover surface. They protect against unauthorized callers
// by sitting behind the shared authMiddleware (constant-time API-key check
// plus the optional source-IP allowlist) and against credential disclosure by
// never returning or logging passwords or password hashes. Request bodies are
// size-capped and passwords length-bounded before hashing (bcrypt DoS). They
// do NOT protect against a caller holding a valid admin API key — key holders
// are fully trusted — nor do they rate-limit; the key and allowlist are the
// perimeter.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
)

const (
	maxMailboxBodyBytes = 64 * 1024
	minPasswordLen      = 12
	maxPasswordLen      = 128
	maxIdentifierLen    = 255
)

type mailboxCreateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

type mailboxUpdateRequest struct {
	Password string `json:"password,omitempty"`
	Email    string `json:"email,omitempty"`
}

type mailboxResponse struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Enabled  bool   `json:"enabled"`
}

// handleMailboxes serves /api/v1/mailboxes (GET list, POST create).
func (s *Server) handleMailboxes(w http.ResponseWriter, r *http.Request) {
	if s.userStore == nil {
		http.Error(w, "User management not available", http.StatusNotImplemented)
		return
	}

	switch r.Method {
	case http.MethodGet:
		users := s.userStore.ListUsers()
		out := make([]mailboxResponse, 0, len(users))
		for _, u := range users {
			out = append(out, mailboxResponse{Username: u.Username, Email: u.Email, Enabled: u.Enabled})
		}
		s.jsonResponse(w, http.StatusOK, out)

	case http.MethodPost:
		var req mailboxCreateRequest
		if !s.decodeMailboxBody(w, r, &req) {
			return
		}
		if req.Email == "" {
			req.Email = req.Username
		}
		if msg := validateIdentifier("username", req.Username); msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
		if msg := validateIdentifier("email", req.Email); msg == "" && !strings.Contains(req.Email, "@") {
			http.Error(w, "email must contain '@'", http.StatusBadRequest)
			return
		} else if msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
		if msg := validatePassword(req.Password); msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
		if _, exists := s.userStore.GetUser(req.Username); exists {
			http.Error(w, "Mailbox already exists", http.StatusConflict)
			return
		}
		if err := s.userStore.AddUser(req.Username, req.Password, req.Email); err != nil {
			s.logger.Error("Failed to create mailbox", zap.String("username", req.Username), zap.Error(err))
			http.Error(w, "Failed to create mailbox", http.StatusInternalServerError)
			return
		}
		s.logger.Info("Mailbox created via API", zap.String("username", req.Username))
		s.jsonResponse(w, http.StatusCreated, mailboxResponse{Username: req.Username, Email: req.Email, Enabled: true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMailbox serves /api/v1/mailboxes/{username} (GET, PUT update, DELETE).
func (s *Server) handleMailbox(w http.ResponseWriter, r *http.Request) {
	if s.userStore == nil {
		http.Error(w, "User management not available", http.StatusNotImplemented)
		return
	}

	username := strings.TrimPrefix(r.URL.Path, "/api/v1/mailboxes/")
	if username == "" || strings.Contains(username, "/") {
		http.Error(w, "Mailbox username required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		user, exists := s.userStore.GetUser(username)
		if !exists {
			http.Error(w, "Mailbox not found", http.StatusNotFound)
			return
		}
		s.jsonResponse(w, http.StatusOK, mailboxResponse{Username: user.Username, Email: user.Email, Enabled: user.Enabled})

	case http.MethodPut:
		user, exists := s.userStore.GetUser(username)
		if !exists {
			http.Error(w, "Mailbox not found", http.StatusNotFound)
			return
		}
		var req mailboxUpdateRequest
		if !s.decodeMailboxBody(w, r, &req) {
			return
		}
		if req.Password == "" && req.Email == "" {
			http.Error(w, "Nothing to update: provide password and/or email", http.StatusBadRequest)
			return
		}
		email := user.Email
		if req.Email != "" {
			if msg := validateIdentifier("email", req.Email); msg != "" {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			if !strings.Contains(req.Email, "@") {
				http.Error(w, "email must contain '@'", http.StatusBadRequest)
				return
			}
			email = req.Email
		}
		if req.Password != "" {
			if msg := validatePassword(req.Password); msg != "" {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			// AddUser hashes and upserts both memory and the repository.
			if err := s.userStore.AddUser(username, req.Password, email); err != nil {
				s.logger.Error("Failed to update mailbox", zap.String("username", username), zap.Error(err))
				http.Error(w, "Failed to update mailbox", http.StatusInternalServerError)
				return
			}
		} else {
			// Email-only update must not touch the password hash, so it cannot
			// go through AddUser (which re-hashes a plaintext password).
			if err := s.userStore.UpdateEmail(username, email); err != nil {
				s.logger.Error("Failed to update mailbox email", zap.String("username", username), zap.Error(err))
				http.Error(w, "Failed to update mailbox", http.StatusInternalServerError)
				return
			}
		}
		s.logger.Info("Mailbox updated via API", zap.String("username", username))
		s.jsonResponse(w, http.StatusOK, mailboxResponse{Username: username, Email: email, Enabled: user.Enabled})

	case http.MethodDelete:
		if err := s.userStore.DeleteUser(username); err != nil {
			if errors.Is(err, auth.ErrUserNotFound) {
				http.Error(w, "Mailbox not found", http.StatusNotFound)
				return
			}
			s.logger.Error("Failed to delete mailbox", zap.String("username", username), zap.Error(err))
			http.Error(w, "Failed to delete mailbox", http.StatusInternalServerError)
			return
		}
		s.logger.Info("Mailbox deleted via API", zap.String("username", username))
		s.jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted", "username": username})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// decodeMailboxBody decodes a size-capped JSON body, writing an HTTP error
// and returning false on failure.
func (s *Server) decodeMailboxBody(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxMailboxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// validateIdentifier bounds usernames and emails: non-empty, length-capped,
// and free of whitespace/control characters that could corrupt logs or
// protocol lines.
func validateIdentifier(field, value string) string {
	if value == "" {
		return field + " is required"
	}
	if len(value) > maxIdentifierLen {
		return field + " too long"
	}
	for _, c := range value {
		if c <= ' ' || c == 0x7f {
			return field + " contains whitespace or control characters"
		}
	}
	return ""
}

// validatePassword enforces length bounds. The upper bound also guards the
// bcrypt cost path against oversized input.
func validatePassword(password string) string {
	if len(password) < minPasswordLen {
		return "password must be at least 12 characters"
	}
	if len(password) > maxPasswordLen {
		return "password too long (max 128 characters)"
	}
	return ""
}
