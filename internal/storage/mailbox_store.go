package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/mail"
	"strings"
	"time"

	goiMap "github.com/emersion/go-imap"
	_ "modernc.org/sqlite"

	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
)

// MailboxStore is a SQLite-backed implementation of imap.Store.
// It wraps IMAPAdapter for raw message storage and adds persistent
// UID tracking, flag management, and IMAP IDLE delivery notifications.
type MailboxStore struct {
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

	if err := initSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &MailboxStore{
		adapter:    adapter,
		db:         db,
		deliveryCh: make(chan [2]string, 64),
	}, nil
}

// Close closes the underlying SQLite database.
func (s *MailboxStore) Close() error {
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
	raw, err := s.adapter.GetMessages(ctx, username, folder)
	if err != nil {
		return nil, err
	}

	// Build a set of expunged msg_ids to filter out.
	expunged, err := s.expungedSet(ctx, username, folder)
	if err != nil {
		return nil, err
	}

	var out []imap.MessageSummary
	for _, r := range raw {
		if expunged[r.ID] {
			continue
		}
		summary := imap.MessageSummary{
			ID:   r.ID,
			Size: r.Size,
		}
		// Enrich from SQLite if a flags row exists.
		var uid int64
		var flagsStr, sender, subject string
		var sentAt int64
		var deleted int
		err := s.db.QueryRowContext(ctx,
			`SELECT uid, flags, sender, subject, sent_at, deleted
			 FROM message_flags WHERE msg_id=? AND username=? AND mailbox=?`,
			r.ID, username, folder).Scan(&uid, &flagsStr, &sender, &subject, &sentAt, &deleted)
		if err == nil {
			summary.UID = uint32(uid)
			if flagsStr != "" {
				summary.Flags = strings.Split(flagsStr, " ")
			}
			summary.From = sender
			summary.Subject = subject
			if sentAt > 0 {
				summary.Date = time.Unix(sentAt, 0)
			}
			summary.Deleted = deleted == 1
		} else {
			summary.Flags = r.Flags
		}
		out = append(out, summary)
	}
	return out, nil
}

// FetchMessage delegates to the underlying IMAPAdapter.
func (s *MailboxStore) FetchMessage(ctx context.Context, msgID string) ([]byte, error) {
	return s.adapter.FetchMessage(ctx, msgID)
}

// StoreMessage stores the message via IMAPAdapter, assigns it a UID, records
// its metadata in SQLite, and fires a delivery notification for IMAP IDLE.
func (s *MailboxStore) StoreMessage(ctx context.Context, username, folder string, data []byte) (string, error) {
	msgID, err := s.adapter.StoreMessage(ctx, username, folder, data)
	if err != nil {
		return "", err
	}

	uid, err := s.AllocateUID(ctx, username, folder)
	if err != nil {
		// Non-fatal — message is stored, UID tracking just failed.
		uid = 0
	}

	sender, subject, sentAt := parseHeaders(data)

	s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO message_flags
		 (msg_id, username, mailbox, uid, flags, sender, subject, size, sent_at)
		 VALUES (?, ?, ?, ?, '', ?, ?, ?, ?)`,
		msgID, username, folder, uid, sender, subject, int64(len(data)), sentAt.Unix())

	// Non-blocking delivery notification for IMAP IDLE.
	select {
	case s.deliveryCh <- [2]string{username, folder}:
	default:
	}

	return msgID, nil
}

// UpdateMessageFlags applies a flag operation to the message in SQLite.
// RFC 3501 §6.4.6 — STORE command.
func (s *MailboxStore) UpdateMessageFlags(ctx context.Context, msgID, username, mailbox string, op goiMap.FlagsOp, flags []string) error {
	var current []string
	var flagsStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT flags FROM message_flags WHERE msg_id=? AND username=? AND mailbox=?`,
		msgID, username, mailbox).Scan(&flagsStr)
	if err == nil && flagsStr != "" {
		current = strings.Split(flagsStr, " ")
	}

	switch op {
	case goiMap.SetFlags:
		current = flags
	case goiMap.AddFlags:
		current = unionFlags(current, flags)
	case goiMap.RemoveFlags:
		current = subtractFlags(current, flags)
	}

	deleted := containsFlag(current, `\Deleted`)
	newFlagsStr := strings.Join(current, " ")

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO message_flags (msg_id, username, mailbox, flags, deleted)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(msg_id, username, mailbox) DO UPDATE SET flags=excluded.flags, deleted=excluded.deleted`,
		msgID, username, mailbox, newFlagsStr, boolInt(deleted))
	return err
}

// ExpungeDeleted permanently removes messages marked \Deleted from the SQLite
// view (setting expunged=1) and returns their message IDs so callers can
// issue EXPUNGE responses to IMAP clients.
func (s *MailboxStore) ExpungeDeleted(ctx context.Context, username, mailbox string) ([]string, error) {
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
