package confadmin

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/failover"
)

func put(t *testing.T, p, s string) {
	t.Helper()
	if e := os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestDocumentsAndAtomicValidation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	put(t, p, config.DefaultDocument)
	if e := ValidateConfig(p, false); e != nil {
		t.Fatal(e)
	}
	old, _ := Read(p)
	next, e := Set(p, old, "/server/require_auth", []byte("false"))
	if e != nil {
		t.Fatal(e)
	}
	// Invalid required-auth/TLS combinations and unknown keys must not be saved.
	next, e = Set(p, next, "/unknown_setting", []byte("true"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Replace(p, old, next, func(c string) error { return ValidateConfig(c, false) }); e == nil {
		t.Fatal("invalid config written")
	}
	got, _ := Read(p)
	if !bytes.Equal(got, old) {
		t.Fatal("failed validation changed original")
	}
	next, e = Set(p, old, "/logging/level", []byte("info"))
	if e != nil {
		t.Fatal(e)
	}
	backup, e := Replace(p, old, next, func(c string) error { return ValidateConfig(c, false) })
	if e != nil {
		t.Fatal(e)
	}
	b, _ := Read(backup)
	if !bytes.Equal(b, old) {
		t.Fatal("wrong backup")
	}
	st, _ := os.Stat(backup)
	if st.Mode().Perm() != 0600 {
		t.Fatal("backup permissions")
	}
	if _, e = Replace(p, old, next, nil); e == nil {
		t.Fatal("stale writer accepted")
	}
	link := filepath.Join(dir, "symlink.yaml")
	if e = os.Symlink(p, link); e != nil {
		t.Fatal(e)
	}
	if _, e = Read(link); e == nil {
		t.Fatal("symlink accepted")
	}
	for _, bad := range []string{"", "a: 1\na: 2", "a: 1\n---\nb: 2", "a: &x [*x]"} {
		if _, e = Parse("x.yml", []byte(bad)); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	n, e := Parse("x.yml", []byte("# secret comment\npassword: secret\napi_keys:\n - key: bearer\nurl: postgres://user:pass@host/db\nnormal: visible\n"))
	if e != nil {
		t.Fatal(e)
	}
	view := string(Redacted(n))
	for _, secret := range []string{"secret", "bearer", "user:pass"} {
		if strings.Contains(view, secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	edited, err := Set("x.json", []byte(`{"n":123456789012345678901234567890,"a":1}`), "/a", []byte("2"))
	if err != nil || !bytes.Contains(edited, []byte("123456789012345678901234567890")) {
		t.Fatal("JSON edit changed unrelated number", err)
	}
	numeric := []byte(`{"n":123456789012345678901234567890}`)
	formatted, e := Format("x.JSON", numeric)
	if e != nil || !bytes.Contains(formatted, []byte("123456789012345678901234567890")) {
		t.Fatal("number changed", e)
	}
}
func TestConfigCommandDryRun(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	put(t, p, config.DefaultDocument)
	value := filepath.Join(dir, "value")
	put(t, value, "info")
	old, _ := Read(p)
	run := func(extra ...string) error {
		cmd := NewCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(append([]string{"--config", p, "config", "set", "/logging/level", "--value-file", value}, extra...))
		return cmd.Execute()
	}
	if e := run(); e != nil {
		t.Fatal(e)
	}
	b, _ := Read(p)
	if !bytes.Equal(old, b) {
		t.Fatal("dry run wrote")
	}
	if e := run("--apply"); e != nil {
		t.Fatal(e)
	}
	b, _ = Read(p)
	if bytes.Equal(old, b) {
		t.Fatal("apply did not write")
	}
}
func TestSQLiteBackupIncludesWALAndTransactionalEdit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p := filepath.Join(dir, "source.db")
	if e := CreateDB(ctx, p, "CREATE TABLE parent(id INTEGER PRIMARY KEY); CREATE TABLE child(id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id), password TEXT);"); e != nil {
		t.Fatal(e)
	}
	db, e := OpenDB(p, true)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("PRAGMA journal_mode=WAL"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("PRAGMA wal_autocheckpoint=0"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO parent VALUES(1)"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO child VALUES(1,1,'private')"); e != nil {
		t.Fatal(e)
	}
	backup := filepath.Join(dir, "backup.db")
	if e = BackupDB(ctx, p, backup); e != nil {
		t.Fatal(e)
	}
	snap, e := OpenDB(backup, false)
	if e != nil {
		t.Fatal(e)
	}
	defer snap.Close()
	var count int
	if e = snap.QueryRow("SELECT count(*) FROM child").Scan(&count); e != nil || count != 1 {
		t.Fatal("WAL row missing", e, count)
	}
	if e = EditDB(ctx, p, "UPDATE child SET parent_id=999"); e == nil {
		t.Fatal("invalid FK committed")
	}
	if e = EditDB(ctx, p, "UPDATE child SET password='new'"); e != nil {
		t.Fatal(e)
	}
	rows, e := DBRows(ctx, db, "SELECT * FROM child")
	if e != nil || rows[0]["password"] != "[REDACTED]" || rows[0]["parent_id"] != int64(1) {
		t.Fatal(rows, e)
	}
	for _, sql := range []string{"ATTACH 'x' AS other", "DELETE FROM child; DROP TABLE child", "PRAGMA writable_schema=1"} {
		if e = EditDB(ctx, p, sql); e == nil {
			t.Fatal("unsafe statement accepted", sql)
		}
	}
	if _, e = snap.Exec("DELETE FROM child"); e == nil {
		t.Fatal("inspection connection writable")
	}
	if e = BackupDB(ctx, p, backup); e == nil {
		t.Fatal("backup overwritten")
	}
	if e = MaintainDB(ctx, p); e != nil {
		t.Fatal(e)
	}
	bad := filepath.Join(dir, "corrupt.db")
	put(t, bad, "not SQLite")
	if e = MaintainDB(ctx, bad); e == nil {
		t.Fatal("corruption reported repaired")
	}
}
func TestOfflineOwnershipAndScope(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	if e := os.MkdirAll(filepath.Join(data, "mail-storage"), 0700); e != nil {
		t.Fatal(e)
	}
	cfg := filepath.Join(dir, "config.yaml")
	put(t, cfg, config.DefaultDocument+"\nplatform:\n  data_dir: "+data+"\n")
	owner, e := failover.Acquire(filepath.Join(data, "mail-storage", ".owner.lock"))
	if e != nil {
		t.Fatal(e)
	}
	if lock, e := Offline(cfg); e == nil {
		lock.Close()
		t.Fatal("live deployment accepted")
	}
	owner.Close()
	lock, e := Offline(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	inside := filepath.Join(data, "test.db")
	put(t, inside, "")
	if e = CheckManagedDB(cfg, inside); e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(dir, "other.db")
	put(t, outside, "")
	if e = CheckManagedDB(cfg, outside); e == nil {
		t.Fatal("unprotected DB accepted")
	}
}
func TestRelayOutcomesNeverSendData(t *testing.T) {
	for _, tc := range []struct {
		reply, outcome string
		wantErr        bool
	}{{"550 relay denied", "rejected", false}, {"450 try later", "inconclusive", true}, {"250 accepted", "possible_relay", false}, {"drop", "inconclusive", true}} {
		t.Run(tc.reply, func(t *testing.T) {
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer listener.Close()
			done := make(chan bool, 1)
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					done <- false
					return
				}
				defer conn.Close()
				fmt.Fprint(conn, "220 test\r\n")
				scan := bufio.NewScanner(conn)
				data := false
				for scan.Scan() {
					line := scan.Text()
					switch {
					case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RSET"):
						fmt.Fprint(conn, "250 ok\r\n")
					case strings.HasPrefix(line, "RCPT"):
						if tc.reply == "drop" {
							done <- data
							return
						}
						fmt.Fprintf(conn, "%s\r\n", tc.reply)
					case strings.HasPrefix(line, "DATA"):
						data = true
						fmt.Fprint(conn, "550 no data\r\n")
					}
				}
				done <- data
			}()
			result, e := RelayCheck(context.Background(), listener.Addr().String(), "sender@outside.invalid", "recipient@external.invalid", "", false)
			if (e != nil) != tc.wantErr || result.Outcome != tc.outcome {
				t.Fatal(result, e)
			}
			if <-done {
				t.Fatal("probe sent DATA")
			}
		})
	}
}

func TestEditorRepairsMalformedConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	broken := "server: [\n"
	put(t, path, broken)
	candidate := filepath.Join(dir, "candidate")
	put(t, candidate, config.DefaultDocument)
	t.Setenv("GEMSADS_TEST_REPAIRED_CONFIG", candidate)
	editor := filepath.Join(dir, "editor")
	put(t, editor, "#!/bin/sh\ncp \"$GEMSADS_TEST_REPAIRED_CONFIG\" \"$1\"\n")
	if e := os.Chmod(editor, 0700); e != nil {
		t.Fatal(e)
	}
	run := func() error {
		cmd := NewCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--config", path, "config", "edit", "--editor", editor, "--apply"})
		return cmd.Execute()
	}
	if e := run(); e != nil {
		t.Fatal(e)
	}
	if e := ValidateConfig(path, false); e != nil {
		t.Fatal(e)
	}
	saved, _ := Read(path)
	put(t, candidate, broken)
	if e := run(); e == nil {
		t.Fatal("invalid edited candidate accepted")
	}
	after, _ := Read(path)
	if !bytes.Equal(saved, after) {
		t.Fatal("invalid candidate replaced working config")
	}
	backups, e := filepath.Glob(path + ".bak.*")
	if e != nil || len(backups) != 1 {
		t.Fatal(backups, e)
	}
	original, _ := Read(backups[0])
	if string(original) != broken {
		t.Fatal("repair did not preserve original")
	}
}
