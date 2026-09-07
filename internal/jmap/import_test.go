package jmap

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestUploadImportHTTPAndValidation(t *testing.T) {
	ctx := context.Background()
	raw, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	s, err := storage.NewMailboxStore(storage.NewIMAPAdapter(raw), filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	j := &JMAPServer{store: s, logger: zap.NewNop(), requestSem: make(chan struct{}, maxJMAPConcurrent)}
	data := []byte("Subject: imported\r\nContent-Type: text/plain\r\n\r\nbody")
	upload := func(user, path, method, media string, data []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r = r.WithContext(context.WithValue(ctx, authUserKey, user))
		r.Header.Set("Content-Type", media)
		w := httptest.NewRecorder()
		j.handleUpload(w, r)
		return w
	}
	for _, tc := range []struct {
		user, path, method, media string
		data                      []byte
		status                    int
	}{
		{"alice", "/jmap/upload/other/", "POST", "", data, 404}, {"", "/jmap/upload/primary/", "POST", "", data, 401}, {"alice", "/jmap/upload/primary/", "GET", "", data, 405}, {"alice", "/jmap/upload/primary/", "POST", "not valid media", data, 400}, {"alice", "/jmap/upload/primary/", "POST", "", make([]byte, mailstate.MaxUploadBytes+1), 413},
	} {
		w := upload(tc.user, tc.path, tc.method, tc.media, tc.data)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for i := 0; i < maxJMAPConcurrent; i++ {
		j.requestSem <- struct{}{}
	}
	if w := upload("alice", "/jmap/upload/primary/", "POST", "", data); w.Code != 429 {
		t.Fatal(w.Code)
	}
	for i := 0; i < maxJMAPConcurrent; i++ {
		<-j.requestSem
	}
	w := upload("alice", "/jmap/upload/primary/", "POST", "message/rfc822", data)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var blob map[string]interface{}
	if err = json.Unmarshal(w.Body.Bytes(), &blob); err != nil {
		t.Fatal(err)
	}
	if blob["size"] != float64(len(data)) || blob["type"] != "message/rfc822" {
		t.Fatal(blob)
	}
	for _, user := range []string{"alice", "bob"} {
		r := httptest.NewRequest("GET", "/jmap/download/primary/"+blob["blobId"].(string)+"/mail.eml", nil)
		r = r.WithContext(context.WithValue(ctx, authUserKey, user))
		w = httptest.NewRecorder()
		j.handleDownload(w, r)
		if user == "bob" {
			if w.Code != 404 {
				t.Fatal(w.Code)
			}
		} else if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) || w.Header().Get("Content-Disposition") != "attachment; filename=mail.eml" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	boxes, _, err := s.MailboxSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	call := func(args map[string]interface{}) MethodResponse {
		return j.processMethodCall(ctx, "alice", MethodCall{Name: "Email/import", Arguments: args, ID: "a"})
	}
	valid := map[string]interface{}{"blobId": blob["blobId"], "mailboxIds": map[string]interface{}{boxes[0].ID: true}, "keywords": map[string]interface{}{"$seen": true}, "receivedAt": "2020-01-02T00:00:00Z"}
	r := call(map[string]interface{}{"emails": map[string]interface{}{"good": valid, "bad": map[string]interface{}{"blobId": blob["blobId"]}}})
	if r.Name != "Email/import" || len(r.Arguments["created"].(map[string]interface{})) != 1 || len(r.Arguments["notCreated"].(map[string]interface{})) != 1 {
		t.Fatal(r)
	}
	stale := call(map[string]interface{}{"ifInState": r.Arguments["oldState"], "emails": map[string]interface{}{"stale": valid}})
	if stale.Arguments["type"] != "stateMismatch" {
		t.Fatal(stale)
	}
	for _, args := range []map[string]interface{}{{}, {"emails": true}, {"emails": map[string]interface{}{}, "extra": true}, {"emails": map[string]interface{}{}, "ifInState": true}} {
		if r = call(args); r.Arguments["type"] != "invalidArguments" {
			t.Fatal(r)
		}
	}
	for key, value := range map[string]interface{}{"keywords": map[string]interface{}{"$seen": false}, "mailboxIds": map[string]interface{}{}, "receivedAt": "yesterday", "blobId": false} {
		bad := map[string]interface{}{}
		for k, v := range valid {
			bad[k] = v
		}
		bad[key] = value
		r = call(map[string]interface{}{"emails": map[string]interface{}{"bad": bad}})
		if len(r.Arguments["notCreated"].(map[string]interface{})) != 1 {
			t.Fatal(r)
		}
	}
}
