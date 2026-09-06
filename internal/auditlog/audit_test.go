package auditlog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditChainDetectsEditsAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.Append("officer", "release", "id"); err != nil {
		t.Fatal(err)
	}
	l, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.Append("officer", "export", "id"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	n, head, err := Verify(bytes.NewReader(raw))
	if err != nil || n != 2 || head == "" {
		t.Fatal(n, head, err)
	}
	tampered := bytes.Replace(raw, []byte("release"), []byte("deleted"), 1)
	if _, _, err := Verify(bytes.NewReader(tampered)); err == nil {
		t.Fatal("tampering accepted")
	}
}
