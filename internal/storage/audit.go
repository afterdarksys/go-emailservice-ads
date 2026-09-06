package storage

import "fmt"

// Audit must succeed before a protected mutation or evidence export proceeds.
// Legacy audit.jsonl remains intact; new records use audit-chain.jsonl.
func (s *MessageStore) Audit(actor, action, id string) error {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	if s.closed {
		return fmt.Errorf("store closed")
	}
	if s.audit == nil {
		return fmt.Errorf("audit storage unavailable")
	}
	return s.audit.Append(actor, action, id)
}
