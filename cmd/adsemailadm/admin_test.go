package main

import (
	"bytes"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/spf13/cobra"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func serverFor(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	old := apiEndpoint
	apiEndpoint = s.URL
	t.Cleanup(func() { apiEndpoint = old })
}
func TestAPIErrorsAndRedirects(t *testing.T) {
	for _, status := range []int{302, 401, 403, 404, 409, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			serverFor(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/target")
				w.WriteHeader(status)
			})
			if resp, e := apiRequest("POST", "/api/v1/config/reload", nil); e == nil {
				resp.Body.Close()
				t.Fatal("error treated as success")
			}
		})
	}
}
func TestQueueResponsesAndBulkRetry(t *testing.T) {
	var ids []string
	serverFor(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/pending":
			w.Write([]byte(`[{"id":"abc","to":["user@example.test"],"attempts":3}]`))
		case "/api/v1/queue/stats":
			w.Write([]byte(`{"metrics":{"enqueued":17},"storage":{"total_messages":9}}`))
		case "/api/v1/dlq/list":
			w.Write([]byte(`[{"id":"a"},{"id":"b"}]`))
		default:
			ids = append(ids, r.URL.Path)
			w.Write([]byte(`{"status":"ok"}`))
		}
	})
	run := func(args ...string) string {
		t.Helper()
		cmd := queueCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		if e := cmd.Execute(); e != nil {
			t.Fatal(e)
		}
		return out.String()
	}
	if !strings.Contains(run("list"), "abc") {
		t.Fatal("pending lost")
	}
	if !strings.Contains(run("stats"), "17") {
		t.Fatal("stats lost")
	}
	run("retry", "--all")
	if len(ids) != 2 || ids[0] != "/api/v1/dlq/retry/a" || ids[1] != "/api/v1/dlq/retry/b" {
		t.Fatal(ids)
	}
}
func TestHealthReadinessFailure(t *testing.T) {
	serverFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			w.WriteHeader(503)
		} else {
			w.Write([]byte(`{"status":"ok"}`))
		}
	})
	cmd := healthCmd()
	cmd.SetOut(&bytes.Buffer{})
	if e := cmd.Execute(); e == nil {
		t.Fatal("unready treated healthy")
	}
}
func TestPlaceholderOperationsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		cmd  *cobra.Command
		args []string
	}{{clusterCmd(), []string{"rebalance"}}, {tlsCmd(), []string{"cert", "renew"}}, {directoryCmd(), []string{"sync"}}, {monitorCmd(), []string{"realtime"}}} {
		tc.cmd.SetArgs(tc.args)
		tc.cmd.SetOut(&bytes.Buffer{})
		tc.cmd.SetErr(&bytes.Buffer{})
		if e := tc.cmd.Execute(); e == nil {
			t.Fatal("placeholder succeeded", tc.args)
		}
	}
}

func TestSieveUsesConfiguredDirectoryAndValidates(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	data := filepath.Join(dir, "data")
	if e := os.WriteFile(cfg, []byte(config.DefaultDocument+"\nplatform:\n  data_dir: "+data+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	oldConfig := configFile
	configFile = cfg
	t.Cleanup(func() { configFile = oldConfig })
	src := filepath.Join(dir, "script")
	if e := os.WriteFile(src, []byte("keep;"), 0600); e != nil {
		t.Fatal(e)
	}
	run := func(user string) error {
		cmd := sieveCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"upload", user, src})
		return cmd.Execute()
	}
	if e := run("../alice"); e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(data, "sieve", "..%2Falice.sieve")
	before, e := os.ReadFile(target)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(src, []byte("unsupported-command;"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = run("../alice"); e == nil {
		t.Fatal("invalid sieve uploaded")
	}
	after, _ := os.ReadFile(target)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid upload changed script")
	}
}
func TestAPIKeyExpiryUsesCurrentSchema(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if e := os.WriteFile(cfg, []byte(config.DefaultDocument), 0600); e != nil {
		t.Fatal(e)
	}
	old := configFile
	configFile = cfg
	t.Cleanup(func() { configFile = old })
	cmd := apikeysCreateCmd()
	cmd.SetArgs([]string{"test", "--expires", "2030-01-01", "--permissions", "queue:read"})
	if e := cmd.Execute(); e != nil {
		t.Fatal(e)
	}
	c, e := config.LoadConfig(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if len(c.API.APIKeys) != 1 || c.API.APIKeys[0].ExpiresAt.Year() != 2030 {
		t.Fatal("expiry missing")
	}
}
