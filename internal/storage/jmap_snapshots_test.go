package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"go.uber.org/zap"
)

func TestQueryHistoryBoundedOwnerScopedAndRestarted(t *testing.T) {
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
	for i := 0; i < 65; i++ {
		if err = s.SaveSnapshot(ctx, "alice", "Email", "filter", fmt.Sprint(i), []string{fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.LoadSnapshot(ctx, "alice", "Email", "filter", "0"); err != mailstate.ErrCannotCalculate {
		t.Fatal("history unbounded", err)
	}
	if err = s.SaveSnapshot(ctx, "alice", "Email", "filter", "1", []string{"1"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewMailboxStore(NewIMAPAdapter(raw), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if ids, err := s.LoadSnapshot(ctx, "alice", "Email", "filter", "1"); err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	for _, tc := range []struct{ user, kind, sig string }{{"bob", "Email", "filter"}, {"alice", "EmailSubmission", "filter"}, {"alice", "Email", "other"}} {
		if _, err = s.LoadSnapshot(ctx, tc.user, tc.kind, tc.sig, "1"); err != mailstate.ErrCannotCalculate {
			t.Fatal(tc, err)
		}
	}
	if err = s.SaveSnapshot(ctx, "alice", "Email", "filter", "oversize", make([]string, 10001)); err != mailstate.ErrCannotCalculate {
		t.Fatal(err)
	}
}
