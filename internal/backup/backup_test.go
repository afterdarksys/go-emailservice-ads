package backup

import (
	"context"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineBackupRestoreDrill(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	s, err := storage.NewMessageStore(filepath.Join(source, "mail-storage"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := s.Store(&storage.JournalEntry{Data: []byte("recover me"), Tier: "out", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "snapshot.tar.gz")
	if err = Create(context.Background(), source, archive); err == nil {
		t.Fatal("live backup accepted")
	}
	s.Close()
	if err = Create(context.Background(), source, archive); err != nil {
		t.Fatal(err)
	}
	if err = Verify(context.Background(), archive, 1<<20); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "restored")
	if err = Restore(context.Background(), archive, target, 1<<20); err != nil {
		t.Fatal(err)
	}
	recovered, err := storage.NewMessageStore(filepath.Join(target, "mail-storage"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	msg, err := recovered.Get(id)
	if err != nil || string(msg.Data) != "recover me" {
		t.Fatal("mail not recovered", err)
	}
	if err = Restore(context.Background(), archive, target, 1<<20); err == nil {
		t.Fatal("overwrote existing data")
	}
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 255
	bad := filepath.Join(root, "bad.tar.gz")
	os.WriteFile(bad, raw, 0600)
	if err = Verify(context.Background(), bad, 1<<20); err == nil {
		t.Fatal("corrupt archive accepted")
	}
	if err = Verify(context.Background(), archive, 1); err == nil {
		t.Fatal("extraction limit ignored")
	}
}
