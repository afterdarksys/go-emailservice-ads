package smtpd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/compliance"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"github.com/google/uuid"
)

func (qm *QueueManager) applyCompliance(msg *Message) (bool, error) {
	if msg.complianceBypass {
		return false, nil
	}
	rules := qm.platform.Compliance.Evaluate(msg.From, msg.To)
	held := false
	var combined *compliance.Rule
	domains := []string{}
	for _, r := range rules {
		if r.Mode == "hold" || r.Mode == "reroute" {
			held = true
			domains = append(domains, strings.ToLower(r.Domain))
			if combined == nil {
				copyRule := r
				combined = &copyRule
			} else {
				combined.Name += "," + r.Name
				combined.LegalHold = combined.LegalHold || r.LegalHold
				if combined.Retention == 0 || r.Retention == 0 {
					combined.Retention = 0
				} else if r.Retention > combined.Retention {
					combined.Retention = r.Retention
				}
			}
		}
	}
	if combined != nil {
		combined.Domain = strings.Join(domains, ",")
		if err := qm.captureCompliance(msg, *combined); err != nil {
			return false, err
		}
	}
	for _, r := range rules {
		if r.Mode == "hold" || r.Mode == "reroute" {
			continue
		}
		if r.Mode == "bcc" {
			// A domain hold takes precedence over all monitoring delivery.
			if !held {
				found := false
				for _, to := range msg.To {
					found = found || strings.EqualFold(to, r.BCC)
				}
				if !found {
					msg.To = append(msg.To, r.BCC)
				}
			}
			if err := qm.store.Audit("compliance:"+r.Name, "bcc_policy_applied", msg.ID); err != nil {
				return false, err
			}
			continue
		}
		if err := qm.captureCompliance(msg, r); err != nil {
			return false, err
		}
	}
	return held, nil
}
func (qm *QueueManager) captureCompliance(msg *Message, r compliance.Rule) error {
	saved := *msg
	saved.Data = nil
	raw, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	metadata := map[string]string{"compliance": "true", "rule": r.Name, "domain": strings.ToLower(r.Domain), "mode": r.Mode, "legal_hold": strconv.FormatBool(r.LegalHold), "source_id": msg.ID, "message": string(raw)}
	digest := storage.EvidenceHash(msg.From, msg.To, msg.Data)
	metadata["evidence_sha256"] = digest
	if r.Retention > 0 {
		metadata["retain_until"] = time.Now().Add(r.Retention).UTC().Format(time.RFC3339Nano)
	}
	// No retention value means indefinite preservation, never immediate expiry.
	id := uuid.NewString()
	if err := qm.store.Audit("compliance:"+r.Name, "capture_requested:"+digest, id); err != nil {
		return err
	}
	_, _, err = qm.store.Store(&storage.JournalEntry{MessageID: id, From: msg.From, To: append([]string(nil), msg.To...), Data: msg.Data, Tier: "compliance", Status: "compliance", Metadata: metadata})
	return err
}

// ReleaseCompliance retains the evidence record and creates a new delivery
// transaction. Storage serializes releases; a stable release ID prevents
// concurrent duplicate release while the transaction remains in the spool.
func (qm *QueueManager) ReleaseCompliance(id, actor, reason string) error {
	e, err := qm.store.Get(id)
	if err != nil {
		return err
	}
	if e.Metadata["compliance"] != "true" || e.Status != "compliance" {
		return fmt.Errorf("not an active compliance item")
	}
	if err := storage.VerifyEvidence(e); err != nil {
		return err
	}
	if e.Metadata["legal_hold"] == "true" {
		return fmt.Errorf("clear legal hold before release")
	}
	if e.Metadata["mode"] == "copy" {
		return fmt.Errorf("monitoring copies cannot be delivered")
	}
	if ok, err := qm.store.ClaimComplianceRelease(id, actor, reason); err != nil || !ok {
		return fmt.Errorf("unable to claim compliance release: %v", err)
	}
	msg := &Message{}
	if err := json.Unmarshal([]byte(e.Metadata["message"]), msg); err != nil {
		return err
	}
	msg.Data = e.Data
	msg.From = e.From
	msg.To = e.To
	msg.ID = "release-" + id
	msg.ComplianceReleaseID = id
	msg.complianceBypass = true
	if err := qm.Enqueue(msg); err != nil {
		return fmt.Errorf("release submission failed; item retained in compliance_releasing for reconciliation: %w", err)
	}
	return qm.store.FinishComplianceRelease(id)
}
