package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIPFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, tc := range []struct {
		yaml  string
		valid bool
	}{
		{"server:\n  ip_filter:\n    denylist: [192.0.2.0/24]\n    allowlist: ['::1']\n    rbl_zones: [rbl.example]\n", true},
		{"server:\n  ip_filter:\n    denylist: [typo]\n", false},
	} {
		if err := os.WriteFile(path, []byte(tc.yaml), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(path)
		if (err == nil) != tc.valid {
			t.Fatalf("valid=%v error=%v", tc.valid, err)
		}
		if tc.valid && (len(cfg.Server.IPFilter.Denylist) != 1 || len(cfg.Server.IPFilter.RBLZones) != 1) {
			t.Fatal("IP configuration lost")
		}
	}
}
