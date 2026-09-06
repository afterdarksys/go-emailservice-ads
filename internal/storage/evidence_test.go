package storage

import "testing"

func TestEvidenceBindsEnvelopeAndBytes(t *testing.T) {
	e := &JournalEntry{From: "sender@example.test", To: []string{"one@example.test"}, Data: []byte("original"), Metadata: map[string]string{}}
	e.Metadata["evidence_sha256"] = EvidenceHash(e.From, e.To, e.Data)
	if err := VerifyEvidence(e); err != nil {
		t.Fatal(err)
	}
	e.Data = []byte("modified")
	if err := VerifyEvidence(e); err == nil {
		t.Fatal("changed content accepted")
	}
	e.Data = []byte("original")
	e.To = []string{"other@example.test"}
	if err := VerifyEvidence(e); err == nil {
		t.Fatal("changed envelope accepted")
	}
}
