package jmap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	mm "github.com/emersion/go-message/mail"
	"go.uber.org/zap"
)

func compositionServer(t *testing.T) (*JMAPServer, *storage.MailboxStore, string) {
	t.Helper()
	raw, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	s, err := storage.NewMailboxStore(storage.NewIMAPAdapter(raw), filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	boxes, _, err := s.MailboxSnapshot(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	v := auth.NewValidator(zap.NewNop())
	if err = v.GetUserStore().AddUser("alice", "test-password-123", "alice@example.test"); err != nil {
		t.Fatal(err)
	}
	return &JMAPServer{store: s, validator: v, logger: zap.NewNop()}, s, boxes[0].ID
}

func draftObject(folder string) map[string]interface{} {
	return map[string]interface{}{
		"mailboxIds": map[string]interface{}{folder: true}, "keywords": map[string]interface{}{"$draft": true},
		"from":    []interface{}{map[string]interface{}{"name": "Álice", "email": "alice@example.test"}},
		"to":      []interface{}{map[string]interface{}{"email": "bob@example.test"}},
		"subject": "A café draft", "receivedAt": "2020-01-02T00:00:00Z",
		"textBody":   []interface{}{map[string]interface{}{"partId": "text", "type": "text/plain"}},
		"htmlBody":   []interface{}{map[string]interface{}{"partId": "html", "type": "text/html"}},
		"bodyValues": map[string]interface{}{"text": map[string]interface{}{"value": "hello café"}, "html": map[string]interface{}{"value": "<p>hello café</p>"}},
	}
}

func TestStructuredCreationRoundTripAndValidation(t *testing.T) {
	j, s, folder := compositionServer(t)
	ctx := context.Background()
	blob, err := s.UploadBlob(ctx, "alice", "application/octet-stream", []byte{0, 1, 2, 255})
	if err != nil {
		t.Fatal(err)
	}
	obj := draftObject(folder)
	obj["attachments"] = []interface{}{map[string]interface{}{"blobId": blob.ID, "name": "café.bin", "type": "application/octet-stream"}}
	r := j.processMethodCall(ctx, "alice", MethodCall{Name: "Email/set", Arguments: map[string]interface{}{"create": map[string]interface{}{"draft": obj}}, ID: "a"})
	created := r.Arguments["created"].(map[string]interface{})
	if len(created) != 1 {
		t.Fatal(r)
	}
	mid := created["draft"].(map[string]interface{})["id"].(string)
	data, err := s.FetchMessage(ctx, mid)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := mm.CreateReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := reader.Header.Subject()
	if err != nil || subject != "A café draft" || reader.Header.Get("Message-ID") == "" || reader.Header.Get("Date") == "" {
		t.Fatal(reader.Header, err)
	}
	for i, want := range []string{"hello café", "<p>hello café</p>", string([]byte{0, 1, 2, 255})} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(part.Body)
		if err != nil || string(body) != want {
			t.Fatalf("part %d: %q %v", i, body, err)
		}
		if i == 2 {
			h, ok := part.Header.(*mm.AttachmentHeader)
			if !ok {
				t.Fatal(part.Header)
			}
			name, _ := h.Filename()
			if name != "café.bin" {
				t.Fatal(name)
			}
		}
	}
	if _, err = reader.NextPart(); err != io.EOF {
		t.Fatal(err)
	}
	emails, state, err := s.EmailSnapshot(ctx, "alice")
	if err != nil || !strings.Contains(strings.Join(emails[mid].Flags, " "), `\Draft`) || emails[mid].Date.Year() != 2020 {
		t.Fatal(emails, err)
	}
	stale := j.processMethodCall(ctx, "alice", MethodCall{Name: "Email/set", Arguments: map[string]interface{}{"ifInState": r.Arguments["oldState"], "create": map[string]interface{}{"again": obj}}, ID: "a"})
	if stale.Arguments["type"] != "stateMismatch" {
		t.Fatal(stale)
	}
	for key, value := range map[string]interface{}{"subject": "injected\r\nBcc: victim@example.test", "from": []interface{}{map[string]interface{}{"email": "bad\n@example.test"}}, "bodyStructure": map[string]interface{}{}, "bodyValues": map[string]interface{}{}, "messageId": []interface{}{"bad id"}, "attachments": []interface{}{map[string]interface{}{"blobId": blob.ID, "type": "invalid type"}}} {
		bad := draftObject(folder)
		bad[key] = value
		if _, kind, _ := j.buildEmail(ctx, "alice", bad); kind != "invalidProperties" {
			t.Fatalf("%s: %s", key, kind)
		}
	}
	foreign := draftObject(folder)
	foreign["attachments"] = obj["attachments"]
	if _, kind, missing := j.buildEmail(ctx, "bob", foreign); kind != "blobNotFound" || len(missing) != 1 {
		t.Fatal(kind, missing)
	}
	_, after, _ := s.EmailSnapshot(ctx, "alice")
	if state != after {
		t.Fatal("validation changed state")
	}
}

type recordingSubmitter struct {
	records map[string][]mailstate.Submission
	calls   int
}

func (s *recordingSubmitter) Submit(ctx context.Context, user, ip, mid, from string, to []string, data []byte) (mailstate.Submission, error) {
	s.calls++
	r := mailstate.Submission{ID: "receipt", EmailID: mid, IdentityID: "primary", ThreadID: mid, SendAt: "2026-01-01T00:00:00Z", UndoStatus: "final"}
	if s.calls > 1 {
		r.ID = fmt.Sprint("receipt-", s.calls)
	}
	s.records[user] = append(s.records[user], r)
	return r, nil
}
func (s *recordingSubmitter) Submissions(ctx context.Context, user string) ([]mailstate.Submission, error) {
	return append([]mailstate.Submission{}, s.records[user]...), nil
}

func TestSubmissionCreationReferenceAuthorizationAndState(t *testing.T) {
	j, s, folder := compositionServer(t)
	sub := &recordingSubmitter{records: map[string][]mailstate.Submission{}}
	j.SetSubmitter(sub)
	ctx := context.WithValue(context.Background(), authUserKey, "alice")
	request := &Request{MethodCalls: []MethodCall{
		{Name: "Email/set", Arguments: map[string]interface{}{"create": map[string]interface{}{"draft": draftObject(folder)}}, ID: "create"},
		{Name: "EmailSubmission/set", Arguments: map[string]interface{}{"create": map[string]interface{}{"send": map[string]interface{}{"emailId": "#draft", "identityId": "primary"}}}, ID: "send"},
	}}
	r := j.processRequest(ctx, request)
	if sub.calls != 1 || r.CreatedIds["draft"] == "" || r.CreatedIds["send"] != "receipt" {
		t.Fatal(r, sub)
	}
	mid := r.CreatedIds["draft"]
	call := func(user, name string, args map[string]interface{}) MethodResponse {
		return j.processMethodCall(ctx, user, MethodCall{Name: name, Arguments: args, ID: "a"})
	}
	identity := call("alice", "Identity/get", nil)
	if identity.Arguments["list"].([]map[string]interface{})[0]["email"] != "alice@example.test" {
		t.Fatal(identity)
	}
	get := call("alice", "EmailSubmission/get", nil)
	state := get.Arguments["state"]
	valid := map[string]interface{}{"emailId": mid, "identityId": "primary"}
	for _, tc := range []struct {
		user string
		args map[string]interface{}
		kind string
	}{
		{"alice", map[string]interface{}{"ifInState": "stale", "create": map[string]interface{}{"send": valid}}, "stateMismatch"},
		{"alice", map[string]interface{}{"create": map[string]interface{}{"send": valid}, "onSuccessDestroyEmail": true}, "invalidArguments"},
		{"bob", map[string]interface{}{"create": map[string]interface{}{"send": valid}}, "invalidProperties"},
	} {
		got := call(tc.user, "EmailSubmission/set", tc.args)
		kind := got.Arguments["type"]
		if got.Name != "error" {
			kind = got.Arguments["notCreated"].(map[string]interface{})["send"].(map[string]interface{})["type"]
		}
		if kind != tc.kind || sub.calls != 1 {
			t.Fatal(got, sub.calls)
		}
	}
	for _, tc := range []struct{ headers, kind string }{
		{"From: impostor@example.test\r\nTo: bob@example.test", "forbiddenFrom"},
		{"From: alice@example.test", "noRecipients"},
	} {
		id, err := s.StoreMessage(ctx, "alice", "INBOX", []byte(tc.headers+"\r\n\r\nbody"))
		if err != nil {
			t.Fatal(err)
		}
		got := call("alice", "EmailSubmission/set", map[string]interface{}{"create": map[string]interface{}{"bad": map[string]interface{}{"emailId": id, "identityId": "primary"}}})
		if got.Arguments["notCreated"].(map[string]interface{})["bad"].(map[string]interface{})["type"] != tc.kind || sub.calls != 1 {
			t.Fatal(got)
		}
	}
	if _, err := s.SetEmails(ctx, "alice", "", nil, []string{mid}); err != nil {
		t.Fatal(err)
	}
	get = call("alice", "EmailSubmission/get", map[string]interface{}{"ids": []interface{}{"receipt"}, "properties": []interface{}{"emailId"}})
	if get.Arguments["state"] != state || get.Arguments["list"].([]map[string]interface{})[0]["emailId"] != mid {
		t.Fatal(get)
	}
	foreign := call("bob", "EmailSubmission/get", map[string]interface{}{"ids": []interface{}{"receipt"}})
	if len(foreign.Arguments["notFound"].([]string)) != 1 {
		t.Fatal(foreign)
	}
}

func TestCreationBatchBoundsExpandedAttachmentBytes(t *testing.T) {
	j, s, folder := compositionServer(t)
	ctx := context.Background()
	blob, err := s.UploadBlob(ctx, "alice", "application/octet-stream", bytes.Repeat([]byte{1}, 4*1024*1024))
	if err != nil {
		t.Fatal(err)
	}
	obj := draftObject(folder)
	obj["attachments"] = []interface{}{map[string]interface{}{"blobId": blob.ID}}
	r := j.processMethodCall(ctx, "alice", MethodCall{Name: "Email/set", ID: "bounded", Arguments: map[string]interface{}{"create": map[string]interface{}{"a": obj, "b": obj}}})
	if len(r.Arguments["created"].(map[string]interface{})) != 1 || r.Arguments["notCreated"].(map[string]interface{})["b"].(map[string]interface{})["type"] != "tooLarge" {
		t.Fatal(r)
	}
}
