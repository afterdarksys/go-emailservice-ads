package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"go.uber.org/zap"
)

func TestEmailSyncConditionalWritesAndRetention(t *testing.T) {
	ctx := context.Background()
	raw, err := NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	s, err := NewMailboxStore(NewIMAPAdapter(raw), filepath.Join(t.TempDir(), "mailbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: sync\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	_, before, err := s.EmailSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, flag := range []string{`\Seen`, `\Flagged`} {
		wg.Add(1)
		go func(flag string) {
			defer wg.Done()
			<-start
			_, err := s.SetEmailKeywords(ctx, "alice", before, map[string]mailstate.Patch{id: {Add: []string{flag}}})
			results <- err
		}(flag)
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
		t.Fatal("conditional writes were not serialized", passed, rejected)
	}
	_, checkpoint, _ := s.EmailSnapshot(ctx, "alice")
	if _, err = s.db.Exec(`CREATE TRIGGER reject_keywords BEFORE UPDATE OF flags ON message_flags BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetEmailKeywords(ctx, "alice", checkpoint, map[string]mailstate.Patch{id: {Add: []string{"new"}}}); err == nil {
		t.Fatal("write failure ignored")
	}
	_, after, _ := s.EmailSnapshot(ctx, "alice")
	if after != checkpoint {
		t.Fatal("rolled back write advanced state")
	}
	if _, err = s.db.Exec(`DROP TRIGGER reject_keywords`); err != nil {
		t.Fatal(err)
	}
	// Exercise bounded history without allocating thousands of message payloads.
	if _, err = s.db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10001) INSERT INTO email_events(username,msg_id,kind) SELECT 'alice',?,'updated' FROM n`, id); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM email_events WHERE username='alice'`).Scan(&count); err != nil || count != 10000 {
		t.Fatal("unbounded history", count, err)
	}
	if _, err = s.EmailChanges(ctx, "alice", before, 500); !errors.Is(err, mailstate.ErrCannotCalculate) {
		t.Fatal("expired state accepted", err)
	}
}

func TestEmailSyncBootstrapsExistingMailboxOnce(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = initSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO message_flags(msg_id,username,mailbox) VALUES('old','alice','INBOX')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = initEmailSync(db); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM email_events`).Scan(&count); err != nil || count != 1 {
		t.Fatal("migration duplicated existing mail", count, err)
	}
}
