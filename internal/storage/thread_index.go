package storage

import (
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

// ThreadIdentity reads immutable headers in place without copying full message
// bodies. Cache entries are bounded and derived, so recovery needs no migration.
func (s *MessageStore) ThreadIdentity(id string) (string, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	entry, ok := s.index[id]
	if !ok {
		return "", fmt.Errorf("message not found")
	}
	if tid := s.threadIDs[id]; tid != "" {
		return tid, nil
	}
	tid := mailstate.ThreadID(id, entry.Data)
	if s.threadIDs == nil {
		s.threadIDs = map[string]string{}
	}
	if len(s.threadIDs) >= 100000 {
		for key := range s.threadIDs {
			delete(s.threadIDs, key)
			break
		}
	}
	s.threadIDs[id] = tid
	return tid, nil
}
