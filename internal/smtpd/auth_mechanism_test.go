package smtpd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/emersion/go-sasl"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

// Threats: go-smtp only advertises/handles AUTH for a Session implementing
// its AuthSession interface (AuthMechanisms + Auth). A Session that merely
// defines a same-looking method under a different name (e.g. "AuthPlain")
// compiles fine but is never called — the server silently never offers
// AUTH, so no account, however strong its password, can ever be used to
// send mail. These tests exercise the actual interface the library calls,
// not just that credential-checking logic exists somewhere.

func newAuthTestSession(t *testing.T, mechanisms []string) (*Session, string, string) {
	t.Helper()
	validator := auth.NewValidator(zap.NewNop())
	if err := validator.GetUserStore().AddUser("meow", "correct-horse-battery-staple", "meow@gomeow.media"); err != nil {
		t.Fatalf("AddUser() error = %v", err)
	}
	cfg := &config.Config{}
	cfg.Server.AuthMechanisms = mechanisms
	return &Session{logger: zap.NewNop(), validator: validator, config: cfg, ip: "203.0.113.9"}, "meow", "correct-horse-battery-staple"
}

func TestSessionImplementsAuthSession(t *testing.T) {
	s, _, _ := newAuthTestSession(t, []string{"PLAIN"})
	var _ interface {
		AuthMechanisms() []string
		Auth(mech string) (sasl.Server, error)
	} = s
}

func TestAuthMechanismsReflectsConfig(t *testing.T) {
	s, _, _ := newAuthTestSession(t, []string{"PLAIN"})
	got := s.AuthMechanisms()
	if len(got) != 1 || got[0] != "PLAIN" {
		t.Fatalf("AuthMechanisms() = %v, want [PLAIN]", got)
	}
}

func TestAuthPlainSucceedsWithCorrectCredentials(t *testing.T) {
	s, username, password := newAuthTestSession(t, []string{"PLAIN"})

	server, err := s.Auth(sasl.Plain)
	if err != nil {
		t.Fatalf("Auth(PLAIN) error = %v", err)
	}

	// Mirrors what conn.go does with the client's initial response: identity,
	// username, and password NUL-separated (RFC 4616).
	response := []byte("\x00" + username + "\x00" + password)
	if _, done, err := server.Next(response); err != nil || !done {
		t.Fatalf("SASL exchange failed: done=%v err=%v", done, err)
	}
	if !s.authenticated {
		t.Fatal("session not marked authenticated after successful SASL PLAIN exchange")
	}
	if s.username != username {
		t.Fatalf("s.username = %q, want %q", s.username, username)
	}
}

func TestAuthPlainRejectsWrongPassword(t *testing.T) {
	s, username, _ := newAuthTestSession(t, []string{"PLAIN"})

	server, err := s.Auth(sasl.Plain)
	if err != nil {
		t.Fatalf("Auth(PLAIN) error = %v", err)
	}

	response := []byte("\x00" + username + "\x00wrong-password")
	if _, _, err := server.Next(response); err == nil {
		t.Fatal("SASL exchange succeeded with the wrong password, want an error")
	}
	if s.authenticated {
		t.Fatal("session marked authenticated after a failed SASL PLAIN exchange")
	}
}

func TestAuthRejectsUnknownMechanism(t *testing.T) {
	s, _, _ := newAuthTestSession(t, []string{"PLAIN"})
	if _, err := s.Auth("GSSAPI"); err == nil {
		t.Fatal("Auth(GSSAPI) = nil error, want a rejection for an unimplemented mechanism")
	}
}

func TestLoadConfigRejectsUnimplementedAuthMechanism(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yamlContent := "server:\n  auth_mechanisms: [\"CRAM-MD5\"]\n"
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := config.LoadConfig(path); err == nil {
		t.Fatal("LoadConfig() with an unimplemented mechanism = nil error, want a fail-fast rejection")
	}
}
