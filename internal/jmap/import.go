package jmap

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

func (j *JMAPServer) imports() bool { _, ok := j.store.(mailstate.ImportStore); return ok }
func (j *JMAPServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		uploadError(w, "POST required", 405)
		return
	}
	if r.URL.Path != "/jmap/upload/primary/" && r.URL.Path != "/jmap/upload/primary" {
		http.NotFound(w, r)
		return
	}
	user := authUserFromContext(r.Context())
	if user == "" {
		uploadError(w, "Authentication required", 401)
		return
	}
	store, ok := j.store.(mailstate.ImportStore)
	if !ok {
		uploadError(w, "Upload unavailable", 501)
		return
	}
	if j.requestSem != nil {
		select {
		case j.requestSem <- struct{}{}:
			defer func() { <-j.requestSem }()
		default:
			uploadError(w, "Too many requests", 429)
			return
		}
	}
	media := r.Header.Get("Content-Type")
	if media == "" {
		media = "application/octet-stream"
	} else {
		kind, params, err := mime.ParseMediaType(media)
		if err != nil {
			uploadError(w, "Invalid Content-Type", 400)
			return
		}
		media = mime.FormatMediaType(kind, params)
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mailstate.MaxUploadBytes))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			uploadError(w, "Upload exceeds maximum size", 413)
		} else {
			uploadError(w, "Unreadable upload", 400)
		}
		return
	}
	blob, err := store.UploadBlob(r.Context(), user, media, data)
	if errors.Is(err, mailstate.ErrUploadQuota) {
		uploadError(w, "Upload storage quota exceeded", 429)
		return
	}
	if err != nil {
		uploadError(w, "Upload storage unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]interface{}{"accountId": "primary", "blobId": blob.ID, "type": blob.MediaType, "size": len(data)})
}
func importObject(value interface{}) (mailstate.EmailImport, string) {
	p := mailstate.EmailImport{}
	obj, ok := value.(map[string]interface{})
	if !ok {
		return p, "invalidProperties"
	}
	for key := range obj {
		switch key {
		case "blobId", "mailboxIds", "keywords", "receivedAt":
		default:
			return p, "invalidProperties"
		}
	}
	p.BlobID, ok = obj["blobId"].(string)
	if !ok || p.BlobID == "" {
		return p, "invalidProperties"
	}
	members, ok := obj["mailboxIds"].(map[string]interface{})
	if !ok || len(members) == 0 {
		return p, "invalidProperties"
	}
	if len(members) > 1 {
		return p, "tooManyMailboxes"
	}
	for id, v := range members {
		if id == "" || v != true {
			return p, "invalidProperties"
		}
		p.MailboxID = id
	}
	if v, exists := obj["keywords"]; exists {
		patch, ok := keywordPatch(map[string]interface{}{"keywords": v})
		if !ok {
			return p, "invalidProperties"
		}
		p.Flags = *patch.Replace
	}
	if v, exists := obj["receivedAt"]; exists {
		date, ok := v.(string)
		if !ok || !strings.HasSuffix(date, "Z") {
			return p, "invalidProperties"
		}
		var err error
		p.ReceivedAt, err = time.Parse(time.RFC3339, date)
		if err != nil || p.ReceivedAt.IsZero() {
			return p, "invalidProperties"
		}
	}
	return p, ""
}
func (j *JMAPServer) emailImport(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	store, ok := j.store.(mailstate.ImportStore)
	if !ok {
		return methodError("accountReadOnly", id)
	}
	for key := range args {
		if key != "accountId" && key != "ifInState" && key != "emails" {
			return methodError("invalidArguments", id)
		}
	}
	since := ""
	if v := args["ifInState"]; v != nil {
		var ok bool
		since, ok = v.(string)
		if !ok {
			return methodError("invalidArguments", id)
		}
		if since == "" {
			return methodError("stateMismatch", id)
		}
	}
	emails, ok := args["emails"].(map[string]interface{})
	if !ok {
		return methodError("invalidArguments", id)
	}
	if len(emails) > maxJMAPObjects {
		return methodError("tooManyObjectsInSet", id)
	}
	parsed := map[string]mailstate.EmailImport{}
	nc := map[string]interface{}{}
	for key, v := range emails {
		p, kind := importObject(v)
		if kind != "" {
			nc[key] = map[string]interface{}{"type": kind}
		} else {
			parsed[key] = p
		}
	}
	r, err := store.ImportEmails(ctx, user, since, parsed)
	if errors.Is(err, mailstate.ErrStateMismatch) {
		return methodError("stateMismatch", id)
	}
	if err != nil {
		return methodError("serverFail", id)
	}
	created := map[string]interface{}{}
	for key, e := range r.Created {
		created[key] = map[string]interface{}{"id": e.ID, "blobId": e.ID, "threadId": e.ID, "size": e.Size}
	}
	for key, kind := range r.NotCreated {
		nc[key] = map[string]interface{}{"type": kind}
	}
	return MethodResponse{Name: "Email/import", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "oldState": r.OldState, "newState": r.NewState, "created": created, "notCreated": nc}}
}

func uploadError(w http.ResponseWriter, detail string, status int) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{"type": "about:blank", "status": status, "title": http.StatusText(status), "detail": detail})
}
