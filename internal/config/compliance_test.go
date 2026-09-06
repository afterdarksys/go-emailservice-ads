package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnvironmentConfigOverridesDefaults(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yaml")
	raw := []byte("platform:\n  bounce:\n    max_attempts: 17\n    initial_delay: 7m\n    max_delay: 9h\n    include_original_headers: false\n  compliance:\n    rules:\n      - name: preservation\n        domain: example.test\n        mode: hold\n        retention: 48h\nlogging:\n  level: warn\n  format: yaml\n")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Platform.Bounce.Defaults()
	if c.MaxAttempts != 17 || c.InitialDelay != 7*time.Minute || c.MaxDelay != 9*time.Hour || *c.IncludeOriginalHeaders || cfg.Logging.Format != "yaml" || cfg.Platform.Compliance.Rules[0].Retention != 48*time.Hour {
		t.Fatal("file values lost to defaults")
	}
}
