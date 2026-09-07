package jmap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	messagemail "github.com/emersion/go-message/mail"
)

func methodError(kind, id string) MethodResponse {
	return MethodResponse{Name: "error", Arguments: map[string]interface{}{"type": kind}, CallID: id}
}
func folderID(folder string) string {
	switch folder {
	case "INBOX":
		return "inbox"
	case "Sent":
		return "sent"
	case "Drafts":
		return "drafts"
	case "Trash":
		return "trash"
	case "Spam", "Junk":
		return "f:" + base64.RawURLEncoding.EncodeToString([]byte(folder))
	}
	return "f:" + base64.RawURLEncoding.EncodeToString([]byte(folder))
}
func (j *JMAPServer) folders(ctx context.Context, user string) ([]string, error) {
	if store, ok := j.store.(interface {
		ListFolders(context.Context, string, bool) ([]string, error)
	}); ok {
		return store.ListFolders(ctx, user, false)
	}
	return []string{"INBOX"}, nil
}
func (j *JMAPServer) ownedMessages(ctx context.Context, user string) (map[string]MessageOwnedSummary, error) {
	owned := map[string]MessageOwnedSummary{}
	if j.store == nil || user == "" {
		return owned, fmt.Errorf("mail store unavailable")
	}
	folders, err := j.folders(ctx, user)
	if err != nil {
		return nil, err
	}
	for _, folder := range folders {
		messages, err := j.store.GetMessages(ctx, user, folder)
		if err != nil {
			return nil, err
		}
		for _, msg := range messages {
			owned[msg.ID] = MessageOwnedSummary{MessageSummary: msg, Folder: folder}
		}
	}
	return owned, nil
}
func emailState(owned map[string]MessageOwnedSummary) string {
	raw, _ := json.Marshal(owned)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (j *JMAPServer) mailboxGet(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	folders, err := j.folders(ctx, user)
	if err != nil || j.store == nil {
		return methodError("serverFail", id)
	}
	wanted := map[string]bool{}
	explicit := args["ids"] != nil
	if explicit {
		ids := toStringSlice(args["ids"])
		if ids == nil {
			return methodError("invalidArguments", id)
		}
		if len(ids) > maxJMAPObjects {
			return methodError("tooManyObjectsInGet", id)
		}
		for _, v := range ids {
			wanted[v] = true
		}
	}
	list := []map[string]interface{}{}
	notFound := []string{}
	sort.Strings(folders)
	for _, folder := range folders {
		fid := folderID(folder)
		msgs, err := j.store.GetMessages(ctx, user, folder)
		if err != nil {
			return methodError("serverFail", id)
		}
		unread := 0
		for _, msg := range msgs {
			seen := false
			for _, f := range msg.Flags {
				if strings.EqualFold(f, `\Seen`) {
					seen = true
				}
			}
			if !seen {
				unread++
			}
		}
		name := folder
		var parent any
		if pos := strings.LastIndex(folder, "/"); pos >= 0 {
			name = folder[pos+1:]
			parent = folderID(folder[:pos])
		}
		var role any
		switch folder {
		case "INBOX":
			role = "inbox"
		case "Sent":
			role = "sent"
		case "Drafts":
			role = "drafts"
		case "Trash":
			role = "trash"
		case "Junk":
			role = "junk"
		}
		list = append(list, map[string]interface{}{"id": fid, "name": name, "parentId": parent, "role": role, "sortOrder": 0, "totalEmails": len(msgs), "unreadEmails": unread, "totalThreads": len(msgs), "unreadThreads": unread, "myRights": map[string]bool{"mayReadItems": true, "mayAddItems": false, "mayRemoveItems": false, "maySetSeen": j.keywordWrites(), "maySetKeywords": j.keywordWrites(), "mayCreateChild": false, "mayRename": false, "mayDelete": false, "maySubmit": false}})
	}
	// State describes every mailbox in the account, independent of ids selection.
	raw, _ := json.Marshal(list)
	sum := sha256.Sum256(raw)
	if explicit {
		selected := []map[string]interface{}{}
		for _, mailbox := range list {
			fid := mailbox["id"].(string)
			if wanted[fid] {
				selected = append(selected, mailbox)
				delete(wanted, fid)
			}
		}
		list = selected
	}
	for v := range wanted {
		notFound = append(notFound, v)
	}
	sort.Strings(notFound)
	return MethodResponse{Name: "Mailbox/get", Arguments: map[string]interface{}{"accountId": "primary", "state": hex.EncodeToString(sum[:]), "list": list, "notFound": notFound}, CallID: id}
}
func addresses(value string) []map[string]string {
	out := []map[string]string{}
	list, _ := mail.ParseAddressList(value)
	for _, a := range list {
		out = append(out, map[string]string{"name": a.Name, "email": a.Address})
	}
	return out
}
func keywords(flags []string) map[string]bool {
	out := map[string]bool{}
	for _, f := range flags {
		switch strings.ToLower(f) {
		case `\seen`:
			out["$seen"] = true
		case `\flagged`:
			out["$flagged"] = true
		case `\answered`:
			out["$answered"] = true
		case `\draft`:
			out["$draft"] = true
		case `\deleted`, `\recent`:
		default:
			out[f] = true
		}
	}
	return out
}
func emailObject(id string, raw []byte, meta MessageOwnedSummary) map[string]interface{} {
	obj := map[string]interface{}{"id": id, "blobId": id, "threadId": id, "mailboxIds": map[string]bool{folderID(meta.Folder): true}, "keywords": keywords(meta.Flags), "size": len(raw), "receivedAt": meta.Date.UTC().Format(time.RFC3339), "subject": "", "from": []any{}, "to": []any{}, "cc": []any{}, "bcc": []any{}, "replyTo": []any{}, "messageId": []string{}, "textBody": []any{}, "htmlBody": []any{}, "attachments": []any{}, "hasAttachment": false, "bodyValues": map[string]interface{}{}}
	reader, err := messagemail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return obj
	}
	defer reader.Close()
	subject, _ := reader.Header.Subject()
	obj["subject"] = subject
	for _, key := range []string{"from", "to", "cc", "bcc"} {
		obj[key] = addresses(reader.Header.Get(key))
	}
	obj["replyTo"] = addresses(reader.Header.Get("Reply-To"))
	if date, err := reader.Header.Date(); err == nil {
		obj["sentAt"] = date.UTC().Format(time.RFC3339)
	}
	if messageID, err := reader.Header.MessageID(); err == nil {
		obj["messageId"] = []string{messageID}
	}
	values := map[string]interface{}{}
	texts, htmls, attachments := []any{}, []any{}, []any{}
	for n := 1; ; n++ {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		data, err := io.ReadAll(part.Body)
		if err != nil {
			break
		}
		pid := fmt.Sprint(n)
		var kind string
		var name any
		attachment := false
		switch h := part.Header.(type) {
		case *messagemail.InlineHeader:
			kind, _, _ = h.ContentType()
		case *messagemail.AttachmentHeader:
			kind, _, _ = h.ContentType()
			filename, _ := h.Filename()
			name = filename
			attachment = true
		}
		item := map[string]interface{}{"partId": pid, "blobId": id + ".part." + pid, "size": len(data), "type": kind, "name": name, "charset": "utf-8"}
		if attachment {
			attachments = append(attachments, item)
		} else {
			values[pid] = map[string]interface{}{"value": string(data), "isEncodingProblem": false, "isTruncated": false}
			if kind == "text/html" {
				htmls = append(htmls, item)
			} else {
				texts = append(texts, item)
			}
		}
	}
	obj["textBody"], obj["htmlBody"], obj["attachments"], obj["bodyValues"], obj["hasAttachment"] = texts, htmls, attachments, values, len(attachments) > 0
	return obj
}
func (j *JMAPServer) emailQuery(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	owned, state, err := j.emailSnapshot(ctx, user)
	if err != nil {
		return methodError("serverFail", id)
	}
	filter := map[string]interface{}{}
	if args["filter"] != nil {
		var ok bool
		filter, ok = args["filter"].(map[string]interface{})
		if !ok {
			return methodError("invalidArguments", id)
		}
	}
	for key, value := range filter {
		switch key {
		case "inMailbox", "subject", "from", "to", "text", "hasKeyword", "notKeyword":
			if _, ok := value.(string); !ok {
				return methodError("invalidArguments", id)
			}
		default:
			return methodError("unsupportedFilter", id)
		}
	}
	if args["anchor"] != nil || args["anchorOffset"] != nil {
		return methodError("invalidArguments", id)
	}
	position, limit := 0, maxJMAPObjects
	for key, dst := range map[string]*int{"position": &position, "limit": &limit} {
		if value, ok := args[key]; ok {
			n, ok := value.(float64)
			if !ok || n < 0 || n != float64(int(n)) {
				return methodError("invalidArguments", id)
			}
			*dst = int(n)
		}
	}
	if limit > maxJMAPObjects {
		limit = maxJMAPObjects
	}
	type entry struct {
		id   string
		meta MessageOwnedSummary
	}
	entries := []entry{}
	for mid, meta := range owned {
		raw, err := j.store.FetchMessage(ctx, mid)
		if err != nil {
			return methodError("serverFail", id)
		}
		msg, _ := mail.ReadMessage(bytes.NewReader(raw))
		match := true
		for key, v := range filter {
			want := v.(string)
			switch key {
			case "inMailbox":
				match = match && folderID(meta.Folder) == want
			case "hasKeyword":
				match = match && keywords(meta.Flags)[want]
			case "notKeyword":
				match = match && !keywords(meta.Flags)[want]
			case "text":
				object := emailObject(mid, raw, meta)
				var searchable strings.Builder
				for _, field := range []string{"subject", "from", "to", "cc", "bcc", "bodyValues"} {
					fmt.Fprintln(&searchable, object[field])
				}
				match = match && strings.Contains(strings.ToLower(searchable.String()), strings.ToLower(want))
			default:
				value := ""
				if msg != nil {
					value = msg.Header.Get(key)
					if decoded, err := new(mime.WordDecoder).DecodeHeader(value); err == nil {
						value = decoded
					}
				}
				match = match && msg != nil && strings.Contains(strings.ToLower(value), strings.ToLower(want))
			}
		}
		if match {
			entries = append(entries, entry{mid, meta})
		}
	}
	if args["sort"] != nil {
		return methodError("unsupportedSort", id)
	}
	sort.Slice(entries, func(a, b int) bool {
		if entries[a].meta.Date.Equal(entries[b].meta.Date) {
			return entries[a].id < entries[b].id
		}
		return entries[a].meta.Date.After(entries[b].meta.Date)
	})
	total := len(entries)
	if position > total {
		position = total
	}
	end := position + limit
	if end > total {
		end = total
	}
	ids := []string{}
	for _, entry := range entries[position:end] {
		ids = append(ids, entry.id)
	}
	return MethodResponse{Name: "Email/query", Arguments: map[string]interface{}{"accountId": "primary", "queryState": state, "canCalculateChanges": false, "position": position, "ids": ids, "total": total}, CallID: id}

}

func (j *JMAPServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/jmap/download/"), "/", 3)
	if len(parts) < 2 || parts[0] != "primary" {
		http.NotFound(w, r)
		return
	}
	blob := parts[1]
	id := blob
	partNumber := 0
	if pos := strings.LastIndex(blob, ".part."); pos >= 0 {
		var err error
		partNumber, err = strconv.Atoi(blob[pos+6:])
		if err != nil || partNumber < 1 {
			http.NotFound(w, r)
			return
		}
		id = blob[:pos]
	}
	owned, err := j.ownedMessages(r.Context(), authUserFromContext(r.Context()))
	if err != nil {
		http.Error(w, "Mail store unavailable", 503)
		return
	}
	if _, ok := owned[id]; !ok {
		http.NotFound(w, r)
		return
	}
	data, err := j.store.FetchMessage(r.Context(), id)
	if err != nil {
		http.Error(w, "Mail store unavailable", 503)
		return
	}
	contentType := "message/rfc822"
	if partNumber > 0 {
		reader, err := messagemail.CreateReader(bytes.NewReader(data))
		if err != nil {
			http.Error(w, "Invalid MIME message", 409)
			return
		}
		defer reader.Close()
		for n := 1; n <= partNumber; n++ {
			part, err := reader.NextPart()
			if err != nil {
				http.NotFound(w, r)
				return
			}
			if n == partNumber {
				data, err = io.ReadAll(part.Body)
				if err != nil {
					http.Error(w, "Unreadable MIME part", 409)
					return
				}
				switch h := part.Header.(type) {
				case *messagemail.InlineHeader:
					contentType, _, _ = h.ContentType()
				case *messagemail.AttachmentHeader:
					contentType, _, _ = h.ContentType()
				}
			}
		}
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(data)
}
