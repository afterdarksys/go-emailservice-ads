package smtpd

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/delivery"
	"github.com/afterdarksys/go-emailservice-ads/internal/elasticsearch"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	mm "github.com/emersion/go-message/mail"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func sieveKey(parts ...string) string {
	b, _ := json.Marshal(parts)
	return fmt.Sprintf("sieve-%x", sha256.Sum256(b))
}
func sieveRecord(id string, metadata map[string]string) *storage.JournalEntry {
	return &storage.JournalEntry{MessageID: id, Tier: "sieve_effect", Status: "sieve_effect", CreatedAt: time.Now(), Metadata: metadata}
}

// Freeze the evaluated plan before its first side effect so a retry after a
// script edit resumes the accepted plan rather than delivering a second route.
func (qm *QueueManager) sievePlan(msg *Message, user, script string, ctx *policy.EmailContext) (*policy.Action, error) {
	qm.sieveMu.Lock()
	defer qm.sieveMu.Unlock()
	key := sieveKey("plan", msg.ID, user)
	if entry, err := qm.store.Get(key); err == nil {
		var action policy.Action
		err = json.Unmarshal([]byte(entry.Metadata["plan"]), &action)
		return &action, err
	}
	action, err := qm.policyManager.EvaluateSieve(qm.ctx, script, ctx)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(action)
	if err != nil {
		return nil, err
	}
	_, _, err = qm.store.Store(sieveRecord(key, map[string]string{"username": user, "plan": string(data), "source_id": msg.ID}))
	return action, err
}
func (qm *QueueManager) executeSieve(msg *Message, user, rcpt string, action *policy.Action) error {
	plan := action.Actions
	if len(plan) == 0 {
		plan = []*policy.Action{action}
	}
	// Combine deliveries to the same folder before writing any mailbox copy.
	folders := map[string][]string{}
	order := []string{}
	for _, a := range plan {
		switch a.Type {
		case policy.ActionKeep, policy.ActionFileinto:
			folder := a.Target
			if a.Type == policy.ActionKeep {
				folder = msg.DeliveryFolder
			}
			if folder == "" {
				folder = msg.DeliveryFolder
			}
			if folder == "" {
				folder = "INBOX"
			}
			if msg.Quarantine && folder == "INBOX" {
				folder = msg.QuarantineFolder
				if folder == "" {
					folder = "Junk"
				}
			}
			if _, ok := folders[folder]; !ok {
				order = append(order, folder)
			}
			seen := map[string]bool{}
			for _, f := range folders[folder] {
				seen[strings.ToLower(f)] = true
			}
			for _, f := range a.Tags {
				if !seen[strings.ToLower(f)] {
					folders[folder] = append(folders[folder], f)
					seen[strings.ToLower(f)] = true
				}
			}
			if folders[folder] == nil {
				folders[folder] = []string{}
			}
		case policy.ActionRedirect:
			parsed, err := mail.ReadMessage(bytes.NewReader(msg.Data))
			if err != nil {
				return err
			}
			if len(parsed.Header["Received"]) >= 10 {
				return fmt.Errorf("Sieve redirect hop limit reached")
			}
			data := removeHeader(removeHeader(msg.Data, "Return-Path"), "Bcc")
			data = append([]byte("Received: by "+qm.hostname+" with Sieve; "+time.Now().Format(time.RFC1123Z)+"\r\n"), data...)
			child := &Message{From: msg.From, To: []string{a.Target}, Data: data, Tier: TierOut, ClientIP: msg.ClientIP, ParentTraceID: msg.TraceID, Quarantine: msg.Quarantine, QuarantineFolder: msg.QuarantineFolder}
			if err = qm.enqueueSieveOnce(child, sieveKey("redirect", msg.ID, user, strings.ToLower(a.Target)), "", time.Time{}, msg.ID, user); err != nil {
				return err
			}
		case policy.ActionVacation:
			if err := qm.sieveVacation(msg, user, rcpt, a.Vacation); err != nil {
				return err
			}
		case policy.ActionReject:
			return qm.generateBounce(msg, &delivery.DeliveryResult{SMTPCode: 550, IsPermanent: true, Message: a.Reason}, []string{rcpt})
		case policy.ActionDiscard:
		default:
			return fmt.Errorf("unsupported Sieve delivery action %s", a.Type)
		}
	}
	for index, folder := range order {
		key := sieveKey("mailbox", msg.ID, user, folder)
		// Preserve the pre-upgrade single-delivery checkpoint for the first copy.
		if index == 0 {
			key = msg.ID + "/" + rcpt
		}
		if _, err := qm.imapStore.DeliverOnceWithFlags(qm.ctx, key, user, folder, msg.Data, folders[folder]); err != nil {
			return err
		}
		qm.logger.Info("Sieve local delivery succeeded", zap.String("msg_id", msg.ID), zap.String("recipient", rcpt), zap.String("folder", folder))
	}
	if len(order) > 0 {
		qm.publishEvent(elasticsearch.EventDelivered, msg, nil)
		return qm.sendDSN(msg, rcpt, "SUCCESS", "delivered")
	}
	return nil
}

// Queue data and both the per-message and optional vacation interval checkpoint
// share one journal record. A delivered child cannot be sent again on replay.
func (qm *QueueManager) enqueueSieveOnce(child *Message, key, interval string, until time.Time, source ...string) error {
	qm.sieveMu.Lock()
	defer qm.sieveMu.Unlock()
	if _, err := qm.store.Get(key); err == nil {
		return nil
	}
	metadata := map[string]string{}
	if len(source) > 0 {
		metadata["source_id"] = source[0]
	}
	if len(source) > 1 {
		metadata["username"] = source[1]
	}
	marker := sieveRecord(key, metadata)
	if interval != "" {
		if old, err := qm.store.Get(interval); err == nil {
			expires, e := time.Parse(time.RFC3339Nano, old.Metadata["until"])
			if e != nil {
				return e
			}
			if time.Now().Before(expires) {
				_, _, err = qm.store.Store(marker)
				return err
			}
			if err = qm.store.UpdateStatus(interval, "delivered", ""); err != nil {
				return err
			}
		}
		child.ExtraReceipts = []*storage.JournalEntry{sieveRecord(interval, map[string]string{"until": until.Format(time.RFC3339Nano), "username": metadata["username"]})}
	}
	child.SubmissionReceipt = marker
	return qm.Enqueue(child)
}

func (qm *QueueManager) sieveVacation(msg *Message, user, rcpt string, v *policy.Vacation) error {
	if v == nil {
		return fmt.Errorf("missing vacation settings")
	}
	if msg.Quarantine || msg.From == "" || msg.From == "<>" || msg.IsBounce {
		return nil
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(msg.Data))
	if err != nil {
		return err
	}
	if auto := parsed.Header.Get("Auto-Submitted"); auto != "" && !strings.EqualFold(strings.TrimSpace(auto), "no") {
		return nil
	}
	for key := range parsed.Header {
		if strings.HasPrefix(strings.ToLower(key), "list-") {
			return nil
		}
	}
	switch strings.ToLower(parsed.Header.Get("Precedence")) {
	case "bulk", "list", "junk":
		return nil
	}
	from, err := mail.ParseAddress(msg.From)
	if err != nil {
		return nil
	}
	local := strings.ToLower(strings.SplitN(from.Address, "@", 2)[0])
	if strings.Contains(local, "mailer-daemon") || local == "postmaster" || strings.HasSuffix(local, "-request") {
		return nil
	}
	identity := rcpt
	if qm.users != nil {
		account, ok := qm.users.GetUser(user)
		if !ok || !account.Enabled {
			return fmt.Errorf("vacation account unavailable")
		}
		identity = account.Email
	}
	if v.From != "" && !strings.EqualFold(v.From, identity) {
		return fmt.Errorf("vacation From must match account identity")
	}
	if strings.EqualFold(from.Address, identity) {
		return nil
	}
	addresses := append([]string{identity, rcpt}, v.Addresses...)
	personal := false
	for _, key := range []string{"To", "Cc"} {
		list, _ := parsed.Header.AddressList(key)
		for _, a := range list {
			for _, own := range addresses {
				personal = personal || strings.EqualFold(a.Address, own)
			}
		}
	}
	if !personal {
		return nil
	}
	var header mm.Header
	header.SetAddressList("From", []*mail.Address{{Address: identity}})
	header.SetAddressList("To", []*mail.Address{{Address: from.Address}})
	subject := v.Subject
	if subject == "" {
		subject = "Auto: away"
	}
	header.SetSubject(subject)
	header.SetDate(time.Now())
	header.SetMessageID(uuid.NewString() + "@" + qm.hostname)
	header.Set("Auto-Submitted", "auto-replied")
	if id := parsed.Header.Get("Message-ID"); id != "" {
		header.Set("In-Reply-To", id)
	}
	var data bytes.Buffer
	if v.MIME {
		body, err := mail.ReadMessage(strings.NewReader(v.Message))
		if err != nil {
			return fmt.Errorf("invalid vacation MIME: %w", err)
		}
		// MIME reason supplies only content headers, never routing/auth headers.
		for key, values := range body.Header {
			if !strings.HasPrefix(strings.ToLower(key), "content-") && !strings.EqualFold(key, "MIME-Version") {
				return fmt.Errorf("unsupported vacation MIME header")
			}
			for _, value := range values {
				header.Add(key, value)
			}
		}
		for fields := header.Fields(); fields.Next(); {
			fmt.Fprintf(&data, "%s: %s\r\n", fields.Key(), fields.Value())
		}
		data.WriteString("\r\n")
		if _, err = data.ReadFrom(body.Body); err != nil {
			return err
		}
	} else {
		header.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		w, err := mm.CreateSingleInlineWriter(&data, header)
		if err != nil {
			return err
		}
		if _, err = w.Write([]byte(v.Message)); err != nil {
			return err
		}
		if err = w.Close(); err != nil {
			return err
		}
	}
	handle := v.Handle
	if handle == "" {
		b, _ := json.Marshal([]interface{}{v.Subject, v.From, v.MIME, v.Message})
		handle = string(b)
	}
	key := sieveKey("vacation", msg.ID, user, handle)
	interval := sieveKey("vacation-interval", user, strings.ToLower(from.Address), handle)
	child := &Message{From: "", To: []string{from.Address}, Data: data.Bytes(), Tier: TierOut, IsBounce: true, ParentTraceID: msg.TraceID, Quarantine: msg.Quarantine, QuarantineFolder: msg.QuarantineFolder}
	return qm.enqueueSieveOnce(child, key, interval, time.Now().Add(time.Duration(v.Days)*24*time.Hour), msg.ID, user)
}
