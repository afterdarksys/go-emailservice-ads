package storage

import (
	"context"
	"database/sql"
	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *MailboxStore) mailboxPath(ctx context.Context, user, id string) (string, error) {
	var path string
	err := s.db.QueryRowContext(ctx, `SELECT mailbox FROM mailbox_catalog WHERE username=? AND jmap_id=?`, user, id).Scan(&path)
	return path, err
}
func (s *MailboxStore) patchedMailboxPath(ctx context.Context, user, old string, p mailstate.MailboxPatch) (string, string) {
	parent, name := "", old
	if i := strings.LastIndex(old, "/"); i >= 0 {
		parent, name = old[:i], old[i+1:]
	}
	if p.Name != nil {
		name = *p.Name
	}
	if name == "" || strings.Contains(name, "/") {
		return "", "invalidProperties"
	}
	if p.Parent != nil {
		parent = ""
		if *p.Parent != "" {
			var err error
			parent, err = s.mailboxPath(ctx, user, *p.Parent)
			if err == sql.ErrNoRows {
				return "", "invalidProperties"
			}
			if err != nil {
				return "", "serverFail"
			}
		}
	}
	path := name
	if parent != "" {
		path = parent + "/" + name
	}
	normalized, err := imap.NormalizeMailbox(path)
	if err != nil || normalized != path || strings.Count(path, "/") >= 10 {
		return "", "invalidProperties"
	}
	if old != "" && (parent == old || strings.HasPrefix(parent, old+"/")) {
		return "", "invalidProperties"
	}
	if old == "INBOX" && path != old {
		return "", "forbidden"
	}
	return path, ""
}
func mailboxAttributes(ctx context.Context, tx *sql.Tx, user, path string, p mailstate.MailboxPatch) error {
	var subscribed, order interface{}
	if p.Subscribed != nil {
		subscribed = boolInt(*p.Subscribed)
	}
	if p.SortOrder != nil {
		order = *p.SortOrder
	}
	_, err := tx.ExecContext(ctx, `UPDATE mailbox_catalog SET subscribed=COALESCE(?,subscribed),jmap_sort_order=COALESCE(?,jmap_sort_order) WHERE username=? AND mailbox=?`, subscribed, order, user, path)
	return err
}
func (s *MailboxStore) createJMAPMailbox(ctx context.Context, user, path string, p mailstate.MailboxPatch) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO mailbox_catalog(username,mailbox,subscribed) VALUES(?,?,0)`, user, path); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mailbox_state(username,mailbox,uidvalidity,uidnext) VALUES(?,?,?,1) ON CONFLICT(username,mailbox) DO UPDATE SET uidvalidity=uidvalidity+1,uidnext=1`, user, path, time.Now().Unix()); err != nil {
		return "", err
	}
	if err = mailboxAttributes(ctx, tx, user, path, p); err != nil {
		return "", err
	}
	var id string
	if err = tx.QueryRowContext(ctx, `SELECT jmap_id FROM mailbox_catalog WHERE username=? AND mailbox=?`, user, path).Scan(&id); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// SetMailboxes serializes the state check and batch with all protocol writers.
// Each object is atomic; independent objects may succeed after another fails.
func (s *MailboxStore) SetMailboxes(ctx context.Context, user, since string, create, update map[string]mailstate.MailboxPatch, destroy []string) (mailstate.MailboxSetResult, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	out := mailstate.MailboxSetResult{Created: map[string]string{}, Updated: []string{}, Destroyed: []string{}, NotCreated: map[string]string{}, NotUpdated: map[string]string{}, NotDestroyed: map[string]string{}}
	if err := s.ensureUser(ctx, user); err != nil {
		return out, err
	}
	if err := s.ensureMailboxSync(ctx, user); err != nil {
		return out, err
	}
	prefix, n, _, err := syncMailboxState(ctx, s.db, user)
	if err != nil {
		return out, err
	}
	out.OldState = stateToken(prefix, n)
	if since != "" && since != out.OldState {
		return out, mailstate.ErrStateMismatch
	}
	pending := map[string]mailstate.MailboxPatch{}
	for id, p := range create {
		pending[id] = p
	}
	for len(pending) > 0 {
		keys := []string{}
		for id := range pending {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		progress := false
		for _, id := range keys {
			p := pending[id]
			if p.Parent != nil && strings.HasPrefix(*p.Parent, "#") {
				ref := strings.TrimPrefix(*p.Parent, "#")
				if _, wait := pending[ref]; wait {
					continue
				}
				resolved, ok := out.Created[ref]
				if !ok {
					out.NotCreated[id] = "invalidProperties"
					delete(pending, id)
					progress = true
					continue
				}
				p.Parent = &resolved
			}
			delete(pending, id)
			progress = true
			path, kind := s.patchedMailboxPath(ctx, user, "", p)
			if kind == "" {
				var count int
				err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM mailbox_catalog WHERE username=? AND mailbox=?`, user, path).Scan(&count)
				if err != nil {
					kind = "serverFail"
				} else if count > 0 {
					kind = "invalidProperties"
				}
			}
			if kind != "" {
				out.NotCreated[id] = kind
				continue
			}
			created, e := s.createJMAPMailbox(ctx, user, path, p)
			if e != nil {
				out.NotCreated[id] = "serverFail"
			} else {
				out.Created[id] = created
			}
		}
		if !progress {
			for id := range pending {
				out.NotCreated[id] = "invalidProperties"
			}
			break
		}
	}
	doomed := map[string]bool{}
	for _, id := range destroy {
		doomed[id] = true
	}
	keys := []string{}
	for id := range update {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		if doomed[id] {
			out.NotUpdated[id] = "willDestroy"
			continue
		}
		p := update[id]
		if p.Parent != nil && strings.HasPrefix(*p.Parent, "#") {
			resolved, ok := out.Created[strings.TrimPrefix(*p.Parent, "#")]
			if !ok {
				out.NotUpdated[id] = "invalidProperties"
				continue
			}
			p.Parent = &resolved
		}
		old, e := s.mailboxPath(ctx, user, id)
		if e != nil {
			out.NotUpdated[id] = "serverFail"
			if e == sql.ErrNoRows {
				out.NotUpdated[id] = "notFound"
			}
			continue
		}
		path, kind := s.patchedMailboxPath(ctx, user, old, p)
		if kind != "" {
			out.NotUpdated[id] = kind
			continue
		}
		if path != old {
			// Every descendant must remain within the advertised path/depth bounds.
			var count int
			e = s.db.QueryRowContext(ctx, `SELECT count(*) FROM mailbox_catalog WHERE username=? AND (mailbox=? OR (substr(mailbox,1,?)=? AND (length(CAST(?||substr(mailbox,?) AS BLOB))>255 OR length(?||substr(mailbox,?))-length(replace(?||substr(mailbox,?),'/',''))>=10)))`, user, path, utf8.RuneCountInString(old)+1, old+"/", path, utf8.RuneCountInString(old)+1, path, utf8.RuneCountInString(old)+1, path, utf8.RuneCountInString(old)+1).Scan(&count)
			if e != nil {
				out.NotUpdated[id] = "serverFail"
				continue
			}
			if count > 0 {
				out.NotUpdated[id] = "invalidProperties"
				continue
			}
			e = s.renameFolder(ctx, user, old, path, func(tx *sql.Tx) error { return mailboxAttributes(ctx, tx, user, path, p) })
		} else {
			var tx *sql.Tx
			tx, e = s.db.BeginTx(ctx, nil)
			if e == nil {
				e = mailboxAttributes(ctx, tx, user, path, p)
				if e == nil {
					e = tx.Commit()
				}
				tx.Rollback()
			}
		}
		if e != nil {
			out.NotUpdated[id] = "serverFail"
		} else {
			out.Updated = append(out.Updated, id)
		}
	}
	// Delete children before parents, irrespective of request order.
	paths := map[string]string{}
	keys = nil
	for id := range doomed {
		path, e := s.mailboxPath(ctx, user, id)
		if e != nil {
			out.NotDestroyed[id] = "serverFail"
			if e == sql.ErrNoRows {
				out.NotDestroyed[id] = "notFound"
			}
			continue
		}
		paths[id] = path
		keys = append(keys, id)
	}
	sort.Slice(keys, func(i, j int) bool { return len(paths[keys[i]]) > len(paths[keys[j]]) })
	for _, id := range keys {
		path := paths[id]
		if path == "INBOX" {
			out.NotDestroyed[id] = "forbidden"
			continue
		}
		var children, messages int
		e := s.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM mailbox_catalog WHERE username=? AND substr(mailbox,1,?)=?),(SELECT count(*) FROM message_flags WHERE username=? AND mailbox=? AND expunged=0)`, user, utf8.RuneCountInString(path)+1, path+"/", user, path).Scan(&children, &messages)
		if e != nil {
			out.NotDestroyed[id] = "serverFail"
		} else if children > 0 {
			out.NotDestroyed[id] = "mailboxHasChild"
		} else if messages > 0 {
			out.NotDestroyed[id] = "mailboxHasEmail"
		} else if e = s.deleteFolder(ctx, user, path, false); e != nil {
			out.NotDestroyed[id] = "serverFail"
		} else {
			out.Destroyed = append(out.Destroyed, id)
		}
	}
	prefix, n, _, err = syncMailboxState(ctx, s.db, user)
	out.NewState = stateToken(prefix, n)
	return out, err
}
