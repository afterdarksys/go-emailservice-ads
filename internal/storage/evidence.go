package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// EvidenceHash binds exact message bytes and the preserved SMTP envelope.
func EvidenceHash(from string, to []string, data []byte) string {
	envelope, _ := json.Marshal(struct {
		From string
		To   []string
	}{from, to})
	hash := sha256.New()
	hash.Write(envelope)
	hash.Write([]byte{0})
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}
func VerifyEvidence(e *JournalEntry) error {
	if e.Metadata["evidence_sha256"] == "" || e.Metadata["evidence_sha256"] != EvidenceHash(e.From, e.To, e.Data) {
		return fmt.Errorf("compliance evidence integrity check failed")
	}
	return nil
}
