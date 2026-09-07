package jmap

import (
	"context"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"testing"
)

func TestThreadsCollapseIsolationAndAncestorDeletion(t *testing.T) {
	j, s, _ := compositionServer(t)
	ctx := context.Background()
	root := []byte("Message-ID: <root@example.test>\r\nSubject: original\r\n\r\nhello")
	reply := []byte("Message-ID: <reply@example.test>\r\nReferences: <root@example.test>\r\nSubject: reply\r\n\r\nhello")
	a, err := s.DeliverOnce(ctx, "root", "alice", "INBOX", root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.DeliverOnce(ctx, "reply", "alice", "INBOX", reply)
	if err != nil {
		t.Fatal(err)
	}
	tid := mailstate.ThreadID(a, root)
	if mailstate.ThreadID(b, reply) != tid {
		t.Fatal("reply split")
	}
	get := j.threadGet(ctx, "alice", map[string]interface{}{"ids": []interface{}{tid}}, "a")
	if get.Name == "error" || len(get.Arguments["list"].([]map[string]interface{})[0]["emailIds"].([]string)) != 2 {
		t.Fatal(get)
	}
	foreign := j.threadGet(ctx, "bob", map[string]interface{}{"ids": []interface{}{tid}}, "a")
	if len(foreign.Arguments["notFound"].([]string)) != 1 {
		t.Fatal(foreign)
	}
	q := j.emailQuery(ctx, "alice", map[string]interface{}{"collapseThreads": true}, "q")
	if q.Arguments["total"] != 1 {
		t.Fatal(q)
	}
	_, err = s.SetEmails(ctx, "alice", "", nil, []string{a})
	if err != nil {
		t.Fatal(err)
	}
	changes := j.threadChanges(ctx, "alice", map[string]interface{}{"sinceState": get.Arguments["state"]}, "c")
	if changes.Name == "error" || len(changes.Arguments["updated"].([]string)) != 1 {
		t.Fatal(changes)
	}
	q2 := j.emailQuery(ctx, "alice", map[string]interface{}{"collapseThreads": true}, "q")
	if q2.Arguments["total"] != 1 {
		t.Fatal(q2)
	}
	got := j.handleEmailGet(ctx, "alice", map[string]interface{}{"ids": []interface{}{b}}, "g")
	if got.Name == "error" {
		t.Fatal(got)
	}
}
