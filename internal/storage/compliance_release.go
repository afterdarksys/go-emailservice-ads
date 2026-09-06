package storage

import (
	"fmt"
	"strings"
	"time"
)

// ReleaseCompliance atomically preserves the release decision and its outbox
// message. The parent remains evidence after the child is delivered/compacted.
func (s *MessageStore) ReleaseCompliance(id, actor, reason string, child *JournalEntry) error {
	if strings.TrimSpace(reason) == "" || len(reason) > 2048 {
		return fmt.Errorf("release reason required (max 2048 bytes)")
	}
	if err := s.Audit(actor, "compliance_release_requested:"+reason, id); err != nil {
		return err
	}
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	parent, ok := s.index[id]
	if !ok || parent.Metadata["compliance"] != "true" || parent.Status != "compliance" || parent.Metadata["legal_hold"] == "true" || parent.Metadata["mode"] == "copy" {
		return fmt.Errorf("release prohibited")
	}
	if err := VerifyEvidence(parent); err != nil {
		return err
	}
	if child == nil || child.MessageID != "release-"+id || child.Metadata["compliance"] == "true" || (child.Status != "pending" && child.Status != "held") {
		return fmt.Errorf("invalid release transaction")
	}
	if EvidenceHash(child.From, child.To, child.Data) != parent.Metadata["evidence_sha256"] {
		return fmt.Errorf("release envelope or bytes differ from preserved evidence")
	}
	if _, exists := s.index[child.MessageID]; exists {
		return fmt.Errorf("release transaction already exists")
	}
	if err := s.capacity(int64(len(child.Data))); err != nil {
		return err
	}
	if err := s.diskCapacity(int64(len(child.Data))*3 + 4096); err != nil {
		return err
	}
	saved := cloneEntry(parent)
	saved.Status = "compliance_released"
	saved.Metadata["release_id"] = child.MessageID
	saved.Metadata["released_by"] = actor
	saved.Metadata["released_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	child = cloneEntry(child)
	child.CreatedAt = time.Now()
	if err := s.journal.WriteBatch(saved, child); err != nil {
		return err
	}
	s.index[id] = saved
	s.index[child.MessageID] = child
	return nil
}
