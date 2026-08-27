package imap

import (
	"context"
	"net"
	"testing"

	goimap "github.com/emersion/go-imap"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
)

// masterTestStore is a no-op Store; master-login tests only exercise auth,
// never message I/O.
type masterTestStore struct{}

func (masterTestStore) GetMessages(context.Context, string, string) ([]MessageSummary, error) {
	return nil, nil
}
func (masterTestStore) FetchMessage(context.Context, string) ([]byte, error) { return nil, nil }
func (masterTestStore) StoreMessage(context.Context, string, string, []byte) (string, error) {
	return "", nil
}
func (masterTestStore) GetUIDValidity(context.Context, string, string) (uint32, error) {
	return 1, nil
}
func (masterTestStore) GetUIDNext(context.Context, string, string) (uint32, error) { return 1, nil }
func (masterTestStore) AllocateUID(context.Context, string, string) (uint32, error) {
	return 1, nil
}
func (masterTestStore) UpdateMessageFlags(context.Context, string, string, string, goimap.FlagsOp, []string) error {
	return nil
}
func (masterTestStore) ExpungeDeleted(context.Context, string, string) ([]string, error) {
	return nil, nil
}

const (
	tgtUser    = "alice@purrr.email"
	masterName = "gateway"
	masterPass = "correct-horse-battery-staple-9f3a"
	allowedIP  = "108.165.121.47"
)

func newMasterBackend(t *testing.T) *Backend {
	t.Helper()
	v := auth.NewValidator(zap.NewNop())
	if err := v.GetUserStore().AddUser(tgtUser, "unused-user-pw", tgtUser); err != nil {
		t.Fatalf("seed target user: %v", err)
	}
	cfg := MasterAuthConfig{
		User:       masterName,
		Password:   masterPass,
		AllowedIPs: []string{allowedIP},
		Separator:  "*",
	}
	return NewBackend(zap.NewNop(), masterTestStore{}, v, cfg)
}

func connFrom(ip string) *goimap.ConnInfo {
	return &goimap.ConnInfo{RemoteAddr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 51000}}
}

// Happy path: valid master creds from an allowlisted IP log in AS the target.
func TestMasterLogin_Success(t *testing.T) {
	b := newMasterBackend(t)
	u, err := b.Login(connFrom(allowedIP), tgtUser+"*"+masterName, masterPass)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if u.Username() != tgtUser {
		t.Fatalf("logged in as %q, want %q", u.Username(), tgtUser)
	}
}

// Negative: wrong master password must fail closed.
func TestMasterLogin_WrongPassword(t *testing.T) {
	b := newMasterBackend(t)
	if _, err := b.Login(connFrom(allowedIP), tgtUser+"*"+masterName, "wrong"); err == nil {
		t.Fatal("expected failure on wrong master password")
	}
}

// Negative: wrong master user name must fail closed.
func TestMasterLogin_WrongMasterName(t *testing.T) {
	b := newMasterBackend(t)
	if _, err := b.Login(connFrom(allowedIP), tgtUser+"*attacker", masterPass); err == nil {
		t.Fatal("expected failure on wrong master name")
	}
}

// Negative: correct creds from a non-allowlisted IP must be rejected.
func TestMasterLogin_UnlistedIP(t *testing.T) {
	b := newMasterBackend(t)
	if _, err := b.Login(connFrom("203.0.113.7"), tgtUser+"*"+masterName, masterPass); err == nil {
		t.Fatal("expected failure from non-allowlisted IP")
	}
}

// Negative: missing connection info (nil conn) must fail closed, not panic.
func TestMasterLogin_NilConn(t *testing.T) {
	b := newMasterBackend(t)
	if _, err := b.Login(nil, tgtUser+"*"+masterName, masterPass); err == nil {
		t.Fatal("expected failure with nil conn info")
	}
}

// Negative: target account that does not exist must be rejected.
func TestMasterLogin_UnknownTarget(t *testing.T) {
	b := newMasterBackend(t)
	if _, err := b.Login(connFrom(allowedIP), "ghost@purrr.email*"+masterName, masterPass); err == nil {
		t.Fatal("expected failure for unknown target mailbox")
	}
}

// Fail-closed: when master auth is not fully configured, a separator-bearing
// username must NOT be treated as a master login (falls through to normal auth,
// which has no such user -> error). Guards against half-config enabling god-mode.
func TestMasterLogin_DisabledWhenUnconfigured(t *testing.T) {
	v := auth.NewValidator(zap.NewNop())
	_ = v.GetUserStore().AddUser(tgtUser, "pw", tgtUser)
	// No AllowedIPs -> disabled.
	b := NewBackend(zap.NewNop(), masterTestStore{}, v, MasterAuthConfig{
		User:     masterName,
		Password: masterPass,
	})
	if _, err := b.Login(connFrom(allowedIP), tgtUser+"*"+masterName, masterPass); err == nil {
		t.Fatal("expected failure: master auth must be disabled without an allowlist")
	}
}

// Malformed: empty target or empty master name must be rejected.
func TestMasterLogin_Malformed(t *testing.T) {
	b := newMasterBackend(t)
	for _, u := range []string{"*" + masterName, tgtUser + "*", "*"} {
		if _, err := b.Login(connFrom(allowedIP), u, masterPass); err == nil {
			t.Fatalf("expected failure for malformed username %q", u)
		}
	}
}
