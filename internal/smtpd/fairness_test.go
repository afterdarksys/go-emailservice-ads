package smtpd

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"testing"
	"time"
)

func TestRetriesInterleaveDestinations(t *testing.T) {
	var entries []*storage.JournalEntry
	for i, domain := range []string{"slow.test", "slow.test", "slow.test", "other.test"} {
		entries = append(entries, &storage.JournalEntry{To: []string{"x@" + domain}, CreatedAt: time.Unix(int64(i), 0)})
	}
	got := fairPending(entries)
	if got[1].To[0] != "x@other.test" {
		t.Fatal("unrelated destination starved")
	}
}
