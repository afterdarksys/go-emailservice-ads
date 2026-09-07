package storage

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"go.uber.org/zap"
)

func scheduledFixture(t *testing.T, s *MessageStore, id string, at time.Time) string {
	t.Helper()
	rid := "jmap-submission-" + id
	data, _ := json.Marshal(mailstate.Submission{ID: id, UndoStatus: "pending", SendAt: at.UTC().Format(time.RFC3339)})
	_, _, err := s.StoreWithReceipt(&JournalEntry{MessageID: id, Tier: "out", Status: "scheduled", Data: []byte("mail"), Metadata: map[string]string{"receipt_id": rid}}, &JournalEntry{MessageID: rid, Tier: "jmap_submission", Status: "jmap_submission", CreatedAt: time.Now().Add(-48 * time.Hour), Metadata: map[string]string{"username": "alice", "queue_id": id, "submission": string(data)}})
	if err != nil {
		t.Fatal(err)
	}
	return rid
}
func TestScheduledCancelDispatchAtomicAndRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := NewMessageStore(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rid := scheduledFixture(t, s, "mail", now.Add(time.Hour))
	if ok, err := s.FinishScheduled(rid, "", false, now); err != nil || ok {
		t.Fatal(ok, err)
	}
	if _, err := s.FinishScheduled(rid, "bob", true, now); !errors.Is(err, mailstate.ErrBlobNotFound) {
		t.Fatal(err)
	}
	if count, err := s.ExpireWorkflowRecords(now, time.Hour, time.Hour); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, cancel := range []bool{true, false} {
		wg.Add(1)
		go func(cancel bool) {
			defer wg.Done()
			ok, err := s.FinishScheduled(rid, "alice", cancel, now.Add(2*time.Hour))
			if err == nil && ok {
				if cancel {
					results <- "canceled"
				} else {
					results <- "final"
				}
			}
		}(cancel)
	}
	wg.Wait()
	close(results)
	winner := ""
	for result := range results {
		if winner != "" {
			t.Fatal("both operations won")
		}
		winner = result
	}
	if winner == "" {
		t.Fatal("neither operation won")
	}
	if err = s.Compact(0); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = NewMessageStore(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	receipt, err := s.Get(rid)
	if err != nil {
		t.Fatal(err)
	}
	var sub mailstate.Submission
	json.Unmarshal([]byte(receipt.Metadata["submission"]), &sub)
	if sub.UndoStatus != winner {
		t.Fatal(sub)
	}
	entry, err := s.Get("mail")
	if winner == "canceled" && err == nil || winner == "final" && (err != nil || entry.Status != "pending") {
		t.Fatal(entry, err)
	}
}
func TestWorkflowRetentionProtectsSourcesIntervalsAndLegacy(t *testing.T) {
	dir := t.TempDir()
	s, err := NewMessageStore(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	now := time.Now()
	for _, e := range []*JournalEntry{
		{MessageID: "source", Tier: "out", Status: "failed"},
		{MessageID: "active", Tier: "sieve_effect", Status: "sieve_effect", Metadata: map[string]string{"source_id": "source"}},
		{MessageID: "done", Tier: "sieve_effect", Status: "sieve_effect", Metadata: map[string]string{"source_id": "absent"}},
		{MessageID: "legacy", Tier: "sieve_effect", Status: "sieve_effect"},
		{MessageID: "future", Tier: "sieve_effect", Status: "sieve_effect", Metadata: map[string]string{"until": now.Add(time.Hour).Format(time.RFC3339Nano)}},
		{MessageID: "expired", Tier: "sieve_effect", Status: "sieve_effect", Metadata: map[string]string{"until": now.Add(-time.Hour).Format(time.RFC3339Nano)}},
	} {
		e.CreatedAt = now.Add(-48 * time.Hour)
		if _, _, err = s.Store(e); err != nil {
			t.Fatal(err)
		}
	}
	rid := scheduledFixture(t, s, "cancel", now.Add(time.Hour))
	if _, err = s.FinishScheduled(rid, "alice", true, now); err != nil {
		t.Fatal(err)
	}
	if count, err := s.ExpireWorkflowRecords(now, 0, 0); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if count, err := s.ExpireWorkflowRecords(now, time.Hour, time.Hour); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	for _, id := range []string{"active", "source", "legacy", "future"} {
		if _, err = s.Get(id); err != nil {
			t.Fatal(id, err)
		}
	}
	if err = s.Compact(0); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = NewMessageStore(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"done", "expired", rid} {
		if _, err = s.Get(id); err == nil {
			t.Fatal(id)
		}
	}
}

func TestScheduledReleasePreservesQuarantineAndJournalFailure(t *testing.T) {
	s, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	rid := scheduledFixture(t, s, "held", now.Add(-time.Second))
	s.indexMu.Lock()
	s.index["held"].Metadata["after_schedule"] = "held"
	s.indexMu.Unlock()
	if ok, err := s.FinishScheduled(rid, "", false, now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	entry, err := s.Get("held")
	if err != nil || entry.Status != "held" {
		t.Fatal(entry, err)
	}
	rid = scheduledFixture(t, s, "failure", now.Add(time.Hour))
	if err = s.journal.file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishScheduled(rid, "alice", true, now); err == nil {
		t.Fatal("acknowledged cancellation without journal durability")
	}
	entry, err = s.Get("failure")
	if err != nil || entry.Status != "scheduled" {
		t.Fatal(entry, err)
	}
}
