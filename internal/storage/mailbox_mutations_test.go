package storage

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
	goimap "github.com/emersion/go-imap"
	"go.uber.org/zap"
)

func TestMailboxMutationsSurviveRestartAndRemainAccountScoped(t *testing.T) {
	root := t.TempDir()
	store, err := NewMessageStore(filepath.Join(root, "spool"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	path := filepath.Join(root, "mailbox.db")
	m, err := NewMailboxStore(NewIMAPAdapter(store), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	user := imap.NewUser(zap.NewNop(), m, "alice")
	if err = user.CreateMailbox("Projects/2026"); err != nil {
		t.Fatal(err)
	}
	if err = user.CreateMailbox("Archive"); err != nil {
		t.Fatal(err)
	}
	box, err := user.GetMailbox("Projects/2026")
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2020, 1, 2, 12, 0, 0, 0, time.FixedZone("offset", 3600))
	raw := []byte("From: sender@example.test\r\nDate: Mon, 02 Jan 2006 15:04:05 -0700\r\nSubject: project\r\n\r\nbody")
	if _, err = m.AppendMessage(ctx, "alice", "Projects/2026", raw, []string{goimap.SeenFlag, "customer"}, date); err != nil {
		t.Fatal(err)
	}
	if err = box.SetSubscribed(true); err != nil {
		t.Fatal(err)
	}
	set := new(goimap.SeqSet)
	set.Add("*")
	if err = box.CopyMessages(true, set, "Archive"); err != nil {
		t.Fatal(err)
	}
	original, _ := m.GetMessages(ctx, "alice", "Projects/2026")
	copies, _ := m.GetMessages(ctx, "alice", "Archive")
	if len(copies) != 1 || copies[0].ID == original[0].ID || !copies[0].Date.Equal(date) || !containsFlag(copies[0].Flags, goimap.SeenFlag) {
		t.Fatalf("copy metadata: %+v", copies)
	}
	if err = user.RenameMailbox("Projects", "Work"); err != nil {
		t.Fatal(err)
	}
	if _, err = user.GetMailbox("Projects/2026"); err == nil {
		t.Fatal("old folder still selectable")
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = NewMailboxStore(NewIMAPAdapter(store), path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	got, err := m.GetMessages(ctx, "alice", "Work/2026")
	if err != nil || len(got) != 1 || got[0].UID != original[0].UID || !got[0].Date.Equal(date) {
		t.Fatalf("restart: %+v %v", got, err)
	}
	data, err := m.FetchMessage(ctx, got[0].ID)
	if err != nil || !bytes.Equal(data, raw) {
		t.Fatal("renamed payload lost", err)
	}
	folders, err := m.ListFolders(ctx, "alice", true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range folders {
		if name == "Work/2026" {
			found = true
		}
	}
	if !found {
		t.Fatal("subscription lost")
	}
	other, err := m.GetMessages(ctx, "bob", "Work/2026")
	if err != nil || len(other) != 0 {
		t.Fatal("cross-account membership")
	}
	if err = m.CopyMessageIDs(ctx, "bob", "Work/2026", "INBOX", []string{got[0].ID}); err == nil {
		t.Fatal("cross-account COPY succeeded")
	}
	if err = m.DeleteFolder(ctx, "alice", "Work/2026"); err != nil {
		t.Fatal(err)
	}
	got, err = m.GetMessages(ctx, "alice", "Archive")
	if err != nil || len(got) != 1 {
		t.Fatal("delete damaged copy")
	}
	if _, err = m.FetchMessage(ctx, got[0].ID); err != nil {
		t.Fatal("copy payload deleted", err)
	}
}

func TestMailboxRecreationChangesUIDValidityAndCopyIsAtomic(t *testing.T) {
	s, _ := NewMessageStore(t.TempDir(), zap.NewNop())
	defer s.Close()
	m, err := NewMailboxStore(NewIMAPAdapter(s), filepath.Join(t.TempDir(), "mailbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx := context.Background()
	if err = m.CreateFolder(ctx, "alice", "Archive"); err != nil {
		t.Fatal(err)
	}
	before, err := m.GetUIDValidity(ctx, "alice", "Archive")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.DeleteFolder(ctx, "alice", "Archive"); err != nil {
		t.Fatal(err)
	}
	if err = m.CreateFolder(ctx, "alice", "Archive"); err != nil {
		t.Fatal(err)
	}
	after, _ := m.GetUIDValidity(ctx, "alice", "Archive")
	if before == after {
		t.Fatal("recreated UIDVALIDITY reused")
	}
	a, err := m.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: one\r\n\r\na"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: two\r\n\r\nb"))
	if err != nil {
		t.Fatal(err)
	}
	m.SetQuota(55)
	if err = m.CopyMessageIDs(ctx, "alice", "INBOX", "Archive", []string{a, b}); err == nil {
		t.Fatal("quota did not reject COPY")
	}
	got, err := m.GetMessages(ctx, "alice", "Archive")
	if err != nil || len(got) != 0 {
		t.Fatal("partial COPY acknowledged", got, err)
	}
	m.SetQuota(0)
	if err = m.RenameFolder(ctx, "alice", "INBOX", "MovedInbox"); err != nil {
		t.Fatal(err)
	}
	inbox, _ := m.GetMessages(ctx, "alice", "INBOX")
	moved, _ := m.GetMessages(ctx, "alice", "MovedInbox")
	if len(inbox) != 0 || len(moved) != 2 {
		t.Fatal("INBOX rename did not move messages")
	}
	if _, err = m.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: new\r\n\r\nbody")); err != nil {
		t.Fatal(err)
	}
}
