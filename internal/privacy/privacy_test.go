package privacy

import (
	"archive/zip"
	"context"
	"encoding/json"
	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOfflineExportDeletionIsolationAndHold(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	logger := zap.NewNop()
	raw, err := storage.NewMessageStore(filepath.Join(dir, "mail-storage"), logger)
	if err != nil {
		t.Fatal(err)
	}
	boxes, err := storage.NewMailboxStore(storage.NewIMAPAdapter(raw), filepath.Join(dir, "mailbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	alice, err := boxes.DeliverOnce(ctx, "alice-mail", "alice", "INBOX", []byte("Subject: personal\r\n\r\nalice body"))
	if err != nil {
		t.Fatal(err)
	}
	bob, err := boxes.DeliverOnce(ctx, "bob-mail", "bob", "INBOX", []byte("Subject: private\r\n\r\nbob body"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = raw.Store(&storage.JournalEntry{MessageID: "held-evidence", Tier: "compliance", Status: "compliance", From: "alice@example.test", Data: []byte("evidence"), CreatedAt: time.Now(), Metadata: map[string]string{"compliance": "true", "legal_hold": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := auth.NewUserRepository(filepath.Join(dir, "users.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveUser(ctx, &auth.User{Username: "alice", Email: "alice@example.test", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	repo.Close()
	cfg := map[string]interface{}{"platform": map[string]interface{}{"data_dir": dir}, "auth": map[string]interface{}{"user_database_url": filepath.Join(dir, "users.db")}}
	b, _ := json.Marshal(cfg)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err = os.WriteFile(configPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	if locked, e := Open(configPath, "alice"); e == nil {
		locked.Close()
		t.Fatal("opened live spool")
	}
	boxes.Close()
	raw.Close()
	s, err := Open(configPath, "alice")
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := s.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Records) != 2 {
		t.Fatal(inventory.Records)
	}
	archive := filepath.Join(t.TempDir(), "export.zip")
	if err = s.Export(ctx, archive); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) < 3 {
		t.Fatal(z.File)
	}
	z.Close()
	if err = s.Delete(ctx, filepath.Join(t.TempDir(), "case.json"), "approved test deletion"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.raw.Get(alice); err == nil {
		t.Fatal("owned payload retained")
	}
	if _, err = s.raw.Get(bob); err != nil {
		t.Fatal("other mailbox deleted", err)
	}
	if _, err = s.raw.Get("held-evidence"); err != nil {
		t.Fatal("held evidence deleted", err)
	}
	var count int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM message_flags WHERE username='alice'").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	s.Close()
	// A repeat operation after identity deletion remains possible for verification.
	s, err = Open(configPath, "alice")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.raw.Get(alice); err == nil {
		t.Fatal("deleted mail resurrected")
	}
}
