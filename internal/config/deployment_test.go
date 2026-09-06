package config

import (
	"bytes"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDeploymentConfigsLoad(t *testing.T) {
	t.Setenv("MAILHUB_ADMIN_KEY", "test-admin-key")
	t.Setenv("MAILHUB_DIRECTORY_KEY", "test-directory-key")
	t.Setenv("MAILHUB_DATA_DIR", t.TempDir())
	raw, err := os.ReadFile("../../deploy/kubernetes/base/configmap.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	count := 0
	for {
		var document struct {
			Data map[string]string `yaml:"data"`
		}
		err = dec.Decode(&document)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err = os.WriteFile(path, []byte(document.Data["config.yaml"]), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Platform.ScannerRequired || !cfg.Platform.ValidateRecipients || len(cfg.Platform.Listeners) == 0 {
			t.Fatal("deployment protections missing")
		}
		count++
	}
	if count != 2 {
		t.Fatalf("wanted both roles, got %d", count)
	}
}
