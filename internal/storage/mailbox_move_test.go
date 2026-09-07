package storage

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestMoveAtomicMetadataQuotaAndRestart(t *testing.T) {
	root := t.TempDir()
	blobs, err := NewMessageStore(filepath.Join(root, "spool"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer blobs.Close()
	path := filepath.Join(root, "mailbox.db")
	s, err := NewMailboxStore(NewIMAPAdapter(blobs), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	ctx := context.Background()
	for _, user := range []string{"alice", "bob"} {
		for _, folder := range []string{"Source", "Archive"} {
			if err := s.CreateFolder(ctx, user, folder); err != nil {
				t.Fatal(err)
			}
		}
	}
	date := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	raw := []byte("Subject: one\r\n\r\npayload")
	a, err := s.AppendMessage(ctx, "alice", "Source", raw, []string{`\Flagged`, "customer"}, date)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.AppendMessage(ctx, "alice", "Source", []byte("Subject: two\r\n\r\nbody"), []string{`\Seen`}, date)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := s.AppendMessage(ctx, "alice", "Source", raw, []string{`\Deleted`}, date)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendMessage(ctx, "alice", "Archive", raw, nil, date); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		user, dest string
		ids        []string
	}{
		{"bob", "Archive", []string{a}},
		{"alice", "Missing", []string{a}},
		{"alice", "Archive", []string{a, "missing"}},
	} {
		if err := s.MoveMessageIDs(ctx, tc.user, "Source", tc.dest, tc.ids); err == nil {
			t.Fatal("invalid move succeeded", tc)
		}
	}
	// Fail after the first row changes to prove both membership and UID state roll back.
	if _, err = s.db.Exec(`CREATE TRIGGER reject_move BEFORE UPDATE OF mailbox ON message_flags WHEN NEW.subject='two' BEGIN SELECT RAISE(ABORT,'injected move failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.MoveMessageIDs(ctx, "alice", "Source", "Archive", []string{a, b}); err == nil {
		t.Fatal("injected failure ignored")
	}
	if got, _ := s.GetMessages(ctx, "alice", "Source"); len(got) != 3 {
		t.Fatal("partial move", got)
	}
	if got, _ := s.GetMessages(ctx, "alice", "Archive"); len(got) != 1 {
		t.Fatal("partial destination", got)
	}
	if next, _ := s.GetUIDNext(ctx, "alice", "Archive"); next != 2 {
		t.Fatal("failed move consumed UIDs", next)
	}
	if _, err = s.db.Exec(`DROP TRIGGER reject_move`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE mailbox_state SET uidnext=4294967295 WHERE username='alice' AND mailbox='Archive'`); err != nil {
		t.Fatal(err)
	}
	if err = s.MoveMessageIDs(ctx, "alice", "Source", "Archive", []string{a}); err == nil {
		t.Fatal("UID exhaustion ignored")
	}
	if got, _ := s.GetMessages(ctx, "alice", "Source"); len(got) != 3 {
		t.Fatal("UID exhaustion lost source", got)
	}
	if _, err = s.db.Exec(`UPDATE mailbox_state SET uidnext=2 WHERE username='alice' AND mailbox='Archive'`); err != nil {
		t.Fatal(err)
	}
	// Moving transfers existing quota usage; it must not reserve a second payload.
	s.SetQuota(1)
	if err = s.MoveMessageIDs(ctx, "alice", "Source", "Archive", []string{b, a, a}); err != nil {
		t.Fatal(err)
	}
	left, _ := s.GetMessages(ctx, "alice", "Source")
	if len(left) != 1 || left[0].ID != deleted || !left[0].Deleted {
		t.Fatal("unrelated deleted message changed", left)
	}
	moved, _ := s.GetMessages(ctx, "alice", "Archive")
	if len(moved) != 3 || moved[1].ID != a || moved[1].UID != 2 || moved[2].ID != b || moved[2].UID != 3 {
		t.Fatal("move ordering/identity", moved)
	}
	if !moved[1].Date.Equal(date) || !containsFlag(moved[1].Flags, "customer") || !containsFlag(moved[1].Flags, `\Flagged`) {
		t.Fatal("metadata changed", moved[1])
	}
	if err = s.MoveMessageIDs(ctx, "alice", "Archive", "Archive", []string{a}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewMailboxStore(NewIMAPAdapter(blobs), path)
	if err != nil {
		t.Fatal(err)
	}
	moved, err = s.GetMessages(ctx, "alice", "Archive")
	if err != nil || len(moved) != 3 || moved[2].ID != a || moved[2].UID != 4 {
		t.Fatal("restart or same-folder MOVE failed", moved, err)
	}
	data, err := s.FetchMessage(ctx, a)
	if err != nil || !bytes.Equal(data, raw) {
		t.Fatal("moved payload lost", err)
	}
	if err = s.DeleteFolder(ctx, "alice", "Source"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FetchMessage(ctx, a); err != nil {
		t.Fatal("source deletion removed moved payload", err)
	}
}
