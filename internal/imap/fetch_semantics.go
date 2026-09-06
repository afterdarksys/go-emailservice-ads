package imap

import (
	"context"
	"github.com/emersion/go-imap/server"
)

// go-imap's backend FETCH interface does not carry the selected read-only state.
// Bind it to this session's mailbox while handling FETCH/UID FETCH so EXAMINE
// cannot persist Seen flags, without changing the requested response sections.
type fetchSemantics struct{}

func (fetchSemantics) Capabilities(server.Conn) []string { return nil }
func (fetchSemantics) Command(name string) server.HandlerFactory {
	if name == "FETCH" {
		return func() server.Handler { return &safeFetch{} }
	}
	return nil
}

type safeFetch struct{ server.Fetch }

func (f *safeFetch) withState(conn server.Conn, uid bool) error {
	if mailbox, ok := conn.Context().Mailbox.(*Mailbox); ok {
		old := mailbox.readOnly
		mailbox.readOnly = conn.Context().MailboxReadOnly
		defer func() {
			mailbox.readOnly = old
			if notifier, ok := mailbox.store.(interface {
				NotifyMessageRead(context.Context, string, string, string) error
			}); ok {
				for _, id := range mailbox.pendingRead {
					_ = notifier.NotifyMessageRead(context.Background(), id, mailbox.username, mailbox.name)
				}
			}
			mailbox.pendingRead = nil
		}()
	}
	if uid {
		return f.Fetch.UidHandle(conn)
	}
	return f.Fetch.Handle(conn)
}
func (f *safeFetch) Handle(conn server.Conn) error    { return f.withState(conn, false) }
func (f *safeFetch) UidHandle(conn server.Conn) error { return f.withState(conn, true) }
