package storage

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

// FinishScheduled serializes dispatch and cancellation with one durable batch.
// A successful cancellation removes the queue payload before returning.
func (s *MessageStore) FinishScheduled(receiptID, user string, cancel bool, now time.Time) (bool, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	receipt := s.index[receiptID]
	if receipt == nil || receipt.Tier != "jmap_submission" || (user != "" && receipt.Metadata["username"] != user) {
		return false, mailstate.ErrBlobNotFound
	}
	var submission mailstate.Submission
	if err := json.Unmarshal([]byte(receipt.Metadata["submission"]), &submission); err != nil {
		return false, err
	}
	if cancel && submission.UndoStatus == "canceled" {
		return true, nil
	}
	if submission.UndoStatus != "pending" {
		return false, mailstate.ErrCannotUnsend
	}
	entry := s.index[receipt.Metadata["queue_id"]]
	if entry == nil || entry.Status != "scheduled" || entry.Metadata["receipt_id"] != receiptID || entry.Metadata["compliance"] == "true" {
		return false, mailstate.ErrCannotUnsend
	}
	due, err := time.Parse(time.RFC3339, submission.SendAt)
	if err != nil {
		return false, err
	}
	if !cancel && now.Before(due) {
		return false, nil
	}
	q, r := cloneEntry(entry), cloneEntry(receipt)
	q.Status = "pending"
	if entry.Metadata["after_schedule"] == "held" {
		q.Status = "held"
	}
	submission.UndoStatus = "final"
	if cancel {
		q.Status = "deleted"
		submission.UndoStatus = "canceled"
	}
	encoded, err := json.Marshal(submission)
	if err != nil {
		return false, err
	}
	r.Metadata["submission"] = string(encoded)
	if s.closed {
		return false, fmt.Errorf("store closed")
	}
	if err = s.journal.WriteBatch(q, r); err != nil {
		return false, err
	}
	s.index[r.MessageID] = r
	if cancel {
		delete(s.index, q.MessageID)
	} else {
		s.index[q.MessageID] = q
	}
	return true, nil
}

// ExpireWorkflowRecords never removes active-send receipts, live-source retry
// checkpoints, unexpired vacation intervals or legacy records without provenance.
func (s *MessageStore) ExpireWorkflowRecords(now time.Time, receipts, sieve time.Duration) (int, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	if s.closed {
		return 0, fmt.Errorf("store closed")
	}
	expired := []*JournalEntry{}
	for _, entry := range s.index {
		age := now.Sub(entry.CreatedAt)
		if entry.CreatedAt.IsZero() {
			continue
		}
		eligible := false
		switch entry.Tier {
		case "jmap_submission":
			if receipts <= 0 || age < receipts {
				continue
			}
			var sub mailstate.Submission
			if json.Unmarshal([]byte(entry.Metadata["submission"]), &sub) != nil || sub.UndoStatus != "final" && sub.UndoStatus != "canceled" {
				continue
			}
			if source := entry.Metadata["queue_id"]; source != "" && s.index[source] != nil {
				continue
			}
			eligible = true
		case "sieve_effect":
			if sieve <= 0 || age < sieve {
				continue
			}
			if until := entry.Metadata["until"]; until != "" {
				expiry, err := time.Parse(time.RFC3339Nano, until)
				eligible = err == nil && !now.Before(expiry)
			} else if source := entry.Metadata["source_id"]; source != "" {
				eligible = s.index[source] == nil
			}
		}
		if eligible {
			v := cloneEntry(entry)
			v.Status = "deleted"
			expired = append(expired, v)
		}
	}
	if len(expired) == 0 {
		return 0, nil
	}
	if err := s.journal.WriteBatch(expired...); err != nil {
		return 0, err
	}
	for _, entry := range expired {
		delete(s.index, entry.MessageID)
	}
	return len(expired), nil
}
