package auth

import (
	"errors"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"testing"
)

type testDirectory struct {
	fail  bool
	calls int
}

func (*testDirectory) Matches(string) bool { return true }
func (d *testDirectory) Authenticate(string, string) error {
	d.calls++
	if d.fail {
		return errors.New("outage")
	}
	return nil
}
func TestLDAPFailClosedAndLocalDisable(t *testing.T) {
	s := NewUserStore()
	if err := s.AddUser("alice@example.test", "local-password", "alice@example.test"); err != nil {
		t.Fatal(err)
	}
	d := &testDirectory{fail: true}
	s.directory = d
	if _, err := s.Authenticate("alice@example.test", "local-password"); err == nil {
		t.Fatal("local fallback")
	}
	d.fail = false
	if _, err := s.Authenticate("alice@example.test", "directory-password"); err != nil {
		t.Fatal(err)
	}
	disabled := false
	if err := s.UpdateAccount("alice@example.test", "", "", &disabled); err != nil {
		t.Fatal(err)
	}
	before := d.calls
	if _, err := s.Authenticate("alice@example.test", "directory-password"); err == nil || d.calls != before {
		t.Fatal("disabled user bound")
	}
	if _, err := s.Authenticate("unknown@example.test", "directory-password"); err == nil {
		t.Fatal("unprovisioned login")
	}
}
func TestLDAPRejectsPlaintext(t *testing.T) {
	if _, err := NewLDAPProvider(config.LDAPConfig{Enabled: true, URL: "ldap://directory.test"}); err == nil {
		t.Fatal("plaintext accepted")
	}
}
