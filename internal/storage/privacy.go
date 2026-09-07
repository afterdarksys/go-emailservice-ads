package storage

import (
	"sort"
	"strings"
)

type PersonalRecord struct {
	Entry     *JournalEntry `json:"entry"`
	Owned     bool          `json:"owned"`
	Protected bool          `json:"protected"`
}

// PersonalRecords inventories account-owned and shared envelope-related data.
// Shared copies remain subject to the other mailbox owner's retention rights.
func (s *MessageStore) PersonalRecords(user, email string) []PersonalRecord {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	out := []PersonalRecord{}
	linked := map[string]bool{}
	for _, e := range s.index {
		if e.Metadata["username"] == user && e.Metadata["queue_id"] != "" {
			linked[e.Metadata["queue_id"]] = true
		}
	}
	for _, e := range s.index {
		owned := e.Metadata["username"] == user || linked[e.MessageID]
		related := owned || strings.EqualFold(e.From, email)
		exclusive := len(e.To) > 0
		for _, to := range e.To {
			if strings.EqualFold(to, email) {
				related = true
			} else {
				exclusive = false
			}
		}
		if e.Tier != "mailbox" && e.Tier != "sieve_effect" && (exclusive || strings.EqualFold(e.From, email)) {
			owned = true
		}
		if !related {
			continue
		}
		out = append(out, PersonalRecord{Entry: cloneEntry(e), Owned: owned, Protected: e.Metadata["compliance"] == "true"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entry.MessageID < out[j].Entry.MessageID })
	return out
}
