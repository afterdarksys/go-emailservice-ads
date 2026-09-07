package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

// initMailboxSync migrates legacy folder paths to persistent identities. Triggers
// keep the journal in the same transaction as every protocol's mutations.
func initMailboxSync(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`PRAGMA table_info(mailbox_catalog)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var n, notnull, pk int
		var name, kind string
		var defaultValue interface{}
		if err = rows.Scan(&n, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for name, definition := range map[string]string{"jmap_id": "TEXT NOT NULL DEFAULT ''", "jmap_role": "TEXT NOT NULL DEFAULT ''", "jmap_sort_order": "INTEGER NOT NULL DEFAULT 0"} {
		if !columns[name] {
			if _, err = tx.Exec(`ALTER TABLE mailbox_catalog ADD COLUMN ` + name + ` ` + definition); err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(`
CREATE UNIQUE INDEX IF NOT EXISTS mailbox_jmap_id ON mailbox_catalog(username,jmap_id) WHERE jmap_id!='';
CREATE TABLE IF NOT EXISTS mailbox_sync(username TEXT PRIMARY KEY,epoch TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 0,floor INTEGER NOT NULL DEFAULT 0,event_count INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS mailbox_events(seq INTEGER PRIMARY KEY AUTOINCREMENT,username TEXT NOT NULL,msg_id TEXT NOT NULL,kind TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS mailbox_events_user ON mailbox_events(username,seq);
CREATE TRIGGER IF NOT EXISTS mailbox_event_retention AFTER INSERT ON mailbox_events BEGIN
 INSERT OR IGNORE INTO mailbox_sync(username,epoch) VALUES(NEW.username,lower(hex(randomblob(16))));
 UPDATE mailbox_sync SET revision=NEW.seq,floor=CASE WHEN event_count>=10000 THEN (SELECT MIN(seq) FROM mailbox_events WHERE username=NEW.username) ELSE floor END,event_count=MIN(event_count+1,10000) WHERE username=NEW.username;
 DELETE FROM mailbox_events WHERE username=NEW.username AND seq<=(SELECT floor FROM mailbox_sync WHERE username=NEW.username);
END;
CREATE TRIGGER IF NOT EXISTS mailbox_identity AFTER INSERT ON mailbox_catalog WHEN NEW.jmap_id='' BEGIN
 UPDATE mailbox_catalog SET jmap_id=lower(hex(randomblob(16))),jmap_role=CASE WHEN mailbox IN ('INBOX','Sent','Drafts','Trash','Junk') AND NOT EXISTS(SELECT 1 FROM mailbox_catalog r WHERE r.username=NEW.username AND r.jmap_role=lower(NEW.mailbox)) THEN lower(mailbox) ELSE '' END WHERE username=NEW.username AND mailbox=NEW.mailbox;
END;
CREATE TRIGGER IF NOT EXISTS mailbox_created AFTER INSERT ON mailbox_catalog WHEN NEW.jmap_id!='' BEGIN
 INSERT INTO mailbox_events(username,msg_id,kind) VALUES(NEW.username,NEW.jmap_id,'created');
END;
CREATE TRIGGER IF NOT EXISTS mailbox_updated AFTER UPDATE ON mailbox_catalog WHEN NEW.jmap_id!='' AND (OLD.jmap_id!=NEW.jmap_id OR OLD.mailbox!=NEW.mailbox OR OLD.subscribed!=NEW.subscribed OR OLD.jmap_sort_order!=NEW.jmap_sort_order OR OLD.jmap_role!=NEW.jmap_role) BEGIN
 INSERT INTO mailbox_events(username,msg_id,kind) VALUES(NEW.username,NEW.jmap_id,CASE WHEN OLD.jmap_id='' THEN 'created' ELSE 'updated' END);
 INSERT INTO email_events(username,msg_id,kind) SELECT username,msg_id,'updated' FROM message_flags WHERE username=NEW.username AND mailbox=NEW.mailbox AND expunged=0 AND OLD.jmap_id!=NEW.jmap_id;
END;
CREATE TRIGGER IF NOT EXISTS mailbox_deleted AFTER DELETE ON mailbox_catalog WHEN OLD.jmap_id!='' BEGIN
 INSERT INTO mailbox_events(username,msg_id,kind) VALUES(OLD.username,OLD.jmap_id,'destroyed');
END;
CREATE TRIGGER IF NOT EXISTS mailbox_count_insert AFTER INSERT ON message_flags WHEN NEW.expunged=0 BEGIN
 INSERT INTO mailbox_events(username,msg_id,kind) SELECT username,jmap_id,'updated' FROM mailbox_catalog WHERE username=NEW.username AND mailbox=NEW.mailbox AND jmap_id!='';
END;
CREATE TRIGGER IF NOT EXISTS mailbox_count_update AFTER UPDATE ON message_flags WHEN (OLD.expunged=0 OR NEW.expunged=0) AND (OLD.expunged!=NEW.expunged OR OLD.flags!=NEW.flags OR OLD.mailbox!=NEW.mailbox) BEGIN
 INSERT INTO mailbox_events(username,msg_id,kind) SELECT username,jmap_id,'updated' FROM mailbox_catalog WHERE username=NEW.username AND mailbox IN (OLD.mailbox,NEW.mailbox) AND jmap_id!='';
END;
CREATE TRIGGER IF NOT EXISTS mailbox_count_delete AFTER DELETE ON message_flags WHEN OLD.expunged=0 BEGIN
 INSERT INTO mailbox_events(username,msg_id,kind) SELECT username,jmap_id,'updated' FROM mailbox_catalog WHERE username=OLD.username AND mailbox=OLD.mailbox AND jmap_id!='';
END;
UPDATE mailbox_catalog SET jmap_id=lower(hex(randomblob(16))),jmap_role=CASE WHEN mailbox IN ('INBOX','Sent','Drafts','Trash','Junk') THEN lower(mailbox) ELSE '' END WHERE jmap_id='';
UPDATE email_sync SET epoch=lower(hex(randomblob(16))) WHERE NOT EXISTS(SELECT 1 FROM email_sync_meta WHERE key='thread-identity-v1');
UPDATE mailbox_sync SET epoch=lower(hex(randomblob(16))) WHERE NOT EXISTS(SELECT 1 FROM email_sync_meta WHERE key='thread-identity-v1');
DELETE FROM jmap_snapshots WHERE kind IN ('Email','Thread') AND NOT EXISTS(SELECT 1 FROM email_sync_meta WHERE key='thread-identity-v1');
INSERT OR IGNORE INTO email_sync_meta(key) VALUES('thread-identity-v1');`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MailboxStore) ensureMailboxSync(ctx context.Context, user string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO mailbox_sync(username,epoch) VALUES(?,lower(hex(randomblob(16))))`, user)
	return err
}
func syncMailboxState(ctx context.Context, q syncQuery, user string) (string, int64, int64, error) {
	var epoch string
	var revision, floor int64
	err := q.QueryRowContext(ctx, `SELECT epoch,revision,floor FROM mailbox_sync WHERE username=?`, user).Scan(&epoch, &revision, &floor)
	return "m1:" + epoch + ":", revision, floor, err
}
func (s *MailboxStore) MailboxSnapshot(ctx context.Context, user string) ([]mailstate.Mailbox, string, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	if err := s.ensureUser(ctx, user); err != nil {
		return nil, "", err
	}
	if err := s.ensureMailboxSync(ctx, user); err != nil {
		return nil, "", err
	}
	prefix, n, _, err := syncMailboxState(ctx, s.db, user)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.jmap_id,c.mailbox,c.jmap_role,c.subscribed,c.jmap_sort_order,COUNT(m.msg_id),COALESCE(SUM(CASE WHEN m.msg_id IS NOT NULL AND instr(' '||lower(m.flags)||' ',' \seen ')=0 THEN 1 ELSE 0 END),0) FROM mailbox_catalog c LEFT JOIN message_flags m ON c.username=m.username AND c.mailbox=m.mailbox AND m.expunged=0 WHERE c.username=? GROUP BY c.mailbox ORDER BY c.mailbox`, user)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []mailstate.Mailbox{}
	for rows.Next() {
		var m mailstate.Mailbox
		var subscribed int
		if err = rows.Scan(&m.ID, &m.Path, &m.Role, &subscribed, &m.SortOrder, &m.Total, &m.Unread); err != nil {
			return nil, "", err
		}
		m.Subscribed = subscribed != 0
		out = append(out, m)
	}
	return out, stateToken(prefix, n), rows.Err()
}

func (s *MailboxStore) MailboxChanges(ctx context.Context, user, since string, limit int) (mailstate.Changes, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	out := mailstate.Changes{OldState: since, Created: []string{}, Updated: []string{}, Destroyed: []string{}}
	if limit < 1 || limit > 500 {
		return out, fmt.Errorf("invalid change limit")
	}
	if err := s.ensureUser(ctx, user); err != nil {
		return out, err
	}
	if err := s.ensureMailboxSync(ctx, user); err != nil {
		return out, err
	}
	prefix, current, floor, err := syncMailboxState(ctx, s.db, user)
	if err != nil {
		return out, err
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(since, prefix), 10, 64)
	if err != nil || !strings.HasPrefix(since, prefix) || stateToken(prefix, n) != since || n < floor || n > current {
		return out, mailstate.ErrCannotCalculate
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq,msg_id,kind FROM mailbox_events WHERE username=? AND seq>? ORDER BY seq`, user, n)
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
