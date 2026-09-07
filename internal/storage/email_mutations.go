package storage

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"github.com/emersion/go-imap/backend"
	"go.uber.org/zap"
)

func patchedEmailFlags(flags []string, p mailstate.Patch) ([]string, error) {
	if p.Replace != nil {
		preserved := []string{}
		for _, f := range flags {
			if strings.EqualFold(f, `\Deleted`) || strings.EqualFold(f, `\Recent`) {
				preserved = append(preserved, f)
			}
		}
		flags = append(preserved, (*p.Replace)...)
	}
	// Preserve existing order so membership-only and repeated updates do not
	// advance state merely because map iteration reordered identical flags.
	return validFlags(subtractFlags(append(flags, p.Add...), p.Remove))
}

type emailNotice struct {
	id, source, dest string
	flags            []string
	destroy          bool
}

func setEmail(ctx context.Context, tx *sql.Tx, user, id string, p mailstate.EmailPatch, destroy bool) (emailNotice, string, error) {
	n := emailNotice{id: id, destroy: destroy}
	var value, current string
	err := tx.QueryRowContext(ctx, `SELECT m.mailbox,m.flags,c.jmap_id FROM message_flags m JOIN mailbox_catalog c ON c.username=m.username AND c.mailbox=m.mailbox WHERE m.username=? AND m.msg_id=? AND m.expunged=0`, user, id).Scan(&n.source, &value, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return n, "notFound", nil
	}
	if err != nil {
		return n, "", err
	}
	if destroy {
		_, err = tx.ExecContext(ctx, `UPDATE message_flags SET expunged=1 WHERE username=? AND msg_id=? AND expunged=0`, user, id)
		return n, "", err
	}
	members := map[string]bool{current: true}
	if p.Mailboxes != nil {
		members = map[string]bool{}
		for _, mid := range *p.Mailboxes {
			members[mid] = true
		}
	}
	for _, mid := range p.RemoveMailboxes {
		delete(members, mid)
	}
	for _, mid := range p.AddMailboxes {
		members[mid] = true
	}
	if len(members) == 0 {
		return n, "invalidProperties", nil
	}
	if len(members) > 1 {
		return n, "tooManyMailboxes", nil
	}
	var target string
	for mid := range members {
		target = mid
	}
	err = tx.QueryRowContext(ctx, `SELECT mailbox FROM mailbox_catalog WHERE username=? AND jmap_id=?`, user, target).Scan(&n.dest)
	if errors.Is(err, sql.ErrNoRows) {
		return n, "invalidProperties", nil
	}
	if err != nil {
		return n, "", err
	}
	n.flags, err = patchedEmailFlags(strings.Fields(value), p.Keywords)
	if err != nil {
		return n, "invalidProperties", nil
	}
	if n.source != n.dest {
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO mailbox_state(username,mailbox,uidvalidity,uidnext) VALUES(?,?,?,1)`, user, n.dest, time.Now().Unix())
		if err != nil {
			return n, "", err
		}
		var next uint64
		if err = tx.QueryRowContext(ctx, `SELECT uidnext FROM mailbox_state WHERE username=? AND mailbox=?`, user, n.dest).Scan(&next); err != nil {
			return n, "", err
		}
		if next >= uint64(^uint32(0)) {
			return n, "overQuota", nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE message_flags SET mailbox=?,uid=?,flags=? WHERE username=? AND msg_id=? AND expunged=0`, n.dest, next, strings.Join(n.flags, " "), user, id)
		if err != nil {
			return n, "", err
		}
		_, err = tx.ExecContext(ctx, `UPDATE mailbox_state SET uidnext=? WHERE username=? AND mailbox=?`, next+1, user, n.dest)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE message_flags SET flags=? WHERE username=? AND msg_id=? AND expunged=0`, strings.Join(n.flags, " "), user, id)
	}
	return n, "", err
}

// SetEmails checks state under the common writer lock. Savepoints keep every
// email's combined move/keyword change atomic while permitting partial success.
func (s *MailboxStore) SetEmails(ctx context.Context, user, since string, patches map[string]mailstate.EmailPatch, destroy []string) (mailstate.EmailSetResult, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	out := mailstate.EmailSetResult{Updated: []string{}, Destroyed: []string{}, NotUpdated: map[string]string{}, NotDestroyed: map[string]string{}}
	if err := s.ensureUser(ctx, user); err != nil {
		return out, err
	}
	if err := s.ensureEmailSync(ctx, user); err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	prefix, rev, _, err := syncState(ctx, tx, user)
	if err != nil {
		return out, err
	}
	out.OldState = stateToken(prefix, rev)
	if since != "" && since != out.OldState {
		return out, mailstate.ErrStateMismatch
	}
	// Capture original sequence numbers before any removal, including cross moves.
	before := map[string][]string{}
	rows, err := tx.QueryContext(ctx, `SELECT msg_id,mailbox FROM message_flags WHERE username=? AND expunged=0 ORDER BY mailbox,uid`, user)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id, folder string
		if err = rows.Scan(&id, &folder); err != nil {
			rows.Close()
			return out, err
		}
		before[folder] = append(before[folder], id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	doomed := map[string]bool{}
	for _, id := range destroy {
		doomed[id] = true
	}
	ids := []string{}
	for id := range patches {
		ids = append(ids, id)
	}
	for id := range doomed {
		if _, ok := patches[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	notices := []emailNotice{}
	for _, id := range ids {
		deleting := doomed[id]
		if deleting {
			if _, ok := patches[id]; ok {
				out.NotUpdated[id] = "willDestroy"
			}
		}
		if _, err = tx.ExecContext(ctx, `SAVEPOINT email_object`); err != nil {
			return out, err
		}
		notice, kind, e := setEmail(ctx, tx, user, id, patches[id], deleting)
		if e != nil {
			kind = "serverFail"
		}
		if kind != "" {
			if _, err = tx.ExecContext(ctx, `ROLLBACK TO email_object`); err != nil {
				return out, err
			}
			if deleting {
				out.NotDestroyed[id] = kind
			} else {
				out.NotUpdated[id] = kind
			}
		}
		if _, err = tx.ExecContext(ctx, `RELEASE email_object`); err != nil {
			return out, err
		}
		if kind == "" {
			notices = append(notices, notice)
			if deleting {
				out.Destroyed = append(out.Destroyed, id)
			} else {
				out.Updated = append(out.Updated, id)
			}
		}
	}
	prefix, rev, _, err = syncState(ctx, tx, user)
	if err != nil {
		return out, err
	}
	out.NewState = stateToken(prefix, rev)
	if err = tx.Commit(); err != nil {
		return out, err
	}
	removed, arrivals := map[string]bool{}, map[string]bool{}
	for _, n := range notices {
		if n.destroy || n.source != n.dest {
			removed[n.id] = true
		}
		if !n.destroy && n.source != n.dest {
			arrivals[n.dest] = true
		}
	}
	folders := []string{}
	for folder := range before {
		folders = append(folders, folder)
	}
	sort.Strings(folders)
	for _, folder := range folders {
		messages := before[folder]
		for i := len(messages) - 1; i >= 0; i-- {
			if removed[messages[i]] {
				s.publish(&backend.ExpungeUpdate{Update: backend.NewUpdate(user, folder), SeqNum: uint32(i + 1)})
			}
		}
	}
	for folder := range arrivals {
		select {
		case s.deliveryCh <- [2]string{user, folder}:
		default:
		}
		s.notifyMailbox(user, folder)
	}
	for _, n := range notices {
		if !n.destroy {
			s.notifyFlags(ctx, user, n.dest, n.id, n.flags)
		}
	}
	// Membership is authoritative; a failed payload tombstone is retried on restart.
	// Never report a committed deletion as failed because physical cleanup failed.
	if len(out.Destroyed) > 0 {
		if err = s.cleanupExpunged(context.Background()); err != nil {
			s.adapter.store.logger.Warn("Deferred JMAP deletion payload cleanup", zap.Error(err))
		}
	}
	return out, nil
}
