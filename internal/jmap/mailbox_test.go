package jmap

import (
	"context"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	"path/filepath"
	"testing"
)

func TestMailboxSetAndChanges(t *testing.T) {
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
	ctx := context.Background()
	j := &JMAPServer{store: s, logger: zap.NewNop()}
	call := func(name string, args map[string]interface{}) MethodResponse {
		return j.processMethodCall(ctx, "alice", MethodCall{Name: name, Arguments: args, ID: "a"})
	}
	initial := call("Mailbox/get", nil).Arguments["state"]
	r := call("Mailbox/set", map[string]interface{}{"ifInState": initial, "create": map[string]interface{}{"p": map[string]interface{}{"name": "工程"}, "c": map[string]interface{}{"name": "Child", "parentId": "#p", "isSubscribed": true, "sortOrder": float64(7)}, "bad": map[string]interface{}{"name": "Bad", "role": "sent"}}})
	if r.Name != "Mailbox/set" {
		t.Fatal(r)
	}
	created := r.Arguments["created"].(map[string]interface{})
	if len(created) != 2 || len(r.Arguments["notCreated"].(map[string]interface{})) != 1 {
		t.Fatal(r)
	}
	parent := created["p"].(map[string]interface{})["id"].(string)
	child := created["c"].(map[string]interface{})["id"].(string)
	r = call("Mailbox/set", map[string]interface{}{"update": map[string]interface{}{parent: map[string]interface{}{"name": "Renamed"}}})
	if len(r.Arguments["updated"].(map[string]interface{})) != 1 {
		t.Fatal(r)
	}
	if folders, err := s.ListFolders(ctx, "alice", true); err != nil {
		t.Fatal(err)
	} else {
		found := false
		for _, p := range folders {
			if p == "Renamed/Child" {
				found = true
			}
		}
		if !found {
			t.Fatal(folders)
		}
	}
	r = call("Mailbox/get", map[string]interface{}{"ids": []interface{}{child}, "properties": []interface{}{"parentId", "sortOrder", "isSubscribed"}})
	box := r.Arguments["list"].([]map[string]interface{})[0]
	if len(box) != 4 || box["parentId"] != parent || box["sortOrder"] != 7 || box["isSubscribed"] != true {
		t.Fatal(box)
	}
	r = call("Mailbox/set", map[string]interface{}{"update": map[string]interface{}{child: map[string]interface{}{"parentId": nil}}})
	if len(r.Arguments["updated"].(map[string]interface{})) != 1 {
		t.Fatal(r)
	}
	r = call("Mailbox/changes", map[string]interface{}{"sinceState": initial})
	if r.Name != "Mailbox/changes" || len(r.Arguments["created"].([]string)) != 2 {
		t.Fatal(r)
	}
	r = call("Mailbox/set", map[string]interface{}{"ifInState": initial, "destroy": []interface{}{child}})
	if r.Arguments["type"] != "stateMismatch" {
		t.Fatal(r)
	}
	for _, args := range []map[string]interface{}{{"destroy": true}, {"onDestroyRemoveEmails": true}, {"create": []interface{}{}}, {"bogus": true}} {
		r = call("Mailbox/set", args)
		if r.Arguments["type"] != "invalidArguments" {
			t.Fatal(r)
		}
	}
	for _, value := range []interface{}{map[string]interface{}{"sortOrder": -1.0}, map[string]interface{}{"name": "a/b"}, map[string]interface{}{"parentId": parent}} {
		r = call("Mailbox/set", map[string]interface{}{"update": map[string]interface{}{parent: value}})
		if len(r.Arguments["notUpdated"].(map[string]interface{})) != 1 {
			t.Fatal(r)
		}
	}
	r = call("Mailbox/set", map[string]interface{}{"create": map[string]interface{}{"a": map[string]interface{}{"name": "a", "parentId": "#b"}, "b": map[string]interface{}{"name": "b", "parentId": "#a"}}})
	if len(r.Arguments["notCreated"].(map[string]interface{})) != 2 {
		t.Fatal(r)
	}
	r = call("Mailbox/set", map[string]interface{}{"destroy": []interface{}{parent, child}})
	if len(r.Arguments["destroyed"].([]string)) != 2 {
		t.Fatal(r)
	}
}
