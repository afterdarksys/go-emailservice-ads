// Package privacy implements offline account export and staged deletion.
package privacy

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

type Inventory struct {
	Identity    map[string]interface{}              `json:"identity"`
	Account     string                              `json:"account"`
	Email       string                              `json:"email"`
	Created     time.Time                           `json:"created"`
	Records     []storage.PersonalRecord            `json:"records"`
	Tables      map[string][]map[string]interface{} `json:"mailbox_metadata"`
	Obligations []string                            `json:"remaining_obligations"`
	Status      string                              `json:"status"`
}
type Session struct {
	cfg   *config.Config
	raw   *storage.MessageStore
	users *auth.UserRepository
	db    *sql.DB
	user  string
	email string
}

func Open(configPath, user string) (*Session, error) {
	if user == "" || strings.ContainsAny(user, "\r\n\x00") {
		return nil, fmt.Errorf("valid account required")
	}
	c, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	// Require existing stores: an inventory must never create an empty deployment.
	for _, path := range []string{filepath.Join(c.Platform.DataDir, "mail-storage", ".owner.lock"), filepath.Join(c.Platform.DataDir, "mailbox.db")} {
		if _, err = os.Stat(path); err != nil {
			return nil, err
		}
	}
	raw, err := storage.NewMessageStore(filepath.Join(c.Platform.DataDir, "mail-storage"), zap.NewNop())
	if err != nil {
		return nil, fmt.Errorf("stop mailhub before privacy operations: %w", err)
	}
	s := &Session{cfg: c, raw: raw, user: user}
	fail := func(err error) (*Session, error) { s.Close(); return nil, err }
	s.users, err = auth.NewUserRepository(c.Auth.UserDatabaseURL, zap.NewNop())
	if err != nil {
		return fail(err)
	}
	u, err := s.users.GetUser(context.Background(), user)
	if err != nil && !errors.Is(err, auth.ErrUserNotFound) {
		return fail(err)
	}
	s.email = user
	if u != nil {
		s.email = u.Email
	}
	s.db, err = sql.Open("sqlite", filepath.Join(c.Platform.DataDir, "mailbox.db"))
	if err != nil {
		return fail(err)
	}
	s.db.SetMaxOpenConns(1)
	return s, nil
}
func (s *Session) Close() {
	if s.db != nil {
		s.db.Close()
	}
	if s.users != nil {
		s.users.Close()
	}
	if s.raw != nil {
		s.raw.Close()
	}
}
func (s *Session) Inventory(ctx context.Context) (Inventory, error) {
	identity := map[string]interface{}{}
	if u, err := s.users.GetUser(ctx, s.user); err == nil {
		identity = map[string]interface{}{"username": u.Username, "email": u.Email, "enabled": u.Enabled, "scim_id": u.SCIMID, "external_id": u.ExternalID}
	} else if !errors.Is(err, auth.ErrUserNotFound) {
		return Inventory{}, err
	}
	domains, err := s.users.GetUserDomainEntitlements(ctx, s.user)
	if err != nil {
		return Inventory{}, err
	}
	identity["domains"] = domains
	quota, err := s.users.GetUserQuota(ctx, s.user)
	if err != nil {
		return Inventory{}, err
	}
	identity["quota"] = quota

	out := Inventory{Identity: identity, Account: s.user, Email: s.email, Created: time.Now().UTC(), Records: s.raw.PersonalRecords(s.user, s.email), Tables: map[string][]map[string]interface{}{}, Status: "inventoried", Obligations: []string{
		"Remove account from identity provider/LDAP/SCIM and bootstrap configuration; prevent automatic reprovisioning.",
		"Review shared mail copies and compliance records; legal holds and retention require independent disposition.",
		"Dispose of external log/search/scanner/directory records according to approved retention; local export cannot enumerate external systems.",
		"Retire or expire backups, replicas, exports and object-locked copies; replay deletion after any older restore before serving clients.",
		"Review aliases, policy files, domain entitlements, legacy Sieve suppression markers and historical metadata without explicit ownership.",
		"Audit chains and the privacy case itself retain minimal accountability data; record retention and disposal separately.",
		"Filesystem snapshots, SSD remnants and database server/WAL backups require storage-layer disposal or key retirement; logical deletion is not physical-media erasure.",
	}}
	for _, table := range []string{"message_flags", "mailbox_state", "mailbox_catalog", "mailbox_users", "email_sync", "email_events", "mailbox_sync", "mailbox_events", "jmap_snapshots", "jmap_uploads"} {
		rows, err := s.db.QueryContext(ctx, `SELECT * FROM `+table+` WHERE username=?`, s.user)
		if err != nil {
			return out, err
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return out, err
		}
		items := []map[string]interface{}{}
		for rows.Next() {
			values := make([]interface{}, len(columns))
			dest := make([]interface{}, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return out, err
			}
			m := map[string]interface{}{}
			for i, k := range columns {
				m[k] = values[i]
			}
			items = append(items, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		out.Tables[table] = items
	}
	return out, nil
}
func exclusiveOutput(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
}
func (s *Session) Export(ctx context.Context, path string) error {
	inventory, err := s.Inventory(ctx)
	if err != nil {
		return err
	}
	f, err := exclusiveOutput(path)
	if err != nil {
		return err
	}
	defer f.Close()
	z := zip.NewWriter(f)
	manifest := map[string]string{}
	put := func(name string, data []byte) error {
		w, err := z.Create(name)
		if err != nil {
			return err
		}
		if _, err = w.Write(data); err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		manifest[name] = hex.EncodeToString(sum[:])
		return nil
	}
	// Inventory JSON includes blobs and record metadata; the message files provide
	// an interoperable RFC 5322 representation as well.
	b, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	if err = put("inventory.json", b); err != nil {
		return err
	}
	for _, record := range inventory.Records {
		if err = ctx.Err(); err != nil {
			return err
		}
		if len(record.Entry.Data) == 0 {
			continue
		}
		sum := sha256.Sum256([]byte(record.Entry.MessageID))
		if err = put(fmt.Sprintf("messages/%x.eml", sum), record.Entry.Data); err != nil {
			return err
		}
	}
	script := filepath.Join(s.cfg.Platform.DataDir, "sieve", url.PathEscape(s.user)+".sieve")
	if b, err = os.ReadFile(script); err == nil {
		if err = put("sieve.sieve", b); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// Export matching local audit records without exposing unrelated audit entries.
	audit, err := os.Open(filepath.Join(s.cfg.Platform.DataDir, "mail-storage", "audit-chain.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines strings.Builder
	if audit != nil {
		scanner := bufio.NewScanner(audit)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		for scanner.Scan() {
			line := scanner.Text()
			match := strings.Contains(line, s.user) || (s.email != "" && strings.Contains(line, s.email))
			for _, r := range inventory.Records {
				match = match || strings.Contains(line, r.Entry.MessageID)
			}
			if match {
				lines.WriteString(line + "\n")
			}
		}
		err = scanner.Err()
		audit.Close()
		if err != nil {
			return err
		}
	}
	if err = put("audit-matches.jsonl", []byte(lines.String())); err != nil {
		return err
	}
	b, _ = json.MarshalIndent(manifest, "", "  ")

	// Create once and write checksum manifest explicitly.
	w, err := z.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err = w.Write(b); err != nil {
		return err
	}
	if err = z.Close(); err != nil {
		return err
	}
	return f.Sync()
}

// Delete removes active owned records; a durable case records steps and retained
// obligations. It must be retried after a partial failure before service resumes.
func (s *Session) Delete(ctx context.Context, casePath, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("deletion reason required")
	}
	u, err := s.users.GetUser(ctx, s.user)
	if err != nil && !errors.Is(err, auth.ErrUserNotFound) {
		return err
	}
	if u != nil && u.Enabled {
		return fmt.Errorf("disable account before deletion")
	}
	for _, u := range s.cfg.Auth.DefaultUsers {
		if u.Username == s.user {
			return fmt.Errorf("remove account from auth.default_users before deletion")
		}
	}
	inventory, err := s.Inventory(ctx)
	if err != nil {
		return err
	}
	// Preserve the disabled identity until the final step, allowing retries while
	// earlier phases remove data. Case files never claim external erasure.
	report := map[string]interface{}{"account": s.user, "email": s.email, "reason": reason, "created": time.Now().UTC(), "status": "in_progress", "remaining_obligations": inventory.Obligations, "retained_records": []string{}}
	f, err := exclusiveOutput(casePath)
	if err != nil {
		return err
	}
	f.Close()
	write := func() error { return atomicCase(casePath, report) }
	if err = write(); err != nil {
		return err
	}
	retained := []string{}
	for _, record := range inventory.Records {
		if err = ctx.Err(); err != nil {
			return err
		}
		e := record.Entry
		if !record.Owned || record.Protected {
			if !record.Protected && e.Tier != "mailbox" {
				recipients := []string{}
				for _, to := range e.To {
					if !strings.EqualFold(to, s.email) {
						recipients = append(recipients, to)
					}
				}
				if len(recipients) != len(e.To) {
					if err = s.raw.UpdateRecipients(e.MessageID, recipients); err != nil {
						return err
					}
				}
			}
			retained = append(retained, e.MessageID)
			continue
		}
		if err = s.raw.UpdateStatus(e.MessageID, "deleted", ""); err != nil {
			return err
		}
	}
	report["retained_records"] = retained
	report["status"] = "mail_payloads_deleted"
	if err = write(); err != nil {
		return err
	}
	if _, err = s.db.ExecContext(ctx, "PRAGMA secure_delete=ON"); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Delete messages first: triggers create events, which are then removed too.
	for _, table := range []string{"message_flags", "mailbox_catalog", "mailbox_state", "mailbox_users", "jmap_uploads", "jmap_snapshots", "email_events", "email_sync", "mailbox_events", "mailbox_sync"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE username=?`, s.user); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if _, err = s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return err
	}
	if _, err = s.db.ExecContext(ctx, "VACUUM"); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(s.cfg.Platform.DataDir, "sieve", url.PathEscape(s.user)+".sieve")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = s.raw.Compact(0); err != nil {
		return err
	}
	report["status"] = "local_mail_data_deleted_identity_disabled"
	if err = write(); err != nil {
		return err
	}
	if err = s.users.SecureDeleteUser(ctx, s.user); err != nil && !errors.Is(err, auth.ErrUserNotFound) {
		return err
	}
	report["status"] = "local_deletion_complete_external_disposition_required"
	return write()
}

func atomicCase(path string, report interface{}) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".privacy-case-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(report); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
