// Package backup creates offline, checksummed snapshots of the complete data directory.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/auditlog"
	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

const manifestName = ".mailhub-backup.json"

type Manifest struct {
	Version int               `json:"version"`
	Created time.Time         `json:"created"`
	Files   map[string]string `json:"files"`
}

func Create(ctx context.Context, source, archive string) error {
	source, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	archive, err = filepath.Abs(archive)
	if err != nil {
		return err
	}
	archiveParent, err := filepath.EvalSymlinks(filepath.Dir(archive))
	if err != nil {
		return err
	}
	archive = filepath.Join(archiveParent, filepath.Base(archive))
	if inside(source, archive) {
		return fmt.Errorf("archive must be outside data directory")
	}
	if _, err = os.Lstat(archive); !os.IsNotExist(err) {
		return fmt.Errorf("archive destination already exists or inaccessible")
	}
	lock, err := os.OpenFile(filepath.Join(source, "mail-storage", ".owner.lock"), os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open spool ownership lock: %w", err)
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("stop mailhub before backup: %w", err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	f, err := os.CreateTemp(filepath.Dir(archive), ".backup-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	manifest := Manifest{Version: 1, Created: time.Now().UTC(), Files: map[string]string{}}
	err = filepath.WalkDir(source, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.Name() == ".owner.lock" {
			return nil
		}
		if rel == manifestName {
			return fmt.Errorf("reserved backup filename")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported nonregular file: %s", rel)
		}
		hdr := &tar.Header{Name: rel, Mode: 0600, Size: info.Size(), ModTime: info.ModTime(), Typeflag: tar.TypeReg}
		if err = tw.WriteHeader(hdr); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(io.MultiWriter(tw, hash), contextReader{ctx, in})
		in.Close()
		if err != nil {
			return err
		}
		manifest.Files[rel] = hex.EncodeToString(hash.Sum(nil))
		return nil
	})
	if err != nil {
		return err
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err = tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err = tw.Write(raw); err != nil {
		return err
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Link provides atomic publication without replacing another backup.
	if err = os.Link(f.Name(), archive); err != nil {
		return err
	}
	return syncDir(filepath.Dir(archive))
}
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// Restore refuses existing destinations. All validation happens in a staging
// directory before atomic installation; failed archives never replace live data.
func Restore(ctx context.Context, archive, target string, maxBytes int64) error {
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return fmt.Errorf("restore requires a nonexistent destination")
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = extract(ctx, archive, stage, maxBytes); err != nil {
		return err
	}
	if err = VerifyDatabases(ctx, stage); err != nil {
		return err
	}
	// Persist directory entries before publishing the restored tree. Sync children
	// first so a crash cannot leave a durable root pointing at missing contents.
	var dirs []string
	if err = filepath.WalkDir(stage, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err = syncDir(dirs[i]); err != nil {
			return err
		}
	}
	// Destination absence must remain an operator-enforced invariant while restoring.
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return fmt.Errorf("restore destination appeared")
	}
	if err = os.Rename(stage, target); err != nil {
		return err
	}
	return syncDir(filepath.Dir(target))
}
func Verify(ctx context.Context, archive string, maxBytes int64) error {
	stage, err := os.MkdirTemp("", "mailhub-verify-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = extract(ctx, archive, stage, maxBytes); err != nil {
		return err
	}
	return VerifyDatabases(ctx, stage)
}
func extract(ctx context.Context, archive, target string, maxBytes int64) error {
	if maxBytes <= 0 {
		return fmt.Errorf("positive extraction byte limit required")
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(contextReader{ctx, gz})
	hashes := map[string]string{}
	var manifest *Manifest
	var total int64
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if h.Typeflag != tar.TypeReg || h.Name == "" || filepath.IsAbs(h.Name) || filepath.ToSlash(filepath.Clean(h.Name)) != h.Name || strings.Contains(h.Name, "\\") {
			return fmt.Errorf("unsafe archive entry")
		}
		path := filepath.Join(target, filepath.FromSlash(h.Name))
		if !inside(target, path) || h.Name == "." {
			return fmt.Errorf("archive path escapes destination")
		}
		if h.Size < 0 || h.Size > maxBytes-total {
			return fmt.Errorf("archive exceeds extraction limit")
		}
		total += h.Size
		if h.Name == manifestName {
			if manifest != nil {
				return fmt.Errorf("duplicate manifest")
			}
			if h.Size > 64<<20 {
				return fmt.Errorf("manifest too large")
			}
			manifest = &Manifest{}
			if err = json.NewDecoder(tr).Decode(manifest); err != nil {
				return err
			}
			continue
		}
		if len(hashes) >= 1000000 {
			return fmt.Errorf("too many archive entries")
		}
		if _, ok := hashes[h.Name]; ok {
			return fmt.Errorf("duplicate archive entry")
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(io.MultiWriter(out, hash), tr)
		if err == nil {
			err = out.Sync()
		}
		out.Close()
		if err != nil {
			return err
		}
		hashes[h.Name] = hex.EncodeToString(hash.Sum(nil))
	}
	// Consume the checksum trailer but reject appended payloads and gzip bombs.
	// Our writer emits no padding after tar EOF, so any further byte is invalid.
	var extra [1]byte
	if n, e := (contextReader{ctx, gz}).Read(extra[:]); n != 0 || e != io.EOF {
		if e != nil && e != io.EOF {
			return e
		}
		return fmt.Errorf("unexpected data after backup archive")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if manifest == nil || manifest.Version != 1 || len(manifest.Files) != len(hashes) {
		return fmt.Errorf("invalid backup manifest")
	}
	for name, hash := range hashes {
		if manifest.Files[name] != hash {
			return fmt.Errorf("checksum mismatch: %s", name)
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func VerifyDatabases(ctx context.Context, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Name() == "audit-chain.jsonl" && !d.IsDir() {
			_, err := auditlog.Open(path)
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".db") {
			return nil
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			return err
		}
		defer db.Close()
		var result string
		if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
			return fmt.Errorf("database integrity %s: %w", d.Name(), err)
		}
		if result != "ok" {
			return fmt.Errorf("database integrity %s: %s", d.Name(), result)
		}
		return nil
	})
}
