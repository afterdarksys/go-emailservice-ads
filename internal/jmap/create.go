package jmap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

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
		case "mailboxIds", "keywords", "receivedAt", "from", "to", "cc", "bcc", "replyTo", "subject", "sentAt", "messageId", "inReplyTo", "references", "textBody", "htmlBody", "bodyValues", "attachments":
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
	values := map[string]interface{}{}
	if v, exists := obj["bodyValues"]; exists {
		var ok bool
		values, ok = v.(map[string]interface{})
		if !ok {
			return out, "invalidProperties", nil
		}
	}
	var data bytes.Buffer
	writer, err := mm.CreateWriter(&data, header)
	if err != nil {
		return out, "invalidProperties", nil
	}
	inline, err := writer.CreateInline()
	if err != nil {
		return out, "serverFail", nil
	}
	used := map[string]bool{}
	parts := 0
	for _, key := range []string{"textBody", "htmlBody"} {
		media := map[string]string{"textBody": "text/plain", "htmlBody": "text/html"}[key]
		if v, exists := obj[key]; exists {
			list, ok := v.([]interface{})
			if !ok || len(list) > 1 {
				return out, "invalidProperties", nil
			}
			for _, item := range list {
				part, ok := item.(map[string]interface{})
				if !ok {
					return out, "invalidProperties", nil
				}
				for k := range part {
					if k != "partId" && k != "type" && k != "charset" {
						return out, "invalidProperties", nil
					}
				}
				id, ok := part["partId"].(string)
				if !ok || id == "" || used[id] {
					return out, "invalidProperties", nil
				}
				if v := part["type"]; v != nil && v != media {
					return out, "invalidProperties", nil
				}
				if v := part["charset"]; v != nil && v != "utf-8" {
					return out, "invalidProperties", nil
				}
				body, ok := values[id].(map[string]interface{})
				if !ok {
					return out, "invalidProperties", nil
				}
				for k, v := range body {
					if k != "value" && ((k != "isEncodingProblem" && k != "isTruncated") || v != false) {
						return out, "invalidProperties", nil
					}
				}
				text, ok := body["value"].(string)
				if !ok || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
					return out, "invalidProperties", nil
				}
				used[id] = true
				parts++
				var h mm.InlineHeader
				h.SetContentType(media, map[string]string{"charset": "utf-8"})
				w, e := inline.CreatePart(h)
				if e != nil {
					return out, "serverFail", nil
				}
				if _, e = io.WriteString(w, text); e != nil {
					return out, "serverFail", nil
				}
				if e = w.Close(); e != nil {
					return out, "serverFail", nil
				}
			}
		}
	}
	if len(used) != len(values) {
		return out, "invalidProperties", nil
	}
	if parts == 0 {
		var h mm.InlineHeader
		h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		w, e := inline.CreatePart(h)
		if e != nil {
			return out, "serverFail", nil
		}
		if e = w.Close(); e != nil {
			return out, "serverFail", nil
		}
	}
	if err = inline.Close(); err != nil {
		return out, "serverFail", nil
	}
	if v, exists := obj["attachments"]; exists {
		list, ok := v.([]interface{})
		if !ok {
			return out, "invalidProperties", nil
		}
		store, ok := j.store.(mailstate.ImportStore)
		if !ok {
			return out, "forbidden", nil
		}
		for _, item := range list {
			part, ok := item.(map[string]interface{})
			if !ok {
				return out, "invalidProperties", nil
			}
			for key := range part {
				if key != "blobId" && key != "type" && key != "name" && key != "disposition" {
					return out, "invalidProperties", nil
				}
			}
			id, ok := part["blobId"].(string)
			if !ok || id == "" {
				return out, "invalidProperties", nil
			}
			blob, e := store.GetBlob(ctx, user, id)
			if errors.Is(e, mailstate.ErrBlobNotFound) {
				return out, "blobNotFound", []string{id}
			}
			if e != nil {
				return out, "serverFail", nil
			}
			media := "application/octet-stream"
			if v := part["type"]; v != nil {
				var ok bool
				media, ok = v.(string)
				if !ok || !safeHeader(media) || strings.HasPrefix(strings.ToLower(media), "multipart/") {
					return out, "invalidProperties", nil
				}
			}
			name := "attachment"
			if v := part["name"]; v != nil {
				var ok bool
				name, ok = v.(string)
				if !ok || !safeHeader(name) {
					return out, "invalidProperties", nil
				}
			}
			if v := part["disposition"]; v != nil && v != "attachment" {
				return out, "invalidProperties", nil
			}
			if data.Len()+len(blob.Data)*4/3 > mailstate.MaxUploadBytes {
				return out, "tooLarge", nil
			}
			var h mm.AttachmentHeader
			kind, params, e := mime.ParseMediaType(media)
			if e != nil {
				return out, "invalidProperties", nil
			}
			h.SetContentType(kind, params)
			h.SetFilename(name)
			w, e := writer.CreateAttachment(h)
			if e != nil {
				return out, "invalidProperties", nil
			}
			if _, e = w.Write(blob.Data); e != nil {
				return out, "serverFail", nil
			}
			if e = w.Close(); e != nil {
				return out, "serverFail", nil
			}
		}
	}
	if err = writer.Close(); err != nil {
		return out, "serverFail", nil
	}
	if data.Len() > mailstate.MaxUploadBytes {
		return out, "tooLarge", nil
	}
	out.Data = data.Bytes()
	return out, "", nil
}
