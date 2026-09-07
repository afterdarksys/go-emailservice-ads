package storage

import (
	"context"
	"fmt"

	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
	"github.com/emersion/go-imap/backend"
)

// MoveMessageIDs changes membership and destination UIDs in one SQLite commit.
// Payload identity, flags and dates are retained; user-wide quota usage is unchanged.
func (s *MailboxStore) MoveMessageIDs(ctx context.Context, user, source, dest string, ids []string) error {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	var err error
	if source, err = imap.NormalizeMailbox(source); err != nil {
		return err
	}
	if dest, err = imap.NormalizeMailbox(dest); err != nil {
		return err
	}
	if err = s.folderExists(ctx, user, source); err != nil {
		return fmt.Errorf("source mailbox unavailable: %v", err)
	}
	if err = s.folderExists(ctx, user, dest); err != nil {
		return err
	}
	before, err := s.GetMessages(ctx, user, source)
	if err != nil {
		return err
	}
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	// Use source UID order, regardless of caller ordering or duplicate IDs.
	ordered := make([]string, 0, len(selected))
	for _, message := range before {
		if selected[message.ID] {
			ordered = append(ordered, message.ID)
		}
	}
	if len(ordered) != len(selected) {
		return fmt.Errorf("move selection contains unavailable messages")
	}
	if len(ordered) == 0 {
		return nil
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
	if next+uint64(len(ordered)) > uint64(^uint32(0)) {
		return fmt.Errorf("mailbox UID space exhausted")
	}
	for _, id := range ordered {
		result, err := tx.ExecContext(ctx, `UPDATE message_flags SET mailbox=?,uid=? WHERE username=? AND mailbox=? AND msg_id=? AND expunged=0`, dest, next, user, source, id)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("move selection changed")
		}
		next++
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mailbox_state SET uidnext=? WHERE username=? AND mailbox=?`, next, user, dest); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// Descending sequence numbers refer to the source snapshot before any removal.
	for index := len(before) - 1; index >= 0; index-- {
		if selected[before[index].ID] {
			s.publish(&backend.ExpungeUpdate{Update: backend.NewUpdate(user, source), SeqNum: uint32(index + 1)})
		}
	}
	select {
	case s.deliveryCh <- [2]string{user, dest}:
	default:
	}
	s.notifyMailbox(user, dest)
	return nil
}
