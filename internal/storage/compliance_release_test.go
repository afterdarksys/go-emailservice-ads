package storage

import (
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"testing"
)

func TestIncompleteReleaseTransactionHasNoPartialEffect(t *testing.T) {
	root := t.TempDir()
	s, err := NewMessageStore(root, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Store(&JournalEntry{MessageID: "case", Status: "compliance", Tier: "compliance", Metadata: map[string]string{"compliance": "true"}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	files, err := filepath.Glob(filepath.Join(root, "journal", "journal-*.log"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	f, err := os.OpenFile(files[0], os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(`{"status":"transaction","transaction":[{"message_id":"case","status":"compliance_released"},`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	s, err = NewMessageStore(root, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entry, err := s.Get("case")
	if err != nil || entry.Status != "compliance" {
		t.Fatal("partial release applied", entry, err)
	}
}

func TestComplianceReleaseSurvivesRecoveryAndCompaction(t *testing.T) {
	root := t.TempDir()
	s, err := NewMessageStore(root, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	parent := &JournalEntry{MessageID: "case", From: "sender@test", To: []string{"recipient@test"}, Data: []byte("evidence"), Tier: "compliance", Status: "compliance", Metadata: map[string]string{"compliance": "true", "mode": "hold"}}
	parent.Metadata["evidence_sha256"] = EvidenceHash(parent.From, parent.To, parent.Data)
	if _, _, err = s.Store(parent); err != nil {
		t.Fatal(err)
	}
	child := &JournalEntry{MessageID: "release-case", From: parent.From, To: parent.To, Data: parent.Data, Tier: "out", Status: "pending"}
	if err = s.ReleaseCompliance("case", "officer", "approved", child); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = NewMessageStore(root, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if e, err := s.Get("case"); err != nil || e.Status != "compliance_released" {
		t.Fatal("decision not recovered", err)
	}
	if e, err := s.Get("release-case"); err != nil || e.Status != "pending" {
		t.Fatal("release lost", err)
	}
	if err = s.ReleaseCompliance("case", "officer", "again", child); err == nil {
		t.Fatal("duplicate release")
	}
	if err = s.UpdateStatus("release-case", "delivered", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.Compact(0); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = NewMessageStore(root, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Get("release-case"); err == nil {
		t.Fatal("compaction resurrected delivery")
	}
	if _, err = s.Get("case"); err != nil {
		t.Fatal("evidence lost", err)
	}
}
