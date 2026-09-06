package storage

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (s *MessageStore) changeCompliance(id, actor, reason, action string, change func(*JournalEntry) error) error {
	if strings.TrimSpace(reason) == "" || len(reason) > 2048 {
		return fmt.Errorf("a reason of 1..2048 bytes is required")
	}
	if err := s.Audit(actor, "compliance_"+action+":"+reason, id); err != nil {
		return err
	}
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	e, ok := s.index[id]
	if !ok || e.Metadata["compliance"] != "true" {
		return fmt.Errorf("compliance item not found")
	}
	v := cloneEntry(e)
	if err := change(v); err != nil {
		return err
	}
	if err := s.journal.Write(v); err != nil {
		return err
	}
	if v.Status == "deleted" {
		delete(s.index, id)
	} else {
		s.index[id] = v
	}
	return nil
}
func (s *MessageStore) ClaimComplianceRelease(id, actor, reason string) (bool, error) {
	err := s.changeCompliance(id, actor, reason, "release_requested", func(e *JournalEntry) error {
		if e.Status != "compliance" || e.Metadata["legal_hold"] == "true" || e.Metadata["mode"] == "copy" {
			return fmt.Errorf("release prohibited")
		}
		e.Status = "compliance_releasing"
		return nil
	})
	return err == nil, err
}
func (s *MessageStore) FinishComplianceRelease(id string) error {
	return s.changeCompliance(id, "system", "delivery transaction persisted", "release_completed", func(e *JournalEntry) error {
		if e.Status != "compliance_releasing" {
			return fmt.Errorf("release not claimed")
		}
		e.Status = "compliance_released"
		return nil
	})
}
func (s *MessageStore) SetComplianceHold(id, actor, reason string, hold bool) error {
	return s.changeCompliance(id, actor, reason, "legal_hold", func(e *JournalEntry) error {
		if e.Status == "compliance_releasing" {
			return fmt.Errorf("release in progress")
		}
		e.Metadata["legal_hold"] = strconv.FormatBool(hold)
		return nil
	})
}
func (s *MessageStore) DeleteCompliance(id, actor, reason string) error {
	return s.changeCompliance(id, actor, reason, "delete_requested", func(e *JournalEntry) error {
		if e.Metadata["legal_hold"] == "true" || e.Status == "compliance_releasing" {
			return fmt.Errorf("item protected")
		}
		until, err := time.Parse(time.RFC3339Nano, e.Metadata["retain_until"])
		if err != nil || time.Now().Before(until) {
			return fmt.Errorf("retention has not expired (empty retention means indefinite)")
		}
		e.Status = "deleted"
		return nil
	})
}
func (s *MessageStore) ListCompliance(domain string) []*JournalEntry {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	out := []*JournalEntry{}
	for _, e := range s.index {
		if e.Metadata["compliance"] == "true" && (domain == "" || complianceDomainMatches(e.Metadata["domain"], domain)) {
			v := *e
			v.Data = nil
			v.To = append([]string(nil), e.To...)
			v.Metadata = map[string]string{}
			for key, value := range e.Metadata {
				if key != "message" {
					v.Metadata[key] = value
				}
			}
			out = append(out, &v)
		}
	}
	return out
}

func complianceDomainMatches(domains, domain string) bool {
	for _, d := range strings.Split(domains, ",") {
		if strings.EqualFold(d, domain) {
			return true
		}
	}
	return false
}
