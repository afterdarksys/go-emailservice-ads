package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Audit records operator intent durably before a security-sensitive mutation.
// This log is independent of queue compaction and contains no message bodies.
func (s *MessageStore) Audit(actor, action, id string) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.basePath, "audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = json.NewEncoder(f).Encode(map[string]string{"time": time.Now().UTC().Format(time.RFC3339Nano), "actor": actor, "action": action, "id": id}); err != nil {
		return err
	}
	return f.Sync()
}
