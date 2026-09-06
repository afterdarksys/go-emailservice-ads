package api

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAPIKeyFileRotationAndExpiry(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	path := filepath.Join(t.TempDir(), "key")
	os.WriteFile(path, []byte("old-secret"), 0600)
	s.config.API.APIKeys = []config.APIKeyConfig{{Name: "rotating", KeyFiles: []string{path}, Permissions: []string{"queue:read"}}}
	if _, ok := s.authorizeKey("old-secret", "queue:read"); !ok {
		t.Fatal("old key denied")
	}
	os.WriteFile(path, []byte("new-secret"), 0600)
	if _, ok := s.authorizeKey("new-secret", "queue:read"); !ok {
		t.Fatal("rotation not visible")
	}
	if _, ok := s.authorizeKey("old-secret", "queue:read"); ok {
		t.Fatal("revoked key accepted")
	}
	s.config.API.APIKeys[0].ExpiresAt = time.Now().Add(-time.Second)
	if _, ok := s.authorizeKey("new-secret", "queue:read"); ok {
		t.Fatal("expired key accepted")
	}
}
