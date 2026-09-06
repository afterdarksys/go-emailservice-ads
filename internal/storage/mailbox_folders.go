package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
)

func validFlags(flags []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, f := range flags {
		f = goimap.CanonicalFlag(f)
		if f == goimap.RecentFlag {
			continue
		}
		if f == "" {
			return nil, fmt.Errorf("empty flag")
		}
		for i, c := range f {
			if c <= 32 || c >= 127 || strings.ContainsRune("(){%*\" ]", c) || (c == '\\' && i != 0) {
				return nil, fmt.Errorf("invalid flag")
			}
		}
		if strings.HasPrefix(f, "\\") && f != goimap.SeenFlag && f != goimap.AnsweredFlag && f != goimap.FlaggedFlag && f != goimap.DeletedFlag && f != goimap.DraftFlag {
			return nil, fmt.Errorf("unsupported system flag")
		}
		if !seen[f] {
			out = append(out, f)
			seen[f] = true
		}
	}
	return out, nil
}

// Bootstrap only once. Deleted standard folders must not reappear on login.
func (s *MailboxStore) ensureUser(ctx context.Context, user string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO mailbox_users(username) VALUES(?)`, user)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		for _, name := range []string{"INBOX", "Sent", "Drafts", "Trash", "Spam", "Junk"} {
			if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO mailbox_catalog(username,mailbox) VALUES(?,?)`, user, name); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO mailbox_catalog(username,mailbox) SELECT username,mailbox FROM mailbox_state WHERE username=?`, user); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *MailboxStore) folderExists(ctx context.Context, user, name string) error {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM mailbox_catalog WHERE username=? AND mailbox=?`, user, name).Scan(&n)
	if err == sql.ErrNoRows {
		return backend.ErrNoSuchMailbox
	}
	return err
}
func (s *MailboxStore) ListFolders(ctx context.Context, user string, subscribed bool) ([]string, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	if err := s.ensureUser(ctx, user); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT mailbox FROM mailbox_catalog WHERE username=? AND (?=0 OR subscribed=1) ORDER BY mailbox`, user, boolInt(subscribed))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
func (s *MailboxStore) createFolder(ctx context.Context, user, name string, allowExisting bool) error {
	var err error
	name, err = imap.NormalizeMailbox(name)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	parts := strings.Split(name, "/")
	for i := range parts {
		current := strings.Join(parts[:i+1], "/")
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO mailbox_catalog(username,mailbox,subscribed) VALUES(?,?,0)`, user, current)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 && i == len(parts)-1 && !allowExisting {
			return fmt.Errorf("mailbox already exists")
		}
		if n > 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO mailbox_state(username,mailbox,uidvalidity,uidnext) VALUES(?,?,?,1) ON CONFLICT(username,mailbox) DO UPDATE SET uidvalidity=uidvalidity+1,uidnext=1`, user, current, time.Now().Unix())
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (s *MailboxStore) CreateFolder(ctx context.Context, user, name string) error {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	if err := s.ensureUser(ctx, user); err != nil {
		return err
	}
	return s.createFolder(ctx, user, name, false)
}
func (s *MailboxStore) SubscribeFolder(ctx context.Context, user, name string, subscribed bool) error {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	var err error
	name, err = imap.NormalizeMailbox(name)
	if err != nil {
		return err
	}
	if err = s.ensureUser(ctx, user); err != nil {
		return err
	}
	if err = s.folderExists(ctx, user, name); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE mailbox_catalog SET subscribed=? WHERE username=? AND mailbox=?`, boolInt(subscribed), user, name)
	return err
}
func (s *MailboxStore) DeleteFolder(ctx context.Context, user, name string) error {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	var err error
	name, err = imap.NormalizeMailbox(name)
	if err != nil {
		return err
	}
	if name == "INBOX" {
		return fmt.Errorf("cannot delete INBOX")
	}
	if err = s.ensureUser(ctx, user); err != nil {
		return err
	}
	if err = s.folderExists(ctx, user, name); err != nil {
		return err
	}
	var children int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM mailbox_catalog WHERE username=? AND substr(mailbox,1,?)=?`, user, len(name)+1, name+"/").Scan(&children); err != nil {
		return err
	}
	if children > 0 {
		return fmt.Errorf("delete child mailboxes first")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE message_flags SET expunged=1 WHERE username=? AND mailbox=?`, user, name); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM mailbox_catalog WHERE username=? AND mailbox=?`, user, name); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.publish(&backend.StatusUpdate{Update: backend.NewUpdate(user, name), StatusResp: &goimap.StatusResp{Type: goimap.StatusRespBye, Info: "Mailbox deleted; reconnect"}})
	return s.cleanupExpunged(ctx)
}
func (s *MailboxStore) RenameFolder(ctx context.Context, user, old, name string) error {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	var err error
	old, err = imap.NormalizeMailbox(old)
	if err != nil {
		return err
	}
	name, err = imap.NormalizeMailbox(name)
	if err != nil {
		return err
	}
	if name == "INBOX" || strings.HasPrefix(name, old+"/") {
		return fmt.Errorf("invalid rename destination")
	}
	if err = s.ensureUser(ctx, user); err != nil {
		return err
	}
	if err = s.folderExists(ctx, user, old); err != nil {
		return err
	}
	if err = s.folderExists(ctx, user, name); err == nil {
		return fmt.Errorf("destination exists")
	} else if err != backend.ErrNoSuchMailbox {
		return err
	}
	// Parents must already exist; CREATE supports automatic parent creation.
	if pos := strings.LastIndex(name, "/"); pos >= 0 {
		if err = s.folderExists(ctx, user, name[:pos]); err != nil {
			return err
		}
	}
	if err = s.ensureMailboxState(ctx, user, old); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT mailbox FROM mailbox_catalog WHERE username=? AND (mailbox=? OR (?!='INBOX' AND substr(mailbox,1,?)=?)) ORDER BY mailbox`, user, old, old, len(old)+1, old+"/")
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var n string
		if err = rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		names = append(names, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, from := range names {
		to := name + strings.TrimPrefix(from, old)
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM mailbox_catalog WHERE username=? AND mailbox=?`, user, to).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("destination hierarchy conflict")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO mailbox_catalog(username,mailbox,subscribed) SELECT username,?,subscribed FROM mailbox_catalog WHERE username=? AND mailbox=?`, to, user, from); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO mailbox_state(username,mailbox,uidvalidity,uidnext) VALUES(?,?,?,1)`, user, from, time.Now().Unix()); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO mailbox_state(username,mailbox,uidvalidity,uidnext) SELECT username,?,uidvalidity,uidnext FROM mailbox_state WHERE username=? AND mailbox=? ON CONFLICT(username,mailbox) DO UPDATE SET uidvalidity=MAX(mailbox_state.uidvalidity+1,excluded.uidvalidity),uidnext=excluded.uidnext`, to, user, from); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE message_flags SET mailbox=? WHERE username=? AND mailbox=? AND expunged=0`, to, user, from); err != nil {
			return err
		}
		if old != "INBOX" {
			if _, err = tx.ExecContext(ctx, `DELETE FROM mailbox_catalog WHERE username=? AND mailbox=?`, user, from); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.publish(&backend.StatusUpdate{Update: backend.NewUpdate(user, old), StatusResp: &goimap.StatusResp{Type: goimap.StatusRespBye, Info: "Mailbox renamed; reconnect"}})
	return nil
}
func (s *MailboxStore) AppendMessage(ctx context.Context, user, folder string, data []byte, flags []string, date time.Time) (string, error) {
	return s.deliverMetadata(ctx, "", user, folder, data, flags, date, false)
}

// Recover interrupted payload cleanup after durable membership deletion.
func (s *MailboxStore) cleanupExpunged(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT msg_id FROM message_flags WHERE expunged=1 AND msg_id NOT IN (SELECT msg_id FROM message_flags WHERE expunged=0)`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = s.adapter.store.Transition(id, "stored", "deleted"); err != nil {
			return err
		}
	}
	return nil
}

func (s *MailboxStore) CopyMessageIDs(ctx context.Context, user, source, dest string, ids []string) error {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	var err error
	dest, err = imap.NormalizeMailbox(dest)
	if err != nil {
		return err
	}
	if err = s.folderExists(ctx, user, dest); err != nil {
		return err
	}
	if err = s.ensureMailboxState(ctx, user, dest); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var next uint64
	if err = tx.QueryRowContext(ctx, `SELECT uidnext FROM mailbox_state WHERE username=? AND mailbox=?`, user, dest).Scan(&next); err != nil {
		return err
	}
	if next+uint64(len(ids)) > uint64(^uint32(0)) {
		return fmt.Errorf("mailbox UID space exhausted")
	}
	var used int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0) FROM message_flags WHERE username=? AND expunged=0`, user).Scan(&used); err != nil {
		return err
	}
	var blobs []string
	committed := false
	defer func() {
		if !committed {
			for _, id := range blobs {
				s.adapter.discardMessage(id)
			}
		}
	}()
	for _, id := range ids {
		var flags, sender, subject string
		var size, date int64
		var deleted int
		if err = tx.QueryRowContext(ctx, `SELECT flags,sender,subject,size,sent_at,deleted FROM message_flags WHERE username=? AND mailbox=? AND msg_id=? AND expunged=0`, user, source, id).Scan(&flags, &sender, &subject, &size, &date, &deleted); err != nil {
			return err
		}
		used += size
		if s.quotaBytes > 0 && used > s.quotaBytes {
			return fmt.Errorf("mailbox quota exceeded")
		}
		data, err := s.adapter.FetchMessage(ctx, id)
		if err != nil {
			return err
		}
		copyID, err := s.adapter.StoreMessage(ctx, user, dest, data)
		if err != nil {
			return err
		}
		blobs = append(blobs, copyID)
		if _, err = tx.ExecContext(ctx, `INSERT INTO message_flags(msg_id,username,mailbox,uid,flags,sender,subject,size,sent_at,deleted) VALUES(?,?,?,?,?,?,?,?,?,?)`, copyID, user, dest, next, flags, sender, subject, size, date, deleted); err != nil {
			return err
		}
		next++
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mailbox_state SET uidnext=? WHERE username=? AND mailbox=?`, next, user, dest); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	select {
	case s.deliveryCh <- [2]string{user, dest}:
	default:
	}
	s.notifyMailbox(user, dest)
	return nil
}

// SQLite membership is authoritative. A crash between blob persistence and the
// metadata commit leaves an unreferenced managed blob, never visible mail.
func (s *MailboxStore) recoverMailboxData(ctx context.Context) error {
	if err := s.cleanupExpunged(ctx); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT msg_id FROM message_flags WHERE expunged=0`)
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		live[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, entry := range s.adapter.store.ListByStatus("stored", "mailbox") {
		if entry.Metadata["mailbox_managed"] == "true" && !live[entry.MessageID] {
			if err = s.adapter.discardMessage(entry.MessageID); err != nil {
				return err
			}
		}
	}
	for id := range live {
		entry, err := s.adapter.store.Get(id)
		if err != nil || entry.Status != "stored" {
			return fmt.Errorf("mailbox payload missing for %s", id)
		}
	}
	return nil
}
