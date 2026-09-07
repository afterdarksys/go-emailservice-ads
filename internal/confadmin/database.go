package confadmin

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/failover"
	_ "modernc.org/sqlite"
)

func OpenDB(path string, write bool) (*sql.DB, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("regular SQLite file required")
	}
	absolute, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	u := url.URL{Scheme: "file", Path: absolute}
	q := u.Query()
	q.Set("mode", "ro")
	if write {
		q.Set("mode", "rw")
	}
	q.Add("_pragma", "busy_timeout(3000)")
	q.Add("_pragma", "foreign_keys(1)")
	if !write {
		q.Add("_pragma", "query_only(1)")
	}
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if e = db.Ping(); e != nil {
		db.Close()
		return nil, e
	}
	var version int
	if e = db.QueryRow("PRAGMA schema_version").Scan(&version); e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}
func Offline(configPath string) (*os.File, error) {
	c, e := config.LoadConfig(configPath)
	if e != nil {
		return nil, e
	}
	path := filepath.Join(c.Platform.DataDir, "mail-storage", ".owner.lock")
	if _, e = os.Stat(path); e != nil {
		return nil, fmt.Errorf("existing deployment ownership lock required: %w", e)
	}
	f, e := failover.Acquire(path)
	if e != nil {
		return nil, fmt.Errorf("stop mailhub before maintenance: %w", e)
	}
	return f, nil
}
func DBRows(ctx context.Context, db *sql.DB, query string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, e := db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	columns, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	out := []map[string]interface{}{}
	total := 0
	for rows.Next() {
		if len(out) >= 1000 {
			return nil, fmt.Errorf("result exceeds 1000 rows; narrow the query")
		}
		v := make([]interface{}, len(columns))
		p := make([]interface{}, len(columns))
		for i := range v {
			p[i] = &v[i]
		}
		if e = rows.Scan(p...); e != nil {
			return nil, e
		}
		item := map[string]interface{}{}
		for i, k := range columns {
			if sensitive(strings.ToLower(k)) {
				item[k] = "[REDACTED]"
				continue
			}
			if b, ok := v[i].([]byte); ok {
				total += len(b)
			}
			if s, ok := v[i].(string); ok {
				total += len(s)
			}
			if total > MaxDocument {
				return nil, fmt.Errorf("result exceeds 8 MiB")
			}
			item[k] = v[i]
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func CheckDB(ctx context.Context, db *sql.DB) error {
	rows, e := db.QueryContext(ctx, "PRAGMA integrity_check")
	if e != nil {
		return e
	}
	for rows.Next() {
		var result string
		if e = rows.Scan(&result); e != nil {
			rows.Close()
			return e
		}
		if result != "ok" {
			rows.Close()
			return fmt.Errorf("SQLite integrity failure: %s", result)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	rows, e = db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("foreign-key violations found")
	}
	return rows.Err()
}

// Backup uses SQLite's consistent snapshot mechanism, including committed WAL.
func BackupDB(ctx context.Context, path, output string) error {
	if _, e := os.Lstat(output); !os.IsNotExist(e) {
		return fmt.Errorf("new backup path required")
	}
	db, e := OpenDB(path, true)
	if e != nil {
		return e
	}
	defer db.Close()
	// A private placeholder prevents permissive default creation modes.
	f, e := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	f.Close()
	if _, e = db.ExecContext(ctx, "VACUUM INTO ?", output); e != nil {
		return e
	}
	check, e := OpenDB(output, false)
	if e != nil {
		return e
	}
	e = CheckDB(ctx, check)
	check.Close()
	if e != nil {
		return e
	}
	f, e = os.OpenFile(output, os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	e = f.Sync()
	f.Close()
	if e != nil {
		return e
	}
	return syncDir(filepath.Dir(output))
}
func EditDB(ctx context.Context, path, statement string) error {
	statement = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(statement), ";"))
	upper := strings.ToUpper(statement)
	if strings.Contains(statement, ";") || !(strings.HasPrefix(upper, "UPDATE ") || strings.HasPrefix(upper, "INSERT ") || strings.HasPrefix(upper, "DELETE ")) {
		return fmt.Errorf("one INSERT, UPDATE or DELETE statement required; schema changes and attached databases are excluded")
	}
	db, e := OpenDB(path, true)
	if e != nil {
		return e
	}
	defer db.Close()
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, statement); e != nil {
		return e
	}
	rows, e := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	bad := rows.Next()
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if bad {
		return fmt.Errorf("foreign-key violation; edit rolled back")
	}
	return tx.Commit()
}
func MaintainDB(ctx context.Context, path string) error {
	db, e := OpenDB(path, true)
	if e != nil {
		return e
	}
	defer db.Close()
	if e = CheckDB(ctx, db); e != nil {
		return fmt.Errorf("automatic repair refused; preserve evidence and restore a verified backup: %w", e)
	}
	if _, e = db.ExecContext(ctx, "REINDEX"); e != nil {
		return e
	}
	if _, e = db.ExecContext(ctx, "VACUUM"); e != nil {
		return e
	}
	return CheckDB(ctx, db)
}

// CheckManagedDB ensures the acquired deployment lock protects the target tree.
// External SQLite account stores require their own operator-coordinated workflow.
func CheckManagedDB(configPath, path string) error {
	c, e := config.LoadConfig(configPath)
	if e != nil {
		return e
	}
	root, e := filepath.EvalSymlinks(c.Platform.DataDir)
	if e != nil {
		return e
	}
	root, e = filepath.Abs(root)
	if e != nil {
		return e
	}
	target, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	target, e = filepath.Abs(target)
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(root, target)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("mutation target must be inside this deployment's data_dir")
	}
	return nil
}
func CreateDB(ctx context.Context, path, schema string) error {
	statements := strings.Split(schema, ";")
	count := 0
	for _, s := range statements {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		u := strings.ToUpper(s)
		if !(strings.HasPrefix(u, "CREATE TABLE ") || strings.HasPrefix(u, "CREATE INDEX ") || strings.HasPrefix(u, "CREATE UNIQUE INDEX ")) {
			return fmt.Errorf("only CREATE TABLE and CREATE INDEX statements are supported")
		}
		count++
	}
	if count == 0 {
		return fmt.Errorf("schema is empty")
	}
	if e := Create(path, nil); e != nil {
		return e
	}
	db, e := OpenDB(path, true)
	if e != nil {
		return e
	}
	defer db.Close()
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, s := range statements {
		if strings.TrimSpace(s) == "" {
			continue
		}
		if _, e = tx.ExecContext(ctx, s); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return CheckDB(ctx, db)
}
