package smtpd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/bounce"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"github.com/google/uuid"
)

func (m *Message) wantsDSN(recipient, event string) bool {
	options := m.DSNRecipients[recipient]
	if len(options.Notify) == 0 {
		return event == "FAILURE" || event == "DELAY"
	}
	for _, n := range options.Notify {
		if string(n) == event {
			return true
		}
	}
	return false
}
func (qm *QueueManager) sendDSN(msg *Message, recipient, event, action string) error {
	if qm.platform.Bounce.Suppress || msg.IsBounce || msg.From == "" || msg.From == "<>" || !msg.wantsDSN(recipient, event) {
		return nil
	}
	code, status := 250, "2.0.0"
	if event == "DELAY" {
		code, status = 451, "4.4.1"
	}
	raw, err := qm.bounceGenerator.GenerateBounce(msg.From, &bounce.BounceReason{SMTPCode: code, EnhancedCode: status, Message: "Delivery " + action, Recipient: recipient, Action: action, EnvelopeID: msg.DSNMail.EnvelopeID, OriginalRecipient: msg.DSNRecipients[recipient].OriginalRecipient}, msg.Data)
	if err != nil {
		return err
	}
	notice := &Message{ID: uuid.NewString(), From: "", To: []string{msg.From}, Data: raw, Tier: TierEmergency, IsBounce: true, CreatedAt: time.Now()}
	saved := *notice
	saved.Data = nil
	metadata, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	child := &storage.JournalEntry{MessageID: notice.ID, From: "", To: notice.To, Data: raw, Tier: "emergency", Status: "pending", CreatedAt: notice.CreatedAt, Metadata: map[string]string{"message": string(metadata)}}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(event+"\x00"+recipient)))
	fresh, err := qm.store.StoreNotification(msg.ID, key, child)
	if err != nil || !fresh {
		return err
	}
	if ok, err := qm.store.Transition(notice.ID, "pending", "queued"); err != nil || !ok {
		return nil
	}
	return qm.enqueueToChannel(TierEmergency, notice)
}
func (qm *QueueManager) RecipientSuppressed(address string) bool {
	for _, recipient := range qm.platform.Bounce.SuppressedRecipients {
		if strings.EqualFold(recipient, address) {
			return true
		}
	}
	return false
}
