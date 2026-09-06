package security

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRepeatedTLSFailuresAggregate(t *testing.T) {
	dir := t.TempDir()
	r, err := NewDurableTLSReports(dir, "mail.test")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err := r.Record("example.test", "mx.example.test", "sts", false); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Record("example.test", "backup.example.test", "sts", false); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("files: %v", files)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var report TLSRPTReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	p := report.Policies[0]
	if len(p.FailureDetails) != 2 || p.FailureDetails[0].FailedSessionCount != 30 || p.Summary.TotalFailureSessionCount != 31 {
		t.Fatalf("incorrect aggregation: %+v", p)
	}
}
