package storage

import (
	"context"
	"database/sql"
	"errors"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"go.uber.org/zap"
	"path/filepath"
	"testing"
)

func strptr(s string) *string { return &s }
func TestMailboxWritesStableIdentityAndRollback(t *testing.T) {
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
	boxes, before, err := s.MailboxSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	inbox := ""
	for _, b := range boxes {
		if b.Role == "inbox" {
			inbox = b.ID
		}
	}
	r, err := s.SetMailboxes(ctx, "alice", before, map[string]mailstate.MailboxPatch{"parent": {Name: strptr("Projects")}, "child": {Name: strptr("Child"), Parent: strptr("#parent")}}, nil, nil)
	if err != nil || len(r.Created) != 2 {
		t.Fatal(r, err)
	}
	parent, child := r.Created["parent"], r.Created["child"]
	c, err := s.MailboxChanges(ctx, "alice", before, 1)
	if err != nil || len(c.Created) != 1 || !c.HasMore {
		t.Fatal(c, err)
	}
	c, err = s.MailboxChanges(ctx, "alice", c.NewState, 1)
	if err != nil || len(c.Created) != 1 || c.HasMore {
		t.Fatal(c, err)
	}
	if _, err = s.SetMailboxes(ctx, "alice", before, nil, map[string]mailstate.MailboxPatch{parent: {Name: strptr("Stale")}}, nil); !errors.Is(err, mailstate.ErrStateMismatch) {
		t.Fatal(err)
	}
	mid, err := s.StoreMessage(ctx, "alice", "Projects/Child", []byte("Subject: mailbox\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.SetMailboxes(ctx, "alice", "", nil, nil, []string{parent, child, inbox})
	if err != nil || r.NotDestroyed[parent] != "mailboxHasChild" || r.NotDestroyed[child] != "mailboxHasEmail" || r.NotDestroyed[inbox] != "forbidden" {
		t.Fatal(r, err)
	}
	_, checkpoint, _ := s.MailboxSnapshot(ctx, "alice")
	if _, err = s.db.Exec(`CREATE TRIGGER reject_sort BEFORE UPDATE OF jmap_sort_order ON mailbox_catalog BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	order := 4
	r, err = s.SetMailboxes(ctx, "alice", "", nil, map[string]mailstate.MailboxPatch{parent: {Name: strptr("Renamed"), SortOrder: &order}}, nil)
	if err != nil || r.NotUpdated[parent] != "serverFail" || r.NewState != checkpoint {
		t.Fatal("rollback", r, err)
	}
	if p, err := s.mailboxPath(ctx, "alice", child); err != nil || p != "Projects/Child" {
		t.Fatal(p, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER reject_sort`); err != nil {
		t.Fatal(err)
	}
	r, err = s.SetMailboxes(ctx, "alice", checkpoint, nil, map[string]mailstate.MailboxPatch{parent: {Name: strptr("Renamed"), SortOrder: &order}}, nil)
	if err != nil || len(r.Updated) != 1 {
		t.Fatal(r, err)
	}
	if p, err := s.mailboxPath(ctx, "alice", child); err != nil || p != "Renamed/Child" {
		t.Fatal(p, err)
	}
	messages, _, err := s.EmailSnapshot(ctx, "alice")
	if err != nil || messages[mid].MailboxID != child {
		t.Fatal(messages, err)
	}
	c, err = s.MailboxChanges(ctx, "alice", checkpoint, 500)
	if err != nil || len(c.Updated) != 2 {
		t.Fatal(c, err)
	}
	r, err = s.SetMailboxes(ctx, "bob", "", nil, map[string]mailstate.MailboxPatch{parent: {Name: strptr("Foreign")}}, []string{child})
	if err != nil || r.NotUpdated[parent] != "notFound" || r.NotDestroyed[child] != "notFound" {
		t.Fatal(r, err)
	}
	_, checkpoint, _ = s.MailboxSnapshot(ctx, "alice")
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewMailboxStore(NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.MailboxChanges(ctx, "alice", checkpoint, 500)
	if err != nil || c.NewState != checkpoint {
		t.Fatal("restart", c, err)
	}
	if err = s.DeleteFolder(ctx, "alice", "Renamed/Child"); err != nil {
		t.Fatal(err)
	}
	r, err = s.SetMailboxes(ctx, "alice", "", nil, nil, []string{parent})
	if err != nil || len(r.Destroyed) != 1 {
		t.Fatal(r, err)
	}
	r, err = s.SetMailboxes(ctx, "alice", "", map[string]mailstate.MailboxPatch{"new": {Name: strptr("Renamed")}}, nil, nil)
	if err != nil || r.Created["new"] == parent || r.Created["new"] == "" {
		t.Fatal("recreated identity", r, err)
	}
}

func TestMailboxChangesRetentionAndCountEvents(t *testing.T) {
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
	boxes, before, err := s.MailboxSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	inbox := ""
	for _, b := range boxes {
		if b.Path == "INBOX" {
			inbox = b.ID
		}
	}
	mid, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: count\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.MailboxChanges(ctx, "alice", before, 500)
	if err != nil || len(c.Updated) != 1 || c.Updated[0] != inbox {
		t.Fatal(c, err)
	}
	if _, err = s.SetEmailKeywords(ctx, "alice", "", map[string]mailstate.Patch{mid: {Add: []string{`\Seen`}}}); err != nil {
		t.Fatal(err)
	}
	boxes, _, err = s.MailboxSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range boxes {
		if b.ID == inbox && (b.Total != 1 || b.Unread != 0) {
			t.Fatal(b)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10001; i++ {
		if _, err = tx.Exec(`INSERT INTO mailbox_events(username,msg_id,kind) VALUES('alice',?,'updated')`, inbox); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MailboxChanges(ctx, "alice", before, 500); !errors.Is(err, mailstate.ErrCannotCalculate) {
		t.Fatal(err)
	}
	var n int
	if err = s.db.QueryRow(`SELECT count(*) FROM mailbox_events WHERE username='alice'`).Scan(&n); err != nil || n != 10000 {
		t.Fatal(n, err)
	}
}

func TestMailboxIdentityMigration(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = initSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO mailbox_catalog(username,mailbox) VALUES('alice','INBOX'); INSERT INTO message_flags(msg_id,username,mailbox) VALUES('old','alice','INBOX')`); err != nil {
		t.Fatal(err)
	}
	if err = initEmailSync(db); err != nil {
		t.Fatal(err)
	}
	var id string
	for i := 0; i < 2; i++ {
		if err = initMailboxSync(db); err != nil {
			t.Fatal(err)
		}
		var got, role string
		if err = db.QueryRow(`SELECT jmap_id,jmap_role FROM mailbox_catalog WHERE username='alice'`).Scan(&got, &role); err != nil || got == "" || role != "inbox" {
			t.Fatal(got, role, err)
		}
		if i == 1 && got != id {
			t.Fatal("identity changed on reopen")
		}
		id = got
	}
	var n int
	if err = db.QueryRow(`SELECT count(*) FROM mailbox_events`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM email_events WHERE kind='updated'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("email mailboxIds must refresh exactly once", n, err)
	}
}
