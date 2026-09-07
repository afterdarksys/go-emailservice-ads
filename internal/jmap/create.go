package jmap

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	mm "github.com/emersion/go-message/mail"
	"github.com/google/uuid"
)

func safeHeader(v string) bool { return !strings.ContainsAny(v, "\r\n\x00") }
func creationAddresses(v interface{}) ([]*mail.Address, bool) {
	list, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	out := []*mail.Address{}
	for _, item := range list {
		obj, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		for key := range obj {
			if key != "name" && key != "email" {
				return nil, false
			}
		}
		email, ok := obj["email"].(string)
		if !ok || !safeHeader(email) {
			return nil, false
		}
		parsed, err := mail.ParseAddress(email)
		if err != nil || parsed.Address != email {
			return nil, false
		}
		name := ""
		if v := obj["name"]; v != nil {
			var ok bool
			name, ok = v.(string)
			if !ok || !safeHeader(name) {
				return nil, false
			}
		}
		out = append(out, &mail.Address{Name: name, Address: email})
	}
	return out, true
}
func (j *JMAPServer) buildEmail(ctx context.Context, user string, value interface{}) (mailstate.EmailCreation, string, []string) {
	out := mailstate.EmailCreation{}
	obj, ok := value.(map[string]interface{})
	if !ok {
		return out, "invalidProperties", nil
	}
	for key := range obj {
		switch key {
		case "mailboxIds", "keywords", "receivedAt", "from", "to", "cc", "bcc", "replyTo", "subject", "sentAt", "messageId", "inReplyTo", "references", "textBody", "htmlBody", "bodyValues", "attachments", "bodyStructure":
		default:
			return out, "invalidProperties", nil
		}
	}
	metadata := map[string]interface{}{"blobId": "placeholder", "mailboxIds": obj["mailboxIds"]}
	for _, key := range []string{"keywords", "receivedAt"} {
		if v, ok := obj[key]; ok {
			metadata[key] = v
		}
	}
	p, kind := importObject(metadata)
	if kind != "" {
		return out, kind, nil
	}
	out.MailboxID, out.Flags, out.ReceivedAt = p.MailboxID, p.Flags, p.ReceivedAt
	var header mm.Header
	for key, name := range map[string]string{"from": "From", "to": "To", "cc": "Cc", "bcc": "Bcc", "replyTo": "Reply-To"} {
		if v, exists := obj[key]; exists {
			addresses, ok := creationAddresses(v)
			if !ok {
				return out, "invalidProperties", nil
			}
			header.SetAddressList(name, addresses)
		}
	}
	if v, exists := obj["subject"]; exists {
		s, ok := v.(string)
		if !ok || !safeHeader(s) {
			return out, "invalidProperties", nil
		}
		header.SetSubject(s)
	}
	date := time.Now()
	if v, exists := obj["sentAt"]; exists {
		s, ok := v.(string)
		if !ok || !strings.HasSuffix(s, "Z") {
			return out, "invalidProperties", nil
		}
		var err error
		date, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return out, "invalidProperties", nil
		}
	}
	header.SetDate(date)
	header.SetMessageID(uuid.NewString() + "@jmap.local")
	for key, name := range map[string]string{"messageId": "Message-ID", "inReplyTo": "In-Reply-To", "references": "References"} {
		if v, exists := obj[key]; exists {
			ids := toStringSlice(v)
			if ids == nil || len(ids) == 0 || (key == "messageId" && len(ids) != 1) {
				return out, "invalidProperties", nil
			}
			quoted := []string{}
			for _, id := range ids {
				if id == "" || strings.ContainsAny(id, "<> \t\r\n\x00") {
					return out, "invalidProperties", nil
				}
				quoted = append(quoted, "<"+id+">")
			}
			header.Set(name, strings.Join(quoted, " "))
		}
	}
	data, kind, missing := j.composeMIME(ctx, user, obj, header)
	out.Data = data
	return out, kind, missing
}
