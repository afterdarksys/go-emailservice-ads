package storage

import "fmt"

// StoreNotification commits a per-source notification checkpoint and outbox
// message together so scheduler restarts cannot create duplicate notices.
func (s *MessageStore) StoreNotification(source, key string, child *JournalEntry) (bool, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	parent, ok := s.index[source]
	if !ok {
		return false, fmt.Errorf("notification source missing")
	}
	if parent.Metadata["notice:"+key] != "" {
		return false, nil
	}
	if _, exists := s.index[child.MessageID]; exists {
		return false, fmt.Errorf("notification ID collision")
	}
	if err := s.diskCapacity(int64(len(child.Data))*3 + 4096); err != nil {
		return false, err
	}
	saved := cloneEntry(parent)
	saved.Metadata["notice:"+key] = child.MessageID
	if err := s.journal.WriteBatch(saved, child); err != nil {
		return false, err
	}
	s.index[source] = saved
	if child.Status != "delivered" {
		s.index[child.MessageID] = cloneEntry(child)
	}
	return true, nil
}
