package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

// All active mail paths modify message_flags. Transactional triggers therefore
// record SMTP, IMAP and JMAP changes together with the mailbox mutation.
func initEmailSync(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS email_sync(username TEXT PRIMARY KEY, epoch TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 0, floor INTEGER NOT NULL DEFAULT 0, event_count INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS email_events(seq INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL, msg_id TEXT NOT NULL, kind TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS email_events_user ON email_events(username,seq);
CREATE TABLE IF NOT EXISTS email_sync_meta(key TEXT PRIMARY KEY);
CREATE TRIGGER IF NOT EXISTS email_event_retention AFTER INSERT ON email_events BEGIN
 INSERT OR IGNORE INTO email_sync(username,epoch) VALUES(NEW.username,lower(hex(randomblob(16))));
 UPDATE email_sync SET revision=NEW.seq, floor=CASE WHEN event_count>=10000 THEN (SELECT MIN(seq) FROM email_events WHERE username=NEW.username) ELSE floor END, event_count=MIN(event_count+1,10000) WHERE username=NEW.username;
 DELETE FROM email_events WHERE username=NEW.username AND seq<=(SELECT floor FROM email_sync WHERE username=NEW.username);
END;
CREATE TRIGGER IF NOT EXISTS email_insert AFTER INSERT ON message_flags WHEN NEW.expunged=0 BEGIN
 INSERT INTO email_events(username,msg_id,kind) VALUES(NEW.username,NEW.msg_id,'created');
END;
CREATE TRIGGER IF NOT EXISTS email_update AFTER UPDATE ON message_flags
WHEN OLD.expunged!=NEW.expunged OR (NEW.expunged=0 AND (OLD.flags!=NEW.flags OR OLD.mailbox!=NEW.mailbox OR OLD.uid!=NEW.uid OR OLD.sent_at!=NEW.sent_at OR OLD.subject!=NEW.subject OR OLD.sender!=NEW.sender OR OLD.size!=NEW.size)) BEGIN
 INSERT INTO email_events(username,msg_id,kind) VALUES(NEW.username,NEW.msg_id,CASE WHEN NEW.expunged!=0 THEN 'destroyed' WHEN OLD.expunged!=0 THEN 'created' ELSE 'updated' END);
END;
CREATE TRIGGER IF NOT EXISTS email_delete AFTER DELETE ON message_flags WHEN OLD.expunged=0 BEGIN
 INSERT INTO email_events(username,msg_id,kind) VALUES(OLD.username,OLD.msg_id,'destroyed');
END;
INSERT INTO email_events(username,msg_id,kind) SELECT username,msg_id,'created' FROM message_flags WHERE expunged=0 AND NOT EXISTS(SELECT 1 FROM email_sync_meta WHERE key='bootstrap');
INSERT OR IGNORE INTO email_sync_meta(key) VALUES('bootstrap');`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type syncQuery interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func syncState(ctx context.Context, q syncQuery, user string) (string, int64, int64, error) {
	var epoch string
	var revision, floor int64
	err := q.QueryRowContext(ctx, `SELECT epoch,revision,floor FROM email_sync WHERE username=?`, user).Scan(&epoch, &revision, &floor)
	return "e1:" + epoch + ":", revision, floor, err
}
func (s *MailboxStore) ensureEmailSync(ctx context.Context, user string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO email_sync(username,epoch) VALUES(?,lower(hex(randomblob(16))))`, user)
	return err
}
func stateToken(prefix string, n int64) string { return prefix + strconv.FormatInt(n, 10) }

func (s *MailboxStore) EmailSnapshot(ctx context.Context, user string) (map[string]mailstate.Message, string, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	if err := s.ensureEmailSync(ctx, user); err != nil {
		return nil, "", err
	}
	prefix, revision, _, err := syncState(ctx, s.db, user)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.msg_id,m.mailbox,COALESCE(c.jmap_id,''),m.uid,m.flags,m.sender,m.subject,m.size,m.sent_at,m.deleted FROM message_flags m LEFT JOIN mailbox_catalog c ON c.username=m.username AND c.mailbox=m.mailbox WHERE m.username=? AND m.expunged=0`, user)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := map[string]mailstate.Message{}
	for rows.Next() {
		var m mailstate.Message
		var flags string
		var date int64
		var deleted int
		if err = rows.Scan(&m.ID, &m.Folder, &m.MailboxID, &m.UID, &flags, &m.From, &m.Subject, &m.Size, &date, &deleted); err != nil {
			return nil, "", err
		}
		if flags != "" {
			m.Flags = strings.Split(flags, " ")
		}
		m.Date = time.Unix(date, 0)
		m.Deleted = deleted != 0
		out[m.ID] = m
	}
	return out, stateToken(prefix, revision), rows.Err()
}

func (s *MailboxStore) EmailChanges(ctx context.Context, user, since string, limit int) (mailstate.Changes, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	out := mailstate.Changes{OldState: since, Created: []string{}, Updated: []string{}, Destroyed: []string{}}
	if limit < 1 || limit > 500 {
		return out, fmt.Errorf("invalid change limit")
	}
	if err := s.ensureEmailSync(ctx, user); err != nil {
		return out, err
	}
	prefix, current, floor, err := syncState(ctx, s.db, user)
	if err != nil {
		return out, err
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(since, prefix), 10, 64)
	if err != nil || !strings.HasPrefix(since, prefix) || stateToken(prefix, n) != since || n < floor || n > current {
		return out, mailstate.ErrCannotCalculate
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq,msg_id,kind FROM email_events WHERE username=? AND seq>? ORDER BY seq`, user, n)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	first, last := map[string]string{}, map[string]string{}
	end := current
	for rows.Next() {
		var seq int64
		var id, kind string
		if err = rows.Scan(&seq, &id, &kind); err != nil {
			return out, err
		}
		if _, ok := first[id]; !ok {
			if len(first) == limit {
				out.HasMore = true
				break
			}
			first[id] = kind
		}
		last[id] = kind
		end = seq
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	for id, kind := range last {
		if first[id] == "created" {
			if kind != "destroyed" {
				out.Created = append(out.Created, id)
			}
		} else if kind == "destroyed" {
			out.Destroyed = append(out.Destroyed, id)
		} else {
			out.Updated = append(out.Updated, id)
		}
	}
	if !out.HasMore {
		end = current
	}
	out.NewState = stateToken(prefix, end)
	sort.Strings(out.Created)
	sort.Strings(out.Updated)
	sort.Strings(out.Destroyed)
	return out, nil
}

func (s *MailboxStore) SetEmailKeywords(ctx context.Context, user, since string, patches map[string]mailstate.Patch) (mailstate.SetResult, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	out := mailstate.SetResult{Updated: []string{}, NotFound: []string{}}
	if err := s.ensureEmailSync(ctx, user); err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	prefix, revision, _, err := syncState(ctx, tx, user)
	if err != nil {
		return out, err
	}
	out.OldState = stateToken(prefix, revision)
	if since != "" && since != out.OldState {
		return out, mailstate.ErrStateMismatch
	}
	ids := make([]string, 0, len(patches))
	for id := range patches {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	type notice struct {
		id, folder string
		flags      []string
	}
	var notices []notice
	for _, id := range ids {
		var folder, value string
		err = tx.QueryRowContext(ctx, `SELECT mailbox,flags FROM message_flags WHERE username=? AND msg_id=? AND expunged=0`, user, id).Scan(&folder, &value)
		if err == sql.ErrNoRows {
			out.NotFound = append(out.NotFound, id)
			continue
		}
		if err != nil {
			return out, err
		}
		flags := strings.Fields(value)
		patch := patches[id]
		if patch.Replace != nil {
			preserved := []string{}
			for _, f := range flags {
				if strings.EqualFold(f, `\Deleted`) || strings.EqualFold(f, `\Recent`) {
					preserved = append(preserved, f)
				}
			}
			flags = append(preserved, (*patch.Replace)...)
		}
		flags = unionFlags(flags, patch.Add)
		flags = subtractFlags(flags, patch.Remove)
		flags, err = validFlags(flags)
		if err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE message_flags SET flags=? WHERE username=? AND msg_id=? AND expunged=0`, strings.Join(flags, " "), user, id); err != nil {
			return out, err
		}
		out.Updated = append(out.Updated, id)
		notices = append(notices, notice{id, folder, flags})
	}
	prefix, revision, _, err = syncState(ctx, tx, user)
	if err != nil {
		return out, err
	}
	out.NewState = stateToken(prefix, revision)
	if err = tx.Commit(); err != nil {
		return out, err
	}
	for _, n := range notices {
		s.notifyFlags(ctx, user, n.folder, n.id, n.flags)
	}
	return out, nil
}
