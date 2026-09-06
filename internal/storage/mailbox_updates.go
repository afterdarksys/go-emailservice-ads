package storage

import (
	"context"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
)

// Enabled only when an IMAP server subscribes. Storage-only callers need no
// event consumer. A full buffer applies backpressure instead of losing EXPUNGE.
func (s *MailboxStore) ProtocolUpdates() chan backend.Update {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	if s.protocolCh == nil {
		s.protocolCh = make(chan backend.Update, 256)
	}
	return s.protocolCh
}
func (s *MailboxStore) publish(update backend.Update) { s.publishWait(update, true) }
func (s *MailboxStore) publishWait(update backend.Update, wait bool) {
	s.eventMu.Lock()
	ch := s.protocolCh
	s.eventMu.Unlock()
	if ch == nil {
		return
	}
	done := update.Done()
	select {
	case ch <- update:
	case <-s.stopEvents:
		return
	}
	if !wait {
		return
	}
	select {
	case <-done:
	case <-s.stopEvents:
	}
}
func (s *MailboxStore) notifyMailbox(user, folder string) {
	messages, err := s.GetMessages(context.Background(), user, folder)
	if err != nil {
		return
	}
	s.publish(&backend.MailboxUpdate{Update: backend.NewUpdate(user, folder), MailboxStatus: &imap.MailboxStatus{Name: folder, Items: map[imap.StatusItem]interface{}{imap.StatusMessages: nil}, Messages: uint32(len(messages))}})
}
func (s *MailboxStore) notifyFlags(ctx context.Context, user, folder, id string, flags []string) {
	messages, err := s.GetMessages(context.Background(), user, folder)
	if err != nil {
		return
	}
	for i, msg := range messages {
		if msg.ID == id {
			async, _ := ctx.Value(fetchUpdateKey{}).(bool)
			message := imap.NewMessage(uint32(i+1), []imap.FetchItem{imap.FetchUid, imap.FetchFlags})
			message.Uid, message.Flags = msg.UID, flags
			s.publishWait(&backend.MessageUpdate{Update: backend.NewUpdate(user, folder), Message: message}, !async)
			return
		}
	}
}

type fetchUpdateKey struct{}

// FETCH holds the IMAP response writer while pulling body data. Queue the
// unsolicited update without waiting for that same writer, avoiding deadlock.
func (s *MailboxStore) MarkMessageRead(ctx context.Context, id, user, folder string) error {
	return s.UpdateMessageFlags(context.WithValue(ctx, fetchUpdateKey{}, true), id, user, folder, imap.AddFlags, []string{imap.SeenFlag})
}
