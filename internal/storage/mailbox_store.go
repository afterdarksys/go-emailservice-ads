package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"

	goiMap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	_ "modernc.org/sqlite"

	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
)

// MailboxStore is a SQLite-backed implementation of imap.Store.
// It wraps IMAPAdapter for raw message storage and adds persistent
// UID tracking, flag management, and IMAP IDLE delivery notifications.
type MailboxStore struct {
	eventMu    sync.Mutex
	protocolCh chan backend.Update
	stopEvents chan struct{}
	closeOnce  sync.Once
	deliveryMu sync.Mutex
	quotaBytes int64
	adapter    *IMAPAdapter
	db         *sql.DB
	deliveryCh chan [2]string // [username, mailbox] — read by imap.Backend for IDLE
}

// NewMailboxStore opens (or creates) the SQLite database at dbPath and
// returns a MailboxStore wrapping the given IMAPAdapter.
func NewMailboxStore(adapter *IMAPAdapter, dbPath string) (*MailboxStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open mailbox db: %w", err)
	}

	db.SetMaxOpenConns(1)
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := initEmailSync(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize email synchronization: %w", err)
	}

	s := &MailboxStore{
		adapter:    adapter,
		db:         db,
		deliveryCh: make(chan [2]string, 64),
		stopEvents: make(chan struct{}),
	}
	if err := s.recoverMailboxData(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying SQLite database.
func (s *MailboxStore) Close() error {
	s.closeOnce.Do(func() { close(s.stopEvents) })
	return s.db.Close()
}

// DeliveryUpdates implements imap.Updater. The returned channel emits
// [username, mailbox] pairs whenever a new message is stored locally.
func (s *MailboxStore) DeliveryUpdates() <-chan [2]string {
	return s.deliveryCh
}

// initSchema creates the required tables if they do not exist.
func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS mailbox_users (username TEXT PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS mailbox_catalog (username TEXT NOT NULL, mailbox TEXT NOT NULL, subscribed INTEGER NOT NULL DEFAULT 1, PRIMARY KEY(username,mailbox));
 CREATE TABLE IF NOT EXISTS mailbox_state (
		username    TEXT NOT NULL,
		mailbox     TEXT NOT NULL,
		uidvalidity INTEGER NOT NULL DEFAULT 1,
		uidnext     INTEGER NOT NULL DEFAULT 1,
		PRIMARY KEY (username, mailbox)
	);

	CREATE TABLE IF NOT EXISTS message_flags (
		msg_id   TEXT NOT NULL,
		username TEXT NOT NULL,
		mailbox  TEXT NOT NULL,
		uid      INTEGER NOT NULL DEFAULT 0,
		flags    TEXT NOT NULL DEFAULT '',
		sender   TEXT NOT NULL DEFAULT '',
		subject  TEXT NOT NULL DEFAULT '',
		size     INTEGER NOT NULL DEFAULT 0,
		sent_at  INTEGER NOT NULL DEFAULT 0,
		deleted  INTEGER NOT NULL DEFAULT 0,
		expunged INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (msg_id, username, mailbox)
	);`)
	if err != nil {
		return fmt.Errorf("mailbox_store schema init: %w", err)
	}
	return nil
}

// GetUIDValidity returns the UIDVALIDITY value for the mailbox, initialising
// it to the current Unix timestamp on first access (RFC 3501 §2.3.1.1).
func (s *MailboxStore) GetUIDValidity(ctx context.Context, username, mailbox string) (uint32, error) {
	if err := s.ensureMailboxState(ctx, username, mailbox); err != nil {
		return 0, err
	}
	var v int64
	err := s.db.QueryRowContext(ctx,
		`SELECT uidvalidity FROM mailbox_state WHERE username=? AND mailbox=?`,
		username, mailbox).Scan(&v)
	return uint32(v), err
}

// GetUIDNext returns the next UID that will be allocated for a mailbox.
func (s *MailboxStore) GetUIDNext(ctx context.Context, username, mailbox string) (uint32, error) {
	if err := s.ensureMailboxState(ctx, username, mailbox); err != nil {
		return 0, err
	}
	var next int64
	err := s.db.QueryRowContext(ctx,
		`SELECT uidnext FROM mailbox_state WHERE username=? AND mailbox=?`,
		username, mailbox).Scan(&next)
	return uint32(next), err
}

// AllocateUID atomically increments uidnext and returns the allocated UID.
func (s *MailboxStore) AllocateUID(ctx context.Context, username, mailbox string) (uint32, error) {
	if err := s.ensureMailboxState(ctx, username, mailbox); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var next int64
	if err := tx.QueryRowContext(ctx,
		`SELECT uidnext FROM mailbox_state WHERE username=? AND mailbox=?`,
		username, mailbox).Scan(&next); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE mailbox_state SET uidnext=uidnext+1 WHERE username=? AND mailbox=?`,
		username, mailbox); err != nil {
		return 0, err
	}
	if next <= 0 || next >= int64(^uint32(0)) {
		return 0, fmt.Errorf("mailbox UID space exhausted")
	}
	return uint32(next), tx.Commit()
}

// ensureMailboxState inserts a mailbox_state row if one does not already exist.
func (s *MailboxStore) ensureMailboxState(ctx context.Context, username, mailbox string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO mailbox_state (username, mailbox, uidvalidity, uidnext)
		 VALUES (?, ?, ?, 1)`,
		username, mailbox, time.Now().Unix())
	return err
}

// GetMessages returns message summaries for the mailbox, enriched with UID
// and flag data from SQLite. Expunged messages are excluded.
func (s *MailboxStore) GetMessages(ctx context.Context, username, folder string) ([]imap.MessageSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT msg_id,uid,flags,sender,subject,size,sent_at,deleted FROM message_flags WHERE username=? AND mailbox=? AND expunged=0 ORDER BY uid`, username, folder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []imap.MessageSummary{}
	for rows.Next() {
		var m imap.MessageSummary
		var flags string
		var date int64
		var deleted int
		if err := rows.Scan(&m.ID, &m.UID, &flags, &m.From, &m.Subject, &m.Size, &date, &deleted); err != nil {
			return nil, err
		}
		if flags != "" {
			m.Flags = strings.Split(flags, " ")
		}
		m.Date = time.Unix(date, 0)
		m.Deleted = deleted == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

// FetchMessage delegates to the underlying IMAPAdapter.
func (s *MailboxStore) FetchMessage(ctx context.Context, msgID string) ([]byte, error) {
	return s.adapter.FetchMessage(ctx, msgID)
}

// StoreMessage stores the message via IMAPAdapter, assigns it a UID, records
// its metadata in SQLite, and fires a delivery notification for IMAP IDLE.
func (s *MailboxStore) StoreMessage(ctx context.Context, username, folder string, data []byte) (string, error) {
	return s.DeliverOnce(ctx, "", username, folder, data)
}
func (s *MailboxStore) SetQuota(bytes int64) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	s.quotaBytes = bytes
}

// DeliverOnce deduplicates retries by the durable queue transaction and recipient.
func (s *MailboxStore) DeliverOnce(ctx context.Context, key, username, folder string, data []byte) (string, error) {
	return s.deliverMetadata(ctx, key, username, folder, data, nil, time.Now(), true)
}
func (s *MailboxStore) deliverMetadata(ctx context.Context, key, username, folder string, data []byte, flags []string, date time.Time, create bool) (string, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	if err := s.ensureUser(ctx, username); err != nil {
		return "", err
	}
	var err error
	folder, err = imap.NormalizeMailbox(folder)
	if err != nil {
		return "", err
	}
	if create {
		if err = s.createFolder(ctx, username, folder, true); err != nil {
			return "", err
		}
	} else if err = s.folderExists(ctx, username, folder); err != nil {
		return "", err
	}
	flags, err = validFlags(flags)
	if err != nil {
		return "", err
	}
	if date.IsZero() {
		date = time.Now()
	}
	id := ""
	if key != "" {
		id = fmt.Sprintf("delivery-%x", sha256.Sum256([]byte(key)))
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM message_flags WHERE msg_id=?", id).Scan(&count); err != nil {
			return "", err
		}
		if count > 0 {
			return id, nil
		}
	}
	if s.quotaBytes > 0 {
		var used int64
		if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(size),0) FROM message_flags WHERE username=? AND expunged=0", username).Scan(&used); err != nil {
			return "", err
		}
		if used+int64(len(data)) > s.quotaBytes {
			return "", fmt.Errorf("mailbox quota exceeded")
		}
	}

	msgID, err := s.adapter.storeMessageID(ctx, id, username, folder, data)
	if err != nil {
		return "", err
	}

	uid, err := s.AllocateUID(ctx, username, folder)
	if err != nil {
		if discardErr := s.adapter.discardMessage(msgID); discardErr != nil {
			return "", fmt.Errorf("allocate mailbox UID: %w (also failed to discard orphaned message: %v)", err, discardErr)
		}
		return "", fmt.Errorf("allocate mailbox UID: %w", err)
	}

	sender, subject, _ := parseHeaders(data)

	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO message_flags
		 (msg_id, username, mailbox, uid, flags, sender, subject, size, sent_at, deleted)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msgID, username, folder, uid, strings.Join(flags, " "), sender, subject, int64(len(data)), date.Unix(), boolInt(containsFlag(flags, `\Deleted`))); err != nil {
		if discardErr := s.adapter.discardMessage(msgID); discardErr != nil {
			return "", fmt.Errorf("persist mailbox metadata: %w (also failed to discard orphaned message: %v)", err, discardErr)
		}
		return "", fmt.Errorf("persist mailbox metadata: %w", err)
	}

	// Non-blocking delivery notification for IMAP IDLE.
	select {
	case s.deliveryCh <- [2]string{username, folder}:
	default:
	}

	s.notifyMailbox(username, folder)
	return msgID, nil
}

// UpdateMessageFlags applies a flag operation to the message in SQLite.
// RFC 3501 §6.4.6 — STORE command.
func (s *MailboxStore) UpdateMessageFlags(ctx context.Context, msgID, username, mailbox string, op goiMap.FlagsOp, flags []string) error {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	var err error
	flags, err = validFlags(flags)
	if err != nil {
		return err
	}
	var value string
	if err = s.db.QueryRowContext(ctx, `SELECT flags FROM message_flags WHERE msg_id=? AND username=? AND mailbox=? AND expunged=0`, msgID, username, mailbox).Scan(&value); err != nil {
		return err
	}
	var current []string
	if value != "" {
		current = strings.Split(value, " ")
	}
	switch op {
	case goiMap.SetFlags:
		current = flags
	case goiMap.AddFlags:
		current = unionFlags(current, flags)
	case goiMap.RemoveFlags:
		current = subtractFlags(current, flags)
	default:
		return fmt.Errorf("invalid flag operation")
	}
	_, err = s.db.ExecContext(ctx, `UPDATE message_flags SET flags=?,deleted=? WHERE msg_id=? AND username=? AND mailbox=? AND expunged=0`, strings.Join(current, " "), boolInt(containsFlag(current, `\Deleted`)), msgID, username, mailbox)
	if err == nil {
		s.notifyFlags(ctx, username, mailbox, msgID, current)
	}
	return err
}

// ExpungeDeleted permanently removes messages marked \Deleted from the SQLite
// view (setting expunged=1) and returns their message IDs so callers can
// issue EXPUNGE responses to IMAP clients.
func (s *MailboxStore) ExpungeDeleted(ctx context.Context, username, mailbox string) ([]string, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	before, err := s.GetMessages(ctx, username, mailbox)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT msg_id FROM message_flags
   WHERE username=? AND mailbox=? AND deleted=1 AND expunged=0`,
		username, mailbox)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := rows.Close(); err != nil {
		return nil, err
	}

	if len(ids) > 0 {
		placeholders := strings.Repeat("?,", len(ids))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]interface{}, len(ids)+2)
		args[0] = username
		args[1] = mailbox
		for i, id := range ids {
			args[i+2] = id
		}
		_, err = s.db.ExecContext(ctx,
			`UPDATE message_flags SET expunged=1
			 WHERE username=? AND mailbox=? AND msg_id IN (`+placeholders+`)`,
			args...)
		if err != nil {
			return nil, err
		}
	}

	// Tombstone expunged payloads, including a previous interrupted cleanup.
	cleanup, err := s.expungedSet(ctx, username, mailbox)
	if err != nil {
		return nil, err
	}
	for id := range cleanup {
		if _, err := s.adapter.store.Transition(id, "stored", "deleted"); err != nil {
			return nil, err
		}
	}
	removed := map[string]bool{}
	for _, id := range ids {
		removed[id] = true
	}
	for i := len(before) - 1; i >= 0; i-- {
		if removed[before[i].ID] {
			s.publish(&backend.ExpungeUpdate{Update: backend.NewUpdate(username, mailbox), SeqNum: uint32(i + 1)})
		}
	}
	return ids, nil
}

// expungedSet returns the set of expunged message IDs for the mailbox.
func (s *MailboxStore) expungedSet(ctx context.Context, username, mailbox string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT msg_id FROM message_flags WHERE username=? AND mailbox=? AND expunged=1`,
		username, mailbox)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// parseHeaders extracts From, Subject and Date from raw RFC 5322 message bytes.
func parseHeaders(data []byte) (sender, subject string, date time.Time) {
	msg, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		return "", "", time.Time{}
	}
	sender = msg.Header.Get("From")
	subject = msg.Header.Get("Subject")
	if d := msg.Header.Get("Date"); d != "" {
		if t, err := mail.ParseDate(d); err == nil {
			date = t
		}
	}
	return
}

func unionFlags(current, add []string) []string {
	set := make(map[string]bool, len(current))
	for _, f := range current {
		set[f] = true
	}
	for _, f := range add {
		set[f] = true
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	return out
}

func subtractFlags(current, remove []string) []string {
	rm := make(map[string]bool, len(remove))
	for _, f := range remove {
		rm[f] = true
	}
	var out []string
	for _, f := range current {
		if !rm[f] {
			out = append(out, f)
		}
	}
	return out
}

func containsFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, flag) {
			return true
		}
	}
	return false
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DeliverOnceWithFlags preserves Sieve flags with the delivery checkpoint.
func (s *MailboxStore) DeliverOnceWithFlags(ctx context.Context, key, user, folder string, data []byte, flags []string) (string, error) {
	return s.deliverMetadata(ctx, key, user, folder, data, flags, time.Now(), true)
}
