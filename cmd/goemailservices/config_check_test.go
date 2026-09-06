package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCheckProcess(t *testing.T) {
	if os.Getenv("MAILHUB_CONFIG_CHECK_HELPER") == "1" {
		flag.CommandLine = flag.NewFlagSet("mailhub", flag.ExitOnError)
		os.Args = []string{"mailhub", "--check-config", "--config", os.Getenv("MAILHUB_CONFIG_CHECK_PATH")}
		main()
		os.Exit(0)
	}
	for _, tc := range []struct {
		name, content string
		success       bool
	}{
		{"valid", "platform:\n  data_dir: ./must-not-create\n", true},
		{"unknown", "platfrom: {}\n", false},
		{"bad-log-level", "logging:\n  level: invalid\n", false},
		{"missing", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if tc.name != "missing" {
				if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestConfigCheckProcess$")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "MAILHUB_CONFIG_CHECK_HELPER=1", "MAILHUB_CONFIG_CHECK_PATH="+path)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.success {
				t.Fatalf("exit=%v output=%s", err, out)
			}
			if tc.success && !strings.Contains(string(out), "Configuration valid") {
				t.Fatalf("output=%s", out)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if tc.name == "missing" {
				want = 0
			}
			if len(entries) != want {
				t.Fatalf("check-config created files: %v", entries)
			}
		})
	}
}
