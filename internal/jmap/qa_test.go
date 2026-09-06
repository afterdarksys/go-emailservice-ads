package jmap

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
)

func TestJMAPReflectsDurableFoldersFlagsAndExpunge(t *testing.T) {
	rawStore, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer rawStore.Close()
	mailbox, err := storage.NewMailboxStore(storage.NewIMAPAdapter(rawStore), filepath.Join(t.TempDir(), "mailbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mailbox.Close()
	ctx := context.Background()
	if err = mailbox.CreateFolder(ctx, "alice", "Projects"); err != nil {
		t.Fatal(err)
	}
	mid, err := mailbox.AppendMessage(ctx, "alice", "Projects", []byte("From: Bob <bob@example.test>\r\nTo: alice@example.test\r\nSubject: unique-project\r\nContent-Type: text/plain\r\n\r\nhello reader"), []string{`\Seen`}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	j := &JMAPServer{logger: zap.NewNop(), store: mailbox}
	response := j.handleEmailGet(ctx, "alice", map[string]interface{}{"ids": []interface{}{mid}}, "a")
	list := response.Arguments["list"].([]map[string]interface{})
	if len(list) != 1 || list[0]["subject"] != "unique-project" || !list[0]["keywords"].(map[string]bool)["$seen"] {
		t.Fatalf("wrong message: %+v", response)
	}
	if len(list[0]["bodyValues"].(map[string]interface{})) == 0 {
		t.Fatal("message body missing")
	}
	query := j.emailQuery(ctx, "alice", map[string]interface{}{"filter": map[string]interface{}{"inMailbox": folderID("Projects")}}, "q")
	if ids := query.Arguments["ids"].([]string); len(ids) != 1 || ids[0] != mid {
		t.Fatalf("query: %+v", query)
	}
	oldState := response.Arguments["state"]
	if err = mailbox.RenameFolder(ctx, "alice", "Projects", "Renamed"); err != nil {
		t.Fatal(err)
	}
	response = j.handleEmailGet(ctx, "alice", map[string]interface{}{"ids": []interface{}{mid}}, "a")
	if response.Arguments["state"] == oldState {
		t.Fatal("state did not track rename")
	}
	req := httptest.NewRequest("GET", "/jmap/download/primary/"+mid+"/message.eml", nil)
	req = req.WithContext(context.WithValue(ctx, authUserKey, "bob"))
	rec := httptest.NewRecorder()
	j.handleDownload(rec, req)
	if rec.Code != 404 {
		t.Fatal("cross-account download", rec.Code)
	}
	req = req.WithContext(context.WithValue(ctx, authUserKey, "alice"))
	rec = httptest.NewRecorder()
	j.handleDownload(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "hello reader") {
		t.Fatal("owned download failed")
	}
	if err = mailbox.DeleteFolder(ctx, "alice", "Renamed"); err != nil {
		t.Fatal(err)
	}
	response = j.handleEmailGet(ctx, "alice", map[string]interface{}{"ids": []interface{}{mid}}, "a")
	if len(response.Arguments["notFound"].([]string)) != 1 {
		t.Fatal("deleted mail still visible")
	}
	for _, method := range []string{"Email/set", "Mailbox/set", "Email/changes"} {
		response = j.processMethodCall(ctx, "alice", MethodCall{Name: method, ID: "a", Arguments: map[string]interface{}{"accountId": "primary"}})
		if response.Name != "error" {
			t.Fatal("fabricated success", method, response)
		}
	}
}
