package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictConfig(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"top-level typo", "platfrom: {}\n", "platfrom"},
		{"nested typo", "server:\n  max_recipient: 5\n", "max_recipient"},
		{"nested sequence typo", "platform:\n  listeners:\n    - addr: ':2525'\n      role: perimeter\n      trusted_network: []\n", "trusted_network"},
		{"second document", "server: {}\n---\nplatform: {}\n", "exactly one"},
		{"empty second document", "server: {}\n---\n", "exactly one"},
		{"duplicate key", "server: {}\nserver: {}\n", "already defined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestStrictConfigPreservesDefaultsAliasesAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAILHUB_DATA_DIR", filepath.Join(dir, "uncreated"))
	t.Setenv("MAILHUB_STRICT_TEST_KEY", "test-secret")
	path := filepath.Join(dir, "config.yaml")
	raw := `server:
  tls: &tls
    cert: /missing/cert
    key: /missing/key
imap:
  tls: *tls
api:
  api_keys:
    - name: test
      key_env: MAILHUB_STRICT_TEST_KEY
      permissions: [queue:read]
platform:
  aliases:
    alias@example.test: [user@example.test]
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.MaxRecipients != 50 || cfg.IMAP.TLS.Cert != "/missing/cert" || cfg.API.APIKeys[0].Key != "test-secret" {
		t.Fatal("defaults/alias/secret resolution lost")
	}
	if _, err := os.Stat(cfg.Platform.DataDir); !os.IsNotExist(err) {
		t.Fatalf("validation touched data directory: %v", err)
	}
}
