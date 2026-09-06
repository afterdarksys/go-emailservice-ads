package preservation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveRequiresVerifiedComplianceRetention(t *testing.T) {
	file := filepath.Join(t.TempDir(), "evidence")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	c := Config{SourceFile: file, Bucket: "bucket", RetainUntil: time.Now().Add(time.Hour).Truncate(time.Second), LegalHold: true}
	for _, mode := range []string{"COMPLIANCE", "GOVERNANCE"} {
		digest := ""
		runner := func(_ context.Context, args []string) ([]byte, error) {
			command := strings.Join(args, " ")
			if strings.Contains(command, "get-object-lock-configuration") {
				return []byte(`{"ObjectLockConfiguration":{"ObjectLockEnabled":"Enabled"}}`), nil
			}
			if strings.Contains(command, "put-object") {
				for i, a := range args {
					if a == "--metadata" {
						digest = strings.TrimPrefix(args[i+1], "sha256=")
					}
				}
				return []byte(`{"VersionId":"version"}`), nil
			}
			return json.Marshal(map[string]any{"ObjectLockMode": mode, "ObjectLockRetainUntilDate": c.RetainUntil, "ObjectLockLegalHoldStatus": "ON", "Metadata": map[string]string{"sha256": digest}})
		}
		receipt, err := Archive(context.Background(), c, runner)
		if mode == "COMPLIANCE" && (err != nil || receipt.SHA256 == "") {
			t.Fatal(receipt, err)
		}
		if mode == "GOVERNANCE" && err == nil {
			t.Fatal("weaker retention accepted")
		}
	}
}
