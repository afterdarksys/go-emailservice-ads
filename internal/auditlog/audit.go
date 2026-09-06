// Package auditlog stores verifiable append-only audit chains. An independently
// retained head hash is required to detect truncation or a full local rewrite.
package auditlog

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Record struct {
	Version  int    `json:"version" yaml:"version"`
	Sequence uint64 `json:"sequence" yaml:"sequence"`
	Time     string `json:"time" yaml:"time"`
	Actor    string `json:"actor" yaml:"actor"`
	Action   string `json:"action" yaml:"action"`
	ID       string `json:"id" yaml:"id"`
	Previous string `json:"previous" yaml:"previous"`
	Hash     string `json:"hash" yaml:"hash"`
}
type Log struct {
	mu       sync.Mutex
	path     string
	sequence uint64
	head     string
	failed   error
}

func hash(r Record) string {
	r.Hash = ""
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func Verify(reader io.Reader) (uint64, string, error) {
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 4096), 1<<20)
	var seq uint64
	head := ""
	for scan.Scan() {
		var r Record
		if err := json.Unmarshal(scan.Bytes(), &r); err != nil {
			return seq, head, fmt.Errorf("invalid audit record %d: %w", seq+1, err)
		}
		if r.Version != 1 || r.Sequence != seq+1 || r.Previous != head || r.Hash != hash(r) {
			return seq, head, fmt.Errorf("audit chain mismatch at %d", seq+1)
		}
		if _, err := time.Parse(time.RFC3339Nano, r.Time); err != nil {
			return seq, head, err
		}
		seq = r.Sequence
		head = r.Hash
	}
	return seq, head, scan.Err()
}
func Open(path string) (*Log, error) {
	l := &Log{path: path}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, e := f.Stat(); e != nil {
		return nil, e
	} else if info.Size() > 0 {
		var last [1]byte
		if _, e = f.ReadAt(last[:], info.Size()-1); e != nil {
			return nil, e
		}
		if last[0] != '\n' {
			return nil, fmt.Errorf("incomplete audit record")
		}
	}
	l.sequence, l.head, err = Verify(f)
	if err != nil {
		return nil, err
	}
	return l, nil
}
func (l *Log) Append(actor, action, id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failed != nil {
		return l.failed
	}
	if len(actor) > 4096 || len(action) > 8192 || len(id) > 4096 {
		return fmt.Errorf("audit field exceeds limit")
	}
	r := Record{Version: 1, Sequence: l.sequence + 1, Time: time.Now().UTC().Format(time.RFC3339Nano), Actor: actor, Action: action, ID: id, Previous: l.head}
	r.Hash = hash(r)
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	n, err := f.Write(raw)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		l.failed = err
		return err
	}
	d, err := os.Open(filepath.Dir(l.path))
	if err == nil {
		err = d.Sync()
		d.Close()
	}
	if err != nil {
		l.failed = err
		return err
	}
	l.sequence = r.Sequence
	l.head = r.Hash
	return nil
}
