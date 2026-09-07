package ha

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDRBDFailClosed(t *testing.T) {
	good := `[{"name":"mailhub","role":"Primary","devices":[{"minor":100,"disk-state":"UpToDate","quorum":true}]}]`
	mount := `{"filesystems":[{"target":"/srv/mailhub","source":"/dev/drbd100","options":"rw,relatime"}]}`
	if _, e := DRBDProof("mailhub", "/srv/mailhub", "/dev/drbd100", []byte(good), []byte(mount)); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{`[]`, `[{"name":"mailhub","role":"Secondary","devices":[{"minor":100,"disk-state":"UpToDate","quorum":true}]}]`, `[{"name":"mailhub","role":"Primary","devices":[{"minor":100,"disk-state":"UpToDate"}]}]`} {
		if _, e := DRBDProof("mailhub", "/srv/mailhub", "/dev/drbd100", []byte(bad), []byte(mount)); e == nil {
			t.Fatal("unsafe proof accepted")
		}
	}
	if _, e := DRBDProof("mailhub", "/srv/mailhub", "/dev/wrong", []byte(good), []byte(mount)); e == nil {
		t.Fatal("wrong mount accepted")
	}
}
func TestGuardRuntimeOwnershipLoss(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "check")
	proof := `{"volume":"/srv/mailhub","primary":true,"up_to_date":true,"quorum":true,"mounted":true}`
	if e := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '"+proof+"'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	g, e := New(Config{Enabled: true, Volume: "/srv/mailhub", CheckCommand: []string{script}})
	if e != nil {
		t.Fatal(e)
	}
	if e = g.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0700)
	if e = g.Check(context.Background()); e == nil || g.Status().Safe {
		t.Fatal("ownership loss ignored")
	}
	if Inside("/srv/mailhub", "/srv/mailhub-other/users.db") {
		t.Fatal("path escape accepted")
	}
}

func TestReplicatedPathsRejectSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if e := os.Symlink(outside, filepath.Join(root, "escape")); e != nil {
		t.Fatal(e)
	}
	if e := ValidatePaths(root, filepath.Join(root, "new", "users.db")); e != nil {
		t.Fatal(e)
	}
	if e := ValidatePaths(root, filepath.Join(root, "escape", "users.db")); e == nil {
		t.Fatal("unreplicated database accepted")
	}
}
