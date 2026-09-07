package jmap

import (
	"context"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	"path/filepath"
	"testing"
)

func TestEmailMembershipAndDestroyAPI(t *testing.T) {
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
	j := &JMAPServer{store: s, logger: zap.NewNop()}
	call := func(name string, args map[string]interface{}) MethodResponse {
		return j.processMethodCall(ctx, "alice", MethodCall{Name: name, Arguments: args, ID: "a"})
	}
	boxes := call("Mailbox/get", nil).Arguments["list"].([]map[string]interface{})
	ids := map[string]string{}
	for _, b := range boxes {
		ids[b["name"].(string)] = b["id"].(string)
		if !b["myRights"].(map[string]bool)["mayRemoveItems"] {
			t.Fatal(b)
		}
	}
	mid, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: test\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	state := call("Email/get", nil).Arguments["state"]
	r := call("Email/set", map[string]interface{}{"ifInState": state, "update": map[string]interface{}{mid: map[string]interface{}{"mailboxIds/" + ids["INBOX"]: nil, "mailboxIds/" + ids["Sent"]: true, "keywords/$seen": true}}})
	if r.Name != "Email/set" || len(r.Arguments["updated"].(map[string]interface{})) != 1 {
		t.Fatal(r)
	}
	obj := call("Email/get", map[string]interface{}{"ids": []interface{}{mid}}).Arguments["list"].([]map[string]interface{})[0]
	if !obj["mailboxIds"].(map[string]bool)[ids["Sent"]] || !obj["keywords"].(map[string]bool)["$seen"] {
		t.Fatal(obj)
	}
	for _, tc := range []struct {
		patch map[string]interface{}
		kind  string
	}{
		{map[string]interface{}{"mailboxIds": map[string]interface{}{}}, "invalidProperties"},
		{map[string]interface{}{"mailboxIds": map[string]interface{}{ids["Sent"]: true, ids["INBOX"]: true}}, "tooManyMailboxes"},
		{map[string]interface{}{"mailboxIds/missing": true}, "tooManyMailboxes"},
		{map[string]interface{}{"mailboxIds": map[string]interface{}{"missing": true}, "keywords/$flagged": true}, "invalidProperties"},
		{map[string]interface{}{"mailboxIds": map[string]interface{}{ids["Sent"]: false}}, "invalidProperties"},
		{map[string]interface{}{"mailboxIds": map[string]interface{}{ids["Sent"]: true}, "mailboxIds/" + ids["Sent"]: nil}, "invalidProperties"},
		{map[string]interface{}{"subject": "immutable"}, "invalidProperties"},
	} {
		r = call("Email/set", map[string]interface{}{"update": map[string]interface{}{mid: tc.patch}})
		if r.Arguments["notUpdated"].(map[string]interface{})[mid].(map[string]interface{})["type"] != tc.kind || r.Arguments["oldState"] != r.Arguments["newState"] {
			t.Fatal(tc, r)
		}
	}
	r = call("Email/set", map[string]interface{}{"ifInState": state, "destroy": []interface{}{mid}})
	if r.Arguments["type"] != "stateMismatch" {
		t.Fatal(r)
	}
	r = call("Email/set", map[string]interface{}{"update": map[string]interface{}{mid: map[string]interface{}{"subject": "ignored"}}, "destroy": []interface{}{mid, mid, "missing"}})
	if len(r.Arguments["destroyed"].([]string)) != 1 || r.Arguments["notUpdated"].(map[string]interface{})[mid].(map[string]interface{})["type"] != "willDestroy" {
		t.Fatal(r)
	}
	r = call("Email/get", map[string]interface{}{"ids": []interface{}{mid}})
	if len(r.Arguments["notFound"].([]string)) != 1 {
		t.Fatal(r)
	}
	r = call("Email/set", map[string]interface{}{"destroy": []interface{}{mid}})
	if r.Arguments["notDestroyed"].(map[string]interface{})[mid].(map[string]interface{})["type"] != "notFound" {
		t.Fatal(r)
	}
}
