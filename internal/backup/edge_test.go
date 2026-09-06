package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectsTrailingPayloadAndTraversal(t *testing.T) {
	for _, name := range []string{"trailer", "../escape", "/absolute", "nested/../../escape"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "bad.gz")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(f)
			tw := tar.NewWriter(gz)
			raw := []byte(`{"version":1,"files":{}}`)
			entry := name
			if name == "trailer" {
				entry = manifestName
			}
			if err := tw.WriteHeader(&tar.Header{Name: entry, Mode: 0600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(raw); err != nil {
				t.Fatal(err)
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if name == "trailer" {
				gz.Write([]byte(strings.Repeat("x", 1<<20)))
			}
			gz.Close()
			f.Close()
			target := filepath.Join(root, "restore")
			if err := Restore(context.Background(), path, target, 1024); err == nil {
				t.Fatal("unsafe archive restored")
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("failed restore published destination")
			}
		})
	}
}

func TestBackupRejectsArchiveInsideSourceViaSymlink(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "data")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	err := Create(context.Background(), source, filepath.Join(alias, "backup.gz"))
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("nested archive: %v", err)
	}
}
