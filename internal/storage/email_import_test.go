package storage

import (
	"bytes"
	"context"
	"errors"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"go.uber.org/zap"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestUploadImportAtomicOwnershipAndRestart(t *testing.T) {
	ctx := context.Background()
	raw, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	path := filepath.Join(t.TempDir(), "mail.db")
	s, err := NewMailboxStore(NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	boxes, mailboxState, err := s.MailboxSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	folder := boxes[0].ID
	folderPath := boxes[0].Path
	data := []byte("From: person@example.test\r\nSubject: こんにちは\r\nReceived: by localhost; Mon, 02 Jan 2006 15:04:05 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nbody")
	upload, err := s.UploadBlob(ctx, "alice", "message/rfc822", data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetBlob(ctx, "bob", upload.ID); !errors.Is(err, mailstate.ErrBlobNotFound) {
		t.Fatal("cross-owner download", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewMailboxStore(NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	fetched, err := s.GetBlob(ctx, "alice", upload.ID)
	if err != nil || !bytes.Equal(fetched.Data, data) {
		t.Fatal(fetched, err)
	}
	_, initial, err := s.EmailSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	p := mailstate.EmailImport{BlobID: upload.ID, MailboxID: folder, Flags: []string{`\Seen`, "customer"}}
	if _, err = s.db.Exec(`CREATE TRIGGER reject_import BEFORE UPDATE OF uidnext ON mailbox_state BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	r, err := s.ImportEmails(ctx, "alice", initial, map[string]mailstate.EmailImport{"fail": p})
	if err != nil || r.NotCreated["fail"] != "serverFail" || r.NewState != initial {
		t.Fatal(r, err)
	}
	if len(raw.ListByStatus("stored", "mailbox")) != 0 {
		t.Fatal("orphan imported payload remains active")
	}
	_, after, _ := s.MailboxSnapshot(ctx, "alice")
	if after != mailboxState {
		t.Fatal("count event escaped rollback")
	}
	if _, err = s.db.Exec(`DROP TRIGGER reject_import`); err != nil {
		t.Fatal(err)
	}
	bad := p
	bad.MailboxID = "missing"
	s.SetQuota(int64(len(data)))
	r, err = s.ImportEmails(ctx, "alice", initial, map[string]mailstate.EmailImport{"a": p, "b": p, "bad": bad})
	if err != nil || len(r.Created) != 1 || r.NotCreated["b"] != "overQuota" || r.NotCreated["bad"] != "invalidProperties" {
		t.Fatal(r, err)
	}
	id := r.Created["a"].ID
	messages, _, err := s.EmailSnapshot(ctx, "alice")
	if err != nil || messages[id].Folder != folderPath || messages[id].UID != 1 || len(messages[id].Flags) != 2 || messages[id].Date.Year() != 2006 {
		t.Fatal(messages, err)
	}
	actual, err := s.FetchMessage(ctx, id)
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("raw MIME changed", err)
	}
	if _, err = s.ImportEmails(ctx, "alice", initial, map[string]mailstate.EmailImport{"stale": p}); !errors.Is(err, mailstate.ErrStateMismatch) {
		t.Fatal("stale import", err)
	}
	foreign, _, err := s.MailboxSnapshot(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	q := p
	q.MailboxID = foreign[0].ID
	r, err = s.ImportEmails(ctx, "bob", "", map[string]mailstate.EmailImport{"foreign": q})
	if err != nil || r.NotCreated["foreign"] != "invalidProperties" {
		t.Fatal(r, err)
	}
	s.SetQuota(0)
	q = p
	q.BlobID = id
	q.ReceivedAt = time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	r, err = s.ImportEmails(ctx, "alice", "", map[string]mailstate.EmailImport{"copy": q})
	if err != nil || len(r.Created) != 1 || r.Created["copy"].ID == id {
		t.Fatal(r, err)
	}
	if _, err = s.db.Exec(`UPDATE jmap_uploads SET expires=0`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetBlob(ctx, "alice", upload.ID); !errors.Is(err, mailstate.ErrBlobNotFound) {
		t.Fatal("expired download", err)
	}
	r, err = s.ImportEmails(ctx, "alice", "", map[string]mailstate.EmailImport{"expired": p})
	if err != nil || r.NotCreated["expired"] != "invalidProperties" {
		t.Fatal(r, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewMailboxStore(NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := s.EmailChanges(ctx, "alice", initial, 500)
	if err != nil || len(changes.Created) != 2 {
		t.Fatal(changes, err)
	}
	messages, _, err = s.EmailSnapshot(ctx, "alice")
	if err != nil || len(messages) != 2 {
		t.Fatal(messages, err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM jmap_uploads`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expiry cleanup", count, err)
	}
}
func TestUploadLimitsAndConcurrentImportState(t *testing.T) {
	ctx := context.Background()
	raw, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	s, err := NewMailboxStore(NewIMAPAdapter(raw), filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < maxUploadCount; i++ {
		if _, err = s.UploadBlob(ctx, "alice", "application/octet-stream", []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	var oldest string
	if err = s.db.QueryRow(`SELECT id FROM jmap_uploads WHERE username='alice' ORDER BY expires,rowid LIMIT 1`).Scan(&oldest); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UploadBlob(ctx, "alice", "application/octet-stream", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetBlob(ctx, "alice", oldest); !errors.Is(err, mailstate.ErrBlobNotFound) {
		t.Fatal("oldest upload was not evicted", err)
	}
	if _, err = s.UploadBlob(ctx, "bob", "application/octet-stream", make([]byte, mailstate.MaxUploadBytes+1)); !errors.Is(err, mailstate.ErrUploadQuota) {
		t.Fatal("size limit", err)
	}
	if _, err = s.db.Exec(`UPDATE jmap_uploads SET expires=0`); err != nil {
		t.Fatal(err)
	}
	blob, err := s.UploadBlob(ctx, "alice", "message/rfc822", []byte("Subject: import\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := s.UploadBlob(ctx, "alice", "application/octet-stream", []byte("not an email"))
	if err != nil {
		t.Fatal(err)
	}
	boxes, _, err := s.MailboxSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.ImportEmails(ctx, "alice", "", map[string]mailstate.EmailImport{"bad": {BlobID: invalid.ID, MailboxID: boxes[0].ID}})
	if err != nil || r.NotCreated["bad"] != "invalidEmail" {
		t.Fatal(r, err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := s.ImportEmails(ctx, "alice", r.NewState, map[string]mailstate.EmailImport{"one": {BlobID: blob.ID, MailboxID: boxes[0].ID}})
			results <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	passed, rejected := 0, 0
	for err := range results {
		if err == nil {
			passed++
		} else if errors.Is(err, mailstate.ErrStateMismatch) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || rejected != 1 {
		t.Fatal(passed, rejected)
	}
}

func TestUploadAccountByteLimit(t *testing.T) {
	ctx := context.Background()
	raw, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	s, err := NewMailboxStore(NewIMAPAdapter(raw), filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data := make([]byte, mailstate.MaxUploadBytes)
	oldest := ""
	for i := 0; i < 11; i++ {
		blob, e := s.UploadBlob(ctx, "alice", "application/octet-stream", data)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			oldest = blob.ID
		}
	}
	if _, err = s.GetBlob(ctx, "alice", oldest); !errors.Is(err, mailstate.ErrBlobNotFound) {
		t.Fatal("byte quota did not evict oldest", err)
	}
	var count, used int64
	if err = s.db.QueryRow(`SELECT count(*),sum(length(data)) FROM jmap_uploads WHERE username='alice'`).Scan(&count, &used); err != nil || count != 10 || used != maxUploadAccountBytes {
		t.Fatal(count, used, err)
	}
}
