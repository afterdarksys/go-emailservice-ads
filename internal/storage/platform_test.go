package storage

import (
	"context"
	"encoding/json"
	"go.uber.org/zap"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExclusiveOwnershipAndCompaction(t *testing.T) {
	path := t.TempDir()
	s, e := NewMessageStore(path, zap.NewNop())
	if e != nil {
		t.Fatal(e)
	}
	if other, e := NewMessageStore(path, zap.NewNop()); e == nil {
		other.Close()
		t.Fatal("two writers accepted")
	}
	id, _, e := s.Store(&JournalEntry{Data: []byte("held data"), Status: "held", Tier: "out"})
	if e != nil {
		t.Fatal(e)
	}
	if ok, e := s.Transition(id, "held", "deleted"); e != nil || !ok {
		t.Fatal(e)
	}
	keep, _, e := s.Store(&JournalEntry{Data: []byte("keep"), Tier: "out"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Compact(0); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = NewMessageStore(path, zap.NewNop())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.Get(id); e == nil {
		t.Fatal("deleted payload resurrected")
	}
	if v, e := s.Get(keep); e != nil || string(v.Data) != "keep" {
		t.Fatalf("checkpoint lost message: %v", e)
	}
}
func TestClaimsAndSnapshots(t *testing.T) {
	s, e := NewMessageStore(t.TempDir(), zap.NewNop())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id, _, _ := s.Store(&JournalEntry{To: []string{"a@example.test"}, Data: []byte("body"), Tier: "out"})
	if ok, e := s.Transition(id, "pending", "queued"); e != nil || !ok {
		t.Fatal(e)
	}
	if ok, _ := s.Transition(id, "pending", "queued"); ok {
		t.Fatal("duplicate claim")
	}
	entry, _ := s.Get(id)
	entry.To[0] = "corrupt"
	entry.Data[0] = 'X'
	got, _ := s.Get(id)
	if got.To[0] != "a@example.test" || string(got.Data) != "body" || got.Attempts != 0 {
		t.Fatal("mutable snapshot or premature attempt")
	}
	s.Transition(id, "queued", "processing")
	got, _ = s.Get(id)
	if got.Attempts != 1 {
		t.Fatal("attempt not recorded")
	}
}
func TestMailboxQuotaAndRetryIdempotency(t *testing.T) {
	s, _ := NewMessageStore(t.TempDir(), zap.NewNop())
	defer s.Close()
	m, e := NewMailboxStore(NewIMAPAdapter(s), filepath.Join(t.TempDir(), "mailbox.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	data := []byte("Subject: x\r\n\r\nbody")
	m.SetQuota(int64(len(data)))
	first, e := m.DeliverOnce(context.Background(), "queue/recipient", "user", "INBOX", data)
	if e != nil {
		t.Fatal(e)
	}
	second, e := m.DeliverOnce(context.Background(), "queue/recipient", "user", "INBOX", data)
	if e != nil || first != second {
		t.Fatalf("retry duplicated %s %s %v", first, second, e)
	}
	if _, e = m.DeliverOnce(context.Background(), "different", "user", "INBOX", data); e == nil {
		t.Fatal("quota exceeded")
	}
}
func TestRetentionRemovesHeldOnly(t *testing.T) {
	s, _ := NewMessageStore(t.TempDir(), zap.NewNop())
	defer s.Close()
	id, _, _ := s.Store(&JournalEntry{Status: "held", Data: []byte("old"), CreatedAt: time.Now().Add(-48 * time.Hour)})
	pending, _, _ := s.Store(&JournalEntry{Status: "pending", Data: []byte("pending"), CreatedAt: time.Now().Add(-48 * time.Hour)})
	if e := s.Compact(24 * time.Hour); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Get(id); e == nil {
		t.Fatal("held message not expired")
	}
	if _, e := s.Get(pending); e != nil {
		t.Fatal("pending message expired")
	}
}

type partialJournalWriter struct{ file *os.File }

func (w partialJournalWriter) Write(b []byte) (int, error) {
	n, err := w.file.Write(b[:len(b)/2])
	if err != nil {
		return n, err
	}
	return n, io.ErrShortWrite
}
func TestPartialJournalAppendIsRolledBack(t *testing.T) {
	path := t.TempDir()
	j, err := NewJournal(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err = j.Write(&JournalEntry{MessageID: "first", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	j.encoder = json.NewEncoder(partialJournalWriter{j.file})
	if err = j.Write(&JournalEntry{MessageID: "partial", Status: "pending"}); err == nil {
		t.Fatal("short write accepted")
	}
	if err = j.Write(&JournalEntry{MessageID: "last", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	entries, err := j.Replay()
	if err != nil || len(entries) != 2 || entries[0].MessageID != "first" || entries[1].MessageID != "last" {
		t.Fatalf("rollback corrupted replay: %v %v", entries, err)
	}
}
