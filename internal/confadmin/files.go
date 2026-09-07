package confadmin

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go.uber.org/zap/zapcore"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const MaxDocument = 8 << 20

func Read(path string) ([]byte, error) {
	li, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !li.Mode().IsRegular() {
		return nil, fmt.Errorf("regular file required")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("regular file required")
	}
	b, e := io.ReadAll(io.LimitReader(f, MaxDocument+1))
	if len(b) > MaxDocument {
		return nil, fmt.Errorf("document exceeds 8 MiB")
	}
	return b, e
}
func Parse(path string, b []byte) (*yaml.Node, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" && ext != ".json" {
		return nil, fmt.Errorf("supported documents: .json, .yml, .yaml")
	}
	if ext == ".json" && !json.Valid(b) {
		return nil, fmt.Errorf("invalid JSON")
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var n yaml.Node
	if e := dec.Decode(&n); e != nil {
		return nil, e
	}
	if len(n.Content) != 1 {
		return nil, fmt.Errorf("nonempty document required")
	}
	var extra yaml.Node
	if dec.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("exactly one document required")
	}
	visits := 0
	var check func(*yaml.Node, int) error
	check = func(n *yaml.Node, depth int) error {
		visits++
		if visits > 100000 {
			return fmt.Errorf("document alias expansion exceeds limit")
		}
		if depth > 64 {
			return fmt.Errorf("document nesting or alias cycle exceeds limit")
		}
		if n.Kind == yaml.AliasNode {
			return check(n.Alias, depth+1)
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
					return fmt.Errorf("duplicate or non-string mapping key")
				}
				seen[k.Value] = true
			}
		}
		for _, c := range n.Content {
			if e := check(c, depth+1); e != nil {
				return e
			}
		}
		return nil
	}
	if e := check(&n, 0); e != nil {
		return nil, e
	}
	return &n, nil
}
func Format(path string, b []byte) ([]byte, error) {
	n, e := Parse(path, b)
	if e != nil {
		return nil, e
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		var out bytes.Buffer
		if e = json.Indent(&out, b, "", "  "); e != nil {
			return nil, e
		}
		return append(out.Bytes(), '\n'), nil
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if e = enc.Encode(n); e != nil {
		return nil, e
	}
	enc.Close()
	return out.Bytes(), nil
}
func Redacted(n *yaml.Node) []byte {
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		n.HeadComment = ""
		n.LineComment = ""
		n.FootComment = ""
		if n.Kind == yaml.MappingNode {
			for i := 0; i < len(n.Content); i += 2 {
				k := strings.ToLower(n.Content[i].Value)
				if sensitive(k) {
					n.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "[REDACTED]"}
				}
			}
		}
		if n.Kind == yaml.ScalarNode && strings.Contains(n.Value, "://") && strings.Contains(n.Value, "@") {
			n.Value = "[REDACTED CONNECTION STRING]"
		}
		if n.Kind == yaml.AliasNode {
			n.Kind = yaml.ScalarNode
			n.Tag = "!!str"
			n.Value = "[ALIAS]"
			n.Alias = nil
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(n)
	b, _ := yaml.Marshal(n)
	return b
}
func sensitive(k string) bool {
	return strings.Contains(k, "password") || strings.Contains(k, "secret") || strings.Contains(k, "token") || k == "key" || strings.Contains(k, "private_key") || k == "script" || k == "metadata"
}
func Set(path string, b []byte, pointer string, value []byte) ([]byte, error) {
	n, e := Parse(path, b)
	if e != nil {
		return nil, e
	}
	if pointer == "" || !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("JSON pointer such as /server/max_recipients required")
	}
	var v yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(value))
	if e = dec.Decode(&v); e != nil || len(v.Content) != 1 {
		return nil, fmt.Errorf("one YAML/JSON value required")
	}
	var extra yaml.Node
	if dec.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("one value required")
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		raw := bytes.TrimSpace(value)
		if !json.Valid(raw) {
			var decoded interface{}
			if e := v.Decode(&decoded); e != nil {
				return nil, e
			}
			raw, e = json.Marshal(decoded)
			if e != nil {
				return nil, e
			}
		}
		next, e := setJSON(b, strings.Split(pointer[1:], "/"), raw)
		if e != nil {
			return nil, e
		}
		return Format(path, next)
	}
	parts := strings.Split(pointer[1:], "/")
	current := n.Content[0]
	for pos, part := range parts {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		last := pos == len(parts)-1
		switch current.Kind {
		case yaml.MappingNode:
			found := -1
			for i := 0; i < len(current.Content); i += 2 {
				if current.Content[i].Value == part {
					found = i + 1
					break
				}
			}
			if found < 0 {
				if !last {
					return nil, fmt.Errorf("pointer parent does not exist")
				}
				current.Content = append(current.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: part}, v.Content[0])
				break
			}
			if last {
				current.Content[found] = v.Content[0]
			} else {
				current = current.Content[found]
			}
		case yaml.SequenceNode:
			i, e := strconv.Atoi(part)
			if e != nil || i < 0 || i >= len(current.Content) {
				return nil, fmt.Errorf("invalid array index")
			}
			if last {
				current.Content[i] = v.Content[0]
			} else {
				current = current.Content[i]
			}
		default:
			return nil, fmt.Errorf("pointer traverses scalar or alias")
		}
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		var data interface{}
		if e = n.Decode(&data); e != nil {
			return nil, e
		}
		return json.MarshalIndent(data, "", "  ")
	}
	return yaml.Marshal(n)
}
func ValidateConfig(path string, tlsFiles bool) error {
	c, e := config.LoadConfig(path)
	if e != nil {
		return e
	}
	if e = c.ValidateRuntimeSettings(); e != nil {
		return e
	}
	if _, e = zapcore.ParseLevel(c.Logging.Level); e != nil {
		return e
	}
	if tlsFiles {
		return CheckTLS(c)
	}
	return nil
}

// Replace serializes utility writers, rejects symlinks/concurrent changes,
// validates a candidate, saves a durable private backup, then atomically replaces.
func Replace(path string, old, next []byte, validate func(string) error) (string, error) {
	lock, e := os.OpenFile(path+".gemsads.lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return "", e
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return "", e
	}
	info, e := os.Lstat(path)
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to replace symlink or non-regular file")
	}
	current, e := Read(path)
	if e != nil {
		return "", e
	}
	if sha256.Sum256(current) != sha256.Sum256(old) {
		return "", fmt.Errorf("file changed since inspection")
	}
	tmp, e := os.CreateTemp(filepath.Dir(path), ".gemsads-*")
	if e != nil {
		return "", e
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, e = tmp.Write(next); e == nil {
		e = tmp.Sync()
	}
	closeErr := tmp.Close()
	if e != nil {
		return "", e
	}
	if closeErr != nil {
		return "", closeErr
	}
	if validate != nil {
		if e = validate(name); e != nil {
			return "", e
		}
	}
	backup := path + ".bak." + time.Now().UTC().Format("20060102T150405.000000000")
	if e = Create(backup, old); e != nil {
		return "", e
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		ti, err := os.Stat(name)
		if err != nil {
			return backup, err
		}
		ts := ti.Sys().(*syscall.Stat_t)
		if stat.Uid != ts.Uid || stat.Gid != ts.Gid {
			if e = os.Chown(name, int(stat.Uid), int(stat.Gid)); e != nil {
				return backup, e
			}
		}
	}
	if e = os.Chmod(name, info.Mode().Perm()); e != nil {
		return backup, e
	}
	current, e = Read(path)
	if e != nil {
		return backup, e
	}
	if sha256.Sum256(current) != sha256.Sum256(old) {
		return backup, fmt.Errorf("file changed during validation")
	}
	if e = os.Rename(name, path); e != nil {
		return backup, e
	}
	return backup, syncDir(filepath.Dir(path))
}
func Create(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return syncDir(filepath.Dir(path))
}
func syncDir(path string) error {
	d, e := os.Open(path)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

// Preserve untouched JSON tokens, including numbers beyond float64 precision.
func setJSON(doc json.RawMessage, parts []string, value json.RawMessage) (json.RawMessage, error) {
	if len(parts) == 0 {
		return value, nil
	}
	part := strings.ReplaceAll(strings.ReplaceAll(parts[0], "~1", "/"), "~0", "~")
	trim := bytes.TrimSpace(doc)
	if len(trim) > 0 && trim[0] == '{' {
		var m map[string]json.RawMessage
		if e := json.Unmarshal(doc, &m); e != nil {
			return nil, e
		}
		child, ok := m[part]
		if !ok && len(parts) > 1 {
			return nil, fmt.Errorf("pointer parent does not exist")
		}
		next, e := setJSON(child, parts[1:], value)
		if e != nil {
			return nil, e
		}
		m[part] = next
		return json.Marshal(m)
	}
	if len(trim) > 0 && trim[0] == '[' {
		var a []json.RawMessage
		if e := json.Unmarshal(doc, &a); e != nil {
			return nil, e
		}
		i, e := strconv.Atoi(part)
		if e != nil || i < 0 || i >= len(a) {
			return nil, fmt.Errorf("invalid array index")
		}
		a[i], e = setJSON(a[i], parts[1:], value)
		if e != nil {
			return nil, e
		}
		return json.Marshal(a)
	}
	return nil, fmt.Errorf("pointer traverses scalar")
}
