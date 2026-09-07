package smtpd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"github.com/google/uuid"
)

// Submit accepts an already authenticated mailbox identity, applying the same
// envelope, policy, rate, scanner and durable queue path as SMTP submission.
func (server *Server) Submit(ctx context.Context, user, ip, emailID, from string, to []string, data []byte) (mailstate.Submission, error) {
	out := mailstate.Submission{}
	b := server.backend
	if b == nil || b.qManager == nil {
		return out, fmt.Errorf("submission unavailable")
	}
	account, ok := b.validator.GetUserStore().GetUser(user)
	if !ok || !account.Enabled {
		return out, fmt.Errorf("account disabled")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if len(data) == 0 || (b.config.Server.MaxMessageBytes > 0 && len(data) > b.config.Server.MaxMessageBytes) {
		return out, fmt.Errorf("message size exceeded")
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return out, fmt.Errorf("invalid message")
	}
	froms, err := parsed.Header.AddressList("From")
	if err != nil || len(froms) != 1 || !strings.EqualFold(froms[0].Address, from) || !b.validator.AuthorizedToSendAs(user, from) {
		return out, fmt.Errorf("unauthorized From")
	}
	if len(to) == 0 {
		return out, fmt.Errorf("no recipients")
	}
	session := &Session{logger: b.logger, qManager: b.qManager, validator: b.validator, dirClient: b.dirClient, policyEngine: b.policyEngine, dkimVerifier: b.dkimVerifier, policyManager: b.policyManager, spreadPrev: b.spreadPrev, ip: ip, ehlo: b.config.Server.Domain, authenticated: true, username: user, config: b.config, messageRates: b.messageRates}
	if err = session.Mail(from, nil); err != nil {
		return out, err
	}
	recipients := []interface{}{}
	for _, address := range to {
		p, e := mail.ParseAddress(address)
		if e != nil || p.Address != address {
			return out, fmt.Errorf("invalid recipient")
		}
		if e = session.Rcpt(address, nil); e != nil {
			return out, e
		}
		recipients = append(recipients, map[string]interface{}{"email": address})
	}
	out = mailstate.Submission{ID: uuid.NewString(), EmailID: emailID, IdentityID: "primary", ThreadID: emailID, SendAt: time.Now().UTC().Format(time.RFC3339), UndoStatus: "final", Envelope: map[string]interface{}{"mailFrom": map[string]interface{}{"email": from}, "rcptTo": recipients}}
	encoded, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	session.msg.SubmissionReceipt = &storage.JournalEntry{MessageID: "jmap-submission-" + out.ID, Tier: "jmap_submission", Status: "jmap_submission", CreatedAt: time.Now(), Metadata: map[string]string{"username": user, "submission": string(encoded)}}
	// Bcc belongs to the private envelope, never the delivered message headers.
	if err = session.Data(bytes.NewReader(removeHeader(data, "Bcc"))); err != nil {
		return mailstate.Submission{}, err
	}
	return out, nil
}
func (server *Server) Submissions(ctx context.Context, user string) ([]mailstate.Submission, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if server.backend == nil || server.backend.qManager == nil {
		return nil, fmt.Errorf("submission unavailable")
	}
	out := []mailstate.Submission{}
	for _, entry := range server.backend.qManager.store.ListByStatus("jmap_submission", "jmap_submission") {
		if entry.Metadata["username"] != user {
			continue
		}
		var s mailstate.Submission
		if err := json.Unmarshal([]byte(entry.Metadata["submission"]), &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Policy discard and compliance hold accept without an ordinary delivery queue
// record. Their receipt must still be durable before acknowledging acceptance.
func (qm *QueueManager) persistSubmissionReceipt(msg *Message) error {
	if msg.SubmissionReceipt == nil {
		return nil
	}
	_, _, err := qm.store.StoreWithReceipt(msg.SubmissionReceipt, msg.ExtraReceipts...)
	return err
}

// Deleting a receipt never cancels or changes the independently queued message.
func (server *Server) DestroySubmission(ctx context.Context, user, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store := server.backend.qManager.store
	entry, err := store.Get("jmap-submission-" + id)
	if err != nil || entry.Tier != "jmap_submission" || entry.Metadata["username"] != user {
		return mailstate.ErrBlobNotFound
	}
	return store.UpdateStatus(entry.MessageID, "delivered", "")
}
