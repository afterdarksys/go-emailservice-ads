package jmap

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	goimap "github.com/emersion/go-imap"
	"go.uber.org/zap"
)

func TestKeywordWritesAndDurableChanges(t *testing.T) {
	ctx := context.Background()
	raw, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	path := filepath.Join(t.TempDir(), "mailbox.db")
	box, err := storage.NewMailboxStore(storage.NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { box.Close() }()
	j := &JMAPServer{store: box, logger: zap.NewNop()}
	call := func(name string, args map[string]interface{}) MethodResponse {
		t.Helper()
		return j.processMethodCall(ctx, "alice", MethodCall{Name: name, Arguments: args, ID: "c"})
	}
	get := func() string {
		t.Helper()
		r := call("Email/get", map[string]interface{}{})
		if r.Name != "Email/get" {
			t.Fatal(r)
		}
		return r.Arguments["state"].(string)
	}
	initial := get()
	a, err := box.AppendMessage(ctx, "alice", "INBOX", []byte("Subject: sync\r\n\r\nbody"), []string{`\Deleted`}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, err := box.AppendMessage(ctx, "alice", "INBOX", []byte("Subject: second\r\n\r\nbody"), nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	page := call("Email/changes", map[string]interface{}{"sinceState": initial, "maxChanges": float64(1)})
	if page.Name != "Email/changes" || page.Arguments["hasMoreChanges"] != true || page.Arguments["created"].([]string)[0] != a {
		t.Fatal(page)
	}
	page = call("Email/changes", map[string]interface{}{"sinceState": page.Arguments["newState"], "maxChanges": float64(1)})
	if page.Arguments["hasMoreChanges"] != false || page.Arguments["created"].([]string)[0] != b {
		t.Fatal(page)
	}
	before := get()
	set := call("Email/set", map[string]interface{}{"ifInState": before, "update": map[string]interface{}{a: map[string]interface{}{"keywords/$seen": true, "keywords/project~1alpha": true}, "missing": map[string]interface{}{"keywords/$seen": true}}})
	if set.Name != "Email/set" || len(set.Arguments["updated"].(map[string]interface{})) != 1 || len(set.Arguments["notUpdated"].(map[string]interface{})) != 1 {
		t.Fatal(set)
	}
	msgs, err := box.GetMessages(ctx, "alice", "INBOX")
	if err != nil || !msgs[0].Deleted || !keywords(msgs[0].Flags)["$seen"] || !keywords(msgs[0].Flags)["project/alpha"] {
		t.Fatal(msgs, err)
	}
	stale := call("Email/set", map[string]interface{}{"ifInState": before, "update": map[string]interface{}{a: map[string]interface{}{"keywords": map[string]interface{}{}}}})
	if stale.Arguments["type"] != "stateMismatch" {
		t.Fatal(stale)
	}
	// A complete replacement clears visible keywords but retains IMAP Deleted.
	set = call("Email/set", map[string]interface{}{"update": map[string]interface{}{a: map[string]interface{}{"keywords": map[string]interface{}{}}}})
	if set.Name != "Email/set" {
		t.Fatal(set)
	}
	changes := call("Email/changes", map[string]interface{}{"sinceState": before})
	if len(changes.Arguments["updated"].([]string)) != 1 || changes.Arguments["updated"].([]string)[0] != a {
		t.Fatal("change-back lost", changes)
	}
	other := j.processMethodCall(ctx, "bob", MethodCall{Name: "Email/set", Arguments: map[string]interface{}{"update": map[string]interface{}{a: map[string]interface{}{"keywords/$seen": true}}}})
	if len(other.Arguments["notUpdated"].(map[string]interface{})) != 1 {
		t.Fatal("cross-owner write", other)
	}
	other = j.processMethodCall(ctx, "bob", MethodCall{Name: "Email/changes", Arguments: map[string]interface{}{"sinceState": before}})
	if other.Arguments["type"] != "cannotCalculateChanges" {
		t.Fatal("cross-owner state", other)
	}
	if err = box.CreateFolder(ctx, "alice", "Archive"); err != nil {
		t.Fatal(err)
	}
	checkpoint := get()
	if err = box.MoveMessageIDs(ctx, "alice", "INBOX", "Archive", []string{b}); err != nil {
		t.Fatal(err)
	}
	if _, err = box.ExpungeDeleted(ctx, "alice", "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err = box.Close(); err != nil {
		t.Fatal(err)
	}
	box, err = storage.NewMailboxStore(storage.NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	j.store = box
	changes = call("Email/changes", map[string]interface{}{"sinceState": checkpoint})
	if changes.Name != "Email/changes" || len(changes.Arguments["destroyed"].([]string)) != 1 || changes.Arguments["destroyed"].([]string)[0] != a || len(changes.Arguments["updated"].([]string)) != 1 || changes.Arguments["updated"].([]string)[0] != b {
		t.Fatal("restart changes", changes)
	}
	checkpoint = get()
	if err = box.UpdateMessageFlags(ctx, b, "alice", "Archive", goimap.AddFlags, []string{goimap.SeenFlag}); err != nil {
		t.Fatal(err)
	}
	if get() == checkpoint {
		t.Fatal("IMAP flag update did not advance state")
	}
}

func TestKeywordPatchValidation(t *testing.T) {
	for _, value := range []interface{}{
		map[string]interface{}{"mailboxIds": map[string]interface{}{"inbox": true}},
		map[string]interface{}{"keywords/$seen": false},
		map[string]interface{}{"keywords/a~2b": true},
		map[string]interface{}{"keywords/a/b": true},
		map[string]interface{}{"keywords/\\Deleted": true},
		map[string]interface{}{"keywords": map[string]interface{}{}, "keywords/$seen": true},
		map[string]interface{}{"keywords": map[string]interface{}{"bad flag": true}},
	} {
		if _, ok := keywordPatch(value); ok {
			t.Fatalf("accepted invalid patch %#v", value)
		}
	}
	if _, ok := keywordPatch(map[string]interface{}{"keywords/$seen": nil}); !ok {
		t.Fatal("null removal rejected")
	}
}
