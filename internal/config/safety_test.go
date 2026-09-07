package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeSubmissionDefaultsAndInvalidOverrides(t *testing.T) {
	load := func(raw string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "config.yaml")
		os.WriteFile(p, []byte(raw), 0600)
		return LoadConfig(p)
	}
	c, e := load("server: {}\n")
	if e != nil {
		t.Fatal(e)
	}
	if c.Server.Addr != ":587" || !c.Server.RequireAuth || !c.Server.RequireTLS || c.Server.AllowInsecureAuth || len(c.Server.Relay.AllowedNetworks) != 0 {
		t.Fatal("unsafe defaults")
	}
	for _, raw := range []string{"server:\n  relay:\n    allowed_networks: [0.0.0.0/0]\n", "server:\n  relay:\n    allowed_networks: ['::/0']\n", "server:\n  allow_insecure_auth: true\n", "server:\n  auth_mechanisms: []\n", "server:\n  addr: ':8080'\n", "server:\n  role: submission\n  require_auth: false\n"} {
		if _, e = load(raw); e == nil {
			t.Fatal("unsafe override accepted", raw)
		}
	}
	newer := *c
	newer.Platform.DataDir = filepath.Join(t.TempDir(), "other")
	if e = newer.ValidateReloadFrom(c); e == nil || !strings.Contains(e.Error(), "migration") {
		t.Fatal(e)
	}
	newer = *c
	newer.Server.MaxRecipients++
	if e = newer.ValidateReloadFrom(c); e != nil {
		t.Fatal(e)
	}
}
