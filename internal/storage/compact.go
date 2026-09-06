package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type checkpoint struct {
	Covered []string `json:"covered"`
}

func syncDir(path string) error {
	d, e := os.Open(path)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func journalName() string {
	now := time.Now()
	return fmt.Sprintf("journal-%sz%09d.log", now.Format("20060102-150405"), now.Nanosecond())
}

// Compact atomically installs a checkpoint before deleting covered journal files.
// Recovery ignores covered logs even if interrupted partway through deletion.
func (s *MessageStore) Compact(retention time.Duration) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	j := s.journal
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failed != nil {
		return j.failed
	}
	if s.closed {
		return fmt.Errorf("store closed")
	}
	var expired []string
	if retention > 0 {
		for id, e := range s.index {
			if e.Status == "held" && time.Since(e.CreatedAt) > retention {
				v := cloneEntry(e)
				v.Status = "deleted"
				if err := j.writeLocked(v); err != nil {
					return err
				}
				expired = append(expired, id)
			}
		}
		if err := j.file.Sync(); err != nil {
			return err
		}
	}
	for _, id := range expired {
		delete(s.index, id)
	}
	old, err := filepath.Glob(filepath.Join(j.basePath, "journal-*.log"))
	if err != nil {
		return err
	}
	path := filepath.Join(j.basePath, journalName())
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			f.Close()
			os.Remove(path)
		}
	}()
	encoder := json.NewEncoder(f)
	for _, entry := range s.index {
		if err = encoder.Encode(entry); err != nil {
			f.Close()
			os.Remove(path)
			return err
		}
	}
	if err = f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err = syncDir(j.basePath); err != nil {
		f.Close()
		return err
	}
	covered := checkpoint{}
	for _, p := range old {
		covered.Covered = append(covered.Covered, filepath.Base(p))
	}
	tmp, err := os.CreateTemp(j.basePath, ".checkpoint-*")
	if err != nil {
		f.Close()
		return err
	}
	defer os.Remove(tmp.Name())
	err = json.NewEncoder(tmp).Encode(covered)
	if err == nil {
		err = tmp.Sync()
	}
	tmp.Close()
	if err != nil {
		f.Close()
		return err
	}
	if err = os.Rename(tmp.Name(), filepath.Join(j.basePath, "checkpoint.json")); err != nil {
		f.Close()
		return err
	}
	committed = true
	j.file.Close()
	j.file = f
	j.encoder = encoder
	if err = syncDir(j.basePath); err != nil {
		return err
	}
	for _, p := range old {
		if err = os.Remove(p); err != nil {
			return err
		}
	}
	// Tier files are an expendable secondary index; remove retired payloads.
	tierFiles, _ := filepath.Glob(filepath.Join(s.basePath, "tiers", "*", "*.json"))
	for _, p := range tierFiles {
		id := filepath.Base(p)
		id = id[:len(id)-5]
		if _, ok := s.index[id]; !ok {
			if err = os.Remove(p); err != nil {
				return err
			}
		}
	}
	return syncDir(j.basePath)
}
