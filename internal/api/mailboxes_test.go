package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

const testAPIKey = "test-api-key-for-mailbox-tests"

func newMailboxTestServer(t *testing.T, requireIP bool) (*Server, *auth.UserStore) {
	t.Helper()

	cfg := &config.Config{}
	cfg.API.APIKeys = []config.APIKeyConfig{{Name: "test", Key: testAPIKey, Permissions: []string{"mailboxes:read", "mailboxes:write"}}}
	cfg.API.RequireIPAuth = requireIP
	if requireIP {
		cfg.API.AllowedIPs = []string{"10.9.9.9"}
	}

	validator := auth.NewValidator(zap.NewNop())
	store := validator.GetUserStore()
	store.SetLogger(zap.NewNop())

	s := &Server{
		config:    cfg,
		logger:    zap.NewNop(),
		userStore: store,
		startTime: time.Now(),
	}
	return s, store
}

func doMailboxRequest(s *Server, method, path, apiKey string, body interface{}) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.RemoteAddr = "127.0.0.1:54321"
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	rec := httptest.NewRecorder()
	s.buildMux().ServeHTTP(rec, req)
	return rec
}

func TestMailboxCreateListDelete(t *testing.T) {
	s, store := newMailboxTestServer(t, false)

	// Create
	rec := doMailboxRequest(s, http.MethodPost, "/api/v1/mailboxes", testAPIKey, map[string]string{
		"username": "hello@purrr.email",
		"password": "a-long-enough-password",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("create response must not echo any password material: %s", rec.Body.String())
	}

	// The created user must authenticate with the given password.
	if _, err := store.Authenticate("hello@purrr.email", "a-long-enough-password"); err != nil {
		t.Fatalf("created user failed to authenticate: %v", err)
	}

	// Email defaults to the username.
	user, ok := store.GetUser("hello@purrr.email")
	if !ok || user.Email != "hello@purrr.email" {
		t.Fatalf("expected email to default to username, got %+v", user)
	}

	// List
	rec = doMailboxRequest(s, http.MethodGet, "/api/v1/mailboxes", testAPIKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: got %d, want 200", rec.Code)
	}
	var listed []mailboxResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("list: bad JSON: %v", err)
	}
	if len(listed) != 1 || listed[0].Username != "hello@purrr.email" {
		t.Fatalf("list: unexpected content: %+v", listed)
	}
	if strings.Contains(rec.Body.String(), "hash") || strings.Contains(rec.Body.String(), "$2a$") {
		t.Fatalf("list must never expose password hashes: %s", rec.Body.String())
	}

	// Get single
	rec = doMailboxRequest(s, http.MethodGet, "/api/v1/mailboxes/hello@purrr.email", testAPIKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: got %d, want 200", rec.Code)
	}

	// Delete
	rec = doMailboxRequest(s, http.MethodDelete, "/api/v1/mailboxes/hello@purrr.email", testAPIKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: got %d, want 200", rec.Code)
	}
	if _, ok := store.GetUser("hello@purrr.email"); ok {
		t.Fatal("user still present after delete")
	}

	// Delete again → 404
	rec = doMailboxRequest(s, http.MethodDelete, "/api/v1/mailboxes/hello@purrr.email", testAPIKey, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete: got %d, want 404", rec.Code)
	}
}

func TestMailboxCreateDuplicateConflicts(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)

	body := map[string]string{"username": "dup@example.com", "password": "a-long-enough-password"}
	if rec := doMailboxRequest(s, http.MethodPost, "/api/v1/mailboxes", testAPIKey, body); rec.Code != http.StatusCreated {
		t.Fatalf("first create: got %d", rec.Code)
	}
	if rec := doMailboxRequest(s, http.MethodPost, "/api/v1/mailboxes", testAPIKey, body); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create: got %d, want 409", rec.Code)
	}
}

func TestMailboxUpdate(t *testing.T) {
	s, store := newMailboxTestServer(t, false)

	create := map[string]string{"username": "u@example.com", "password": "original-password-123"}
	if rec := doMailboxRequest(s, http.MethodPost, "/api/v1/mailboxes", testAPIKey, create); rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d", rec.Code)
	}

	// Password update
	rec := doMailboxRequest(s, http.MethodPut, "/api/v1/mailboxes/u@example.com", testAPIKey, map[string]string{
		"password": "rotated-password-456",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if _, err := store.Authenticate("u@example.com", "rotated-password-456"); err != nil {
		t.Fatalf("rotated password does not authenticate: %v", err)
	}
	if _, err := store.Authenticate("u@example.com", "original-password-123"); err == nil {
		t.Fatal("old password still authenticates after rotation")
	}

	// Email-only update keeps the password working.
	rec = doMailboxRequest(s, http.MethodPut, "/api/v1/mailboxes/u@example.com", testAPIKey, map[string]string{
		"email": "changed@example.com",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("email update: got %d", rec.Code)
	}
	user, _ := store.GetUser("u@example.com")
	if user == nil || user.Email != "changed@example.com" {
		t.Fatalf("email not updated: %+v", user)
	}
	if _, err := store.Authenticate("u@example.com", "rotated-password-456"); err != nil {
		t.Fatalf("password broken by email-only update: %v", err)
	}

	// Update on missing user → 404
	rec = doMailboxRequest(s, http.MethodPut, "/api/v1/mailboxes/missing@example.com", testAPIKey, map[string]string{
		"password": "whatever-password-789",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update missing: got %d, want 404", rec.Code)
	}

	// Empty update → 400
	rec = doMailboxRequest(s, http.MethodPut, "/api/v1/mailboxes/u@example.com", testAPIKey, map[string]string{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty update: got %d, want 400", rec.Code)
	}
}

func TestMailboxRejectsBadInput(t *testing.T) {
	s, store := newMailboxTestServer(t, false)

	cases := []struct {
		name string
		body map[string]string
	}{
		{"missing password", map[string]string{"username": "x@example.com"}},
		{"short password", map[string]string{"username": "x@example.com", "password": "short"}},
		{"oversized password", map[string]string{"username": "x@example.com", "password": strings.Repeat("a", 129)}},
		{"missing username", map[string]string{"password": "a-long-enough-password"}},
		{"whitespace username", map[string]string{"username": "bad user@example.com", "password": "a-long-enough-password"}},
		{"control-char username", map[string]string{"username": "bad\nuser@example.com", "password": "a-long-enough-password"}},
		{"overlong username", map[string]string{"username": strings.Repeat("a", 300) + "@e.com", "password": "a-long-enough-password"}},
		{"email without @", map[string]string{"username": "x@example.com", "password": "a-long-enough-password", "email": "not-an-email"}},
	}

	for _, tc := range cases {
		rec := doMailboxRequest(s, http.MethodPost, "/api/v1/mailboxes", testAPIKey, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400 (body: %s)", tc.name, rec.Code, rec.Body.String())
		}
	}

	// Unknown fields are rejected (typo'd payloads must not half-apply).
	rec := doMailboxRequest(s, http.MethodPost, "/api/v1/mailboxes", testAPIKey, map[string]string{
		"username": "x@example.com", "password": "a-long-enough-password", "pasword_typo": "x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: got %d, want 400", rec.Code)
	}

	if users := store.ListUsers(); len(users) != 0 {
		t.Fatalf("invalid requests must not create users, store has %d", len(users))
	}
}

func TestMailboxAuthRequired(t *testing.T) {
	s, store := newMailboxTestServer(t, false)

	// No credentials
	if rec := doMailboxRequest(s, http.MethodGet, "/api/v1/mailboxes", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list: got %d, want 401", rec.Code)
	}

	// Wrong API key — and it must not create anything.
	rec := doMailboxRequest(s, http.MethodPost, "/api/v1/mailboxes", "wrong-key", map[string]string{
		"username": "evil@example.com", "password": "a-long-enough-password",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-key create: got %d, want 401", rec.Code)
	}
	if _, ok := store.GetUser("evil@example.com"); ok {
		t.Fatal("unauthorized request created a user")
	}
}

func TestMailboxIPAllowlistEnforced(t *testing.T) {
	s, store := newMailboxTestServer(t, true) // allowlist = 10.9.9.9 only

	rec := doMailboxRequest(s, http.MethodPost, "/api/v1/mailboxes", testAPIKey, map[string]string{
		"username": "evil@example.com", "password": "a-long-enough-password",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-allowlisted IP with valid key: got %d, want 403", rec.Code)
	}
	if _, ok := store.GetUser("evil@example.com"); ok {
		t.Fatal("non-allowlisted request created a user")
	}
}

// TestMailboxIPAllowlistIgnoresSpoofedHeaders proves a caller from a
// non-allowlisted address cannot bypass require_ip_auth by supplying
// X-Forwarded-For / X-Real-IP headers naming an allowlisted address.
func TestMailboxIPAllowlistIgnoresSpoofedHeaders(t *testing.T) {
	s, store := newMailboxTestServer(t, true) // allowlist = 10.9.9.9 only

	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		raw, _ := json.Marshal(map[string]string{
			"username": "evil@example.com", "password": "a-long-enough-password",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mailboxes", bytes.NewReader(raw))
		req.RemoteAddr = "203.0.113.50:44444" // not allowlisted
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		req.Header.Set(header, "10.9.9.9") // spoofed allowlisted address

		rec := httptest.NewRecorder()
		s.buildMux().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("spoofed %s: got %d, want 403", header, rec.Code)
		}
	}
	if _, ok := store.GetUser("evil@example.com"); ok {
		t.Fatal("spoofed-header request created a user")
	}
}

func TestMailboxOversizedBodyRejected(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)

	big := bytes.NewReader(bytes.Repeat([]byte("a"), maxMailboxBodyBytes+1024))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mailboxes", big)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	rec := httptest.NewRecorder()
	s.buildMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body: got %d, want 400", rec.Code)
	}
}

// TestMailboxPersistenceRoundTrip proves API-created users survive a restart
// when the SQLite-backed repository is attached, and that deletes persist too.
func TestMailboxPersistenceRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "users.db")

	// "First boot": create two users via the API.
	s, store := newMailboxTestServer(t, false)
	repo, err := auth.NewUserRepository(dbPath, zap.NewNop())
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	if err := store.SetRepository(repo); err != nil {
		t.Fatalf("set repo: %v", err)
	}

	for _, u := range []string{"a@example.com", "b@example.com"} {
		rec := doMailboxRequest(s, http.MethodPost, "/api/v1/mailboxes", testAPIKey, map[string]string{
			"username": u, "password": "a-long-enough-password",
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: got %d (body: %s)", u, rec.Code, rec.Body.String())
		}
	}
	// Delete one before "restart".
	if rec := doMailboxRequest(s, http.MethodDelete, "/api/v1/mailboxes/b@example.com", testAPIKey, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: got %d", rec.Code)
	}
	repo.Close()

	// "Second boot": a fresh store loading from the same database.
	validator2 := auth.NewValidator(zap.NewNop())
	store2 := validator2.GetUserStore()
	repo2, err := auth.NewUserRepository(dbPath, zap.NewNop())
	if err != nil {
		t.Fatalf("reopen repo: %v", err)
	}
	defer repo2.Close()
	if err := store2.SetRepository(repo2); err != nil {
		t.Fatalf("set repo on second store: %v", err)
	}

	if _, err := store2.Authenticate("a@example.com", "a-long-enough-password"); err != nil {
		t.Fatalf("persisted user failed to authenticate after reload: %v", err)
	}
	if _, ok := store2.GetUser("b@example.com"); ok {
		t.Fatal("deleted user resurrected after reload")
	}
}

func TestMailboxPasswordByteBoundaries(t *testing.T) {
	for _, password := range []string{strings.Repeat("a", 72), strings.Repeat("é", 36)} {
		s, store := newMailboxTestServer(t, false)
		name := "boundary@example.test"
		rec := doMailboxRequest(s, "POST", "/api/v1/mailboxes", testAPIKey, map[string]string{"username": name, "password": password})
		if rec.Code != 201 {
			t.Fatalf("72-byte create: %d %s", rec.Code, rec.Body.String())
		}
		if _, err := store.Authenticate(name, password); err != nil {
			t.Fatal(err)
		}
		for _, tooLong := range []string{password + "a", strings.Repeat("é", 37)} {
			rec = doMailboxRequest(s, "POST", "/api/v1/mailboxes", testAPIKey, map[string]string{"username": "rejected@example.test", "password": tooLong})
			if rec.Code != 400 {
				t.Fatalf("oversized create: %d %s", rec.Code, rec.Body.String())
			}
			if _, exists := store.GetUser("rejected@example.test"); exists {
				t.Fatal("rejected account persisted")
			}
			rec = doMailboxRequest(s, "PUT", "/api/v1/mailboxes/"+name, testAPIKey, map[string]string{"password": tooLong})
			if rec.Code != 400 {
				t.Fatalf("oversized update: %d %s", rec.Code, rec.Body.String())
			}
			if _, err := store.Authenticate(name, password); err != nil {
				t.Fatalf("failed update changed password: %v", err)
			}
		}
	}
}
