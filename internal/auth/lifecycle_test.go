package auth

import (
	"context"
	"errors"
	"go.uber.org/zap"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrentCreationAndStaleSCIMDeletion(t *testing.T) {
	repo, err := NewUserRepository(filepath.Join(t.TempDir(), "users.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	s := NewUserStore()
	if err = s.SetRepository(repo); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.AddUser("alice", "a-long-password", "alice@example.test")
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, ErrAccountConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal(wins.Load())
	}
	u, _ := s.GetUser("alice")
	u.Enabled = false
	if stored, _ := s.GetUser("alice"); !stored.Enabled {
		t.Fatal("caller mutated account")
	}
	for i := 0; i < 5; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.UpdateEmail("alice", "changed@example.test") }()
		go func() { defer wg.Done(); s.DeleteUser("alice") }()
		wg.Wait()
		if _, ok := s.GetUser("alice"); ok {
			t.Fatal("deleted account resurrected in memory")
		}
		if _, err := repo.GetUser(context.Background(), "alice"); !errors.Is(err, ErrUserNotFound) {
			t.Fatal("deleted account resurrected in database", err)
		}
		if err = s.AddUser("alice", "a-long-password", "alice@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.DeleteUser("alice"); err != nil {
		t.Fatal(err)
	}
	provisioned, err := s.ProvisionSCIM("alice", "alice@example.test", "external", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteSCIM(provisioned.SCIMID); err != nil {
		t.Fatal(err)
	}
	if err = s.AddUser("alice", "a-new-password", "new@example.test"); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteSCIM(provisioned.SCIMID); !errors.Is(err, ErrUserNotFound) {
		t.Fatal(err)
	}
	if _, ok := s.GetUser("alice"); !ok {
		t.Fatal("stale ID deleted replacement account")
	}
}
