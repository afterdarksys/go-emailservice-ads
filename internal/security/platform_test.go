package security

import (
	"context"
	"encoding/json"
	"errors"
	"go.uber.org/zap"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSTSPersistentPolicySurvivesDNSFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sts.json")
	m := NewMTASTSManager(zap.NewNop())
	if e := m.SetCachePath(path); e != nil {
		t.Fatal(e)
	}
	m.cache["example.test"] = &MTASTSCacheEntry{Policy: &MTASTSPolicy{Version: "STSv1", Mode: "enforce", MX: []string{"*.example.test"}, MaxAge: 3600}, ExpiresAt: time.Now().Add(time.Hour)}
	if e := m.saveCache(); e != nil {
		t.Fatal(e)
	}
	restored := NewMTASTSManager(zap.NewNop())
	if e := restored.SetCachePath(path); e != nil {
		t.Fatal(e)
	}
	restored.lookupTXT = func(context.Context, string) ([]string, error) { return nil, errors.New("DNS down") }
	if enforce, e := restored.ShouldEnforceTLS(context.Background(), "example.test", "mx.example.test."); e != nil || !enforce {
		t.Fatalf("cached enforcement lost: %v %v", enforce, e)
	}
	if _, e := restored.ShouldEnforceTLS(context.Background(), "example.test", "example.test"); e == nil {
		t.Fatal("wildcard matched bare domain")
	}
	if _, e := m.parsePolicy("version: STSv1\nmode: enforce\nmx: mx.example.test\nmax_age: nope\n"); e == nil {
		t.Fatal("invalid max_age accepted")
	}
}
func TestTLSReportFailedUploadRemainsDurable(t *testing.T) {
	dir := t.TempDir()
	reports, e := NewDurableTLSReports(dir, "mail.test")
	if e != nil {
		t.Fatal(e)
	}
	if e = reports.Record("example.test", "mx.example.test", "sts", false); e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	raw, _ := os.ReadFile(files[0])
	var report TLSRPTReport
	json.Unmarshal(raw, &report)
	report.DateRange.EndDatetime = time.Now().Add(-time.Hour)
	atomicJSON(files[0], report)
	status := 503
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got TLSRPTReport
		if e := json.NewDecoder(r.Body).Decode(&got); e != nil || len(got.Policies) != 1 || got.Policies[0].Policy.PolicyDomain != "example.test" {
			t.Error("wrong report domain")
		}
		w.WriteHeader(status)
	}))
	defer endpoint.Close()
	reports.client = endpoint.Client()
	reports.lookup = func(context.Context, string) ([]string, error) {
		return []string{"v=TLSRPTv1; rua=" + endpoint.URL}, nil
	}
	if e = reports.SendPending(context.Background()); e == nil {
		t.Fatal("failed upload reported successful")
	}
	if _, e = os.Stat(files[0]); e != nil {
		t.Fatal("failed report deleted")
	}
	status = 200
	if e = reports.SendPending(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(files[0]); !os.IsNotExist(e) {
		t.Fatal("successful report not acknowledged")
	}
}
