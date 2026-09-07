package storage

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

type Limits struct {
	MaxBytes     int64
	MaxMessages  int
	MinFreeBytes uint64
}

func cloneEntry(e *JournalEntry) *JournalEntry {
	v := *e
	v.Data = append([]byte(nil), e.Data...)
	v.To = append([]string(nil), e.To...)
	v.Metadata = make(map[string]string, len(e.Metadata))
	for k, x := range e.Metadata {
		v.Metadata[k] = x
	}
	return &v
}
func (s *MessageStore) SetLimits(l Limits) { s.indexMu.Lock(); defer s.indexMu.Unlock(); s.limits = l }
func (s *MessageStore) capacity(add int64) error {
	if err := s.diskCapacity(add); err != nil {
		return err
	}
	var size int64
	count := 0
	for _, e := range s.index {
		if e.Tier != "mailbox" && e.Tier != "jmap_submission" && e.Tier != "sieve_effect" {
			size += int64(len(e.Data))
			count++
		}
	}
	if s.limits.MaxBytes > 0 && size+add > s.limits.MaxBytes {
		return fmt.Errorf("spool byte limit reached")
	}
	if s.limits.MaxMessages > 0 && count >= s.limits.MaxMessages {
		return fmt.Errorf("spool message limit reached")
	}
	return nil
}
func (s *MessageStore) Health() error {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	if s.closed {
		return fmt.Errorf("store closed")
	}
	if err := s.capacity(0); err != nil {
		return err
	}
	s.journal.mu.RLock()
	defer s.journal.mu.RUnlock()
	if s.journal.failed != nil {
		return s.journal.failed
	}
	return s.journal.file.Sync()
}

// Transition is a compare-and-set; only one worker can own a pending attempt.
func (s *MessageStore) Transition(id, from, to string) (bool, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	e, ok := s.index[id]
	if !ok || e.Status != from {
		return false, nil
	}
	if e.Metadata["compliance"] == "true" {
		return false, fmt.Errorf("use compliance controls for preserved records")
	}
	v := cloneEntry(e)
	v.Status = to
	if to == "processing" {
		v.Attempts++
		v.LastAttempt = time.Now()
	}
	if err := s.journal.Write(v); err != nil {
		return false, err
	}
	if to == "deleted" {
		delete(s.index, id)
	} else {
		s.index[id] = v
	}
	return true, nil
}
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("mail storage already owned by another process: %w", err)
	}
	return f, nil
}

func (s *MessageStore) ReleaseHeld(id, actor string) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	e, ok := s.index[id]
	if !ok || e.Status != "held" {
		return fmt.Errorf("message is not held")
	}
	v := cloneEntry(e)
	v.Status = "pending"
	v.Metadata["released_by"] = actor
	v.Metadata["released_at"] = time.Now().UTC().Format(time.RFC3339)
	if err := s.journal.Write(v); err != nil {
		return err
	}
	s.index[id] = v
	return nil
}

func (s *MessageStore) diskCapacity(add int64) error {
	var st unix.Statfs_t
	if err := unix.Statfs(s.basePath, &st); err != nil {
		return err
	}
	if uint64(st.Bavail)*uint64(st.Bsize) < s.limits.MinFreeBytes+uint64(add) {
		return fmt.Errorf("storage free-space reserve reached")
	}

	return nil
}
