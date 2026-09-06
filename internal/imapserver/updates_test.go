package server

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
)

func TestUpdateResponseForEveryRecipient(t *testing.T) {
	message := imap.NewMessage(2, []imap.FetchItem{imap.FetchUid, imap.FetchFlags})
	message.Uid, message.Flags = 7, []string{imap.SeenFlag}
	for _, tc := range []struct {
		name   string
		update backend.Update
		want   string
	}{
		{"flags", &backend.MessageUpdate{Message: message}, "* 2 FETCH (UID 7 FLAGS (\\Seen))\r\n"},
		{"expunge", &backend.ExpungeUpdate{SeqNum: 2}, "* 2 EXPUNGE\r\n"},
		{"list", &backend.MailboxInfoUpdate{MailboxInfo: &imap.MailboxInfo{Name: "Projects", Delimiter: "/"}}, "Projects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var wg sync.WaitGroup
			for n := 0; n < 8; n++ {
				response := updateResponse(tc.update)
				wg.Add(1)
				go func() {
					defer wg.Done()
					var data bytes.Buffer
					writer := imap.NewWriter(&data)
					if err := response.WriteTo(writer); err != nil {
						t.Error(err)
						return
					}
					if err := writer.Flush(); err != nil {
						t.Error(err)
						return
					}
					if !strings.Contains(data.String(), tc.want) {
						t.Errorf("recipient received %q, want %q", data.String(), tc.want)
					}
				}()
			}
			wg.Wait()
		})
	}
}

type departedConn struct {
	Conn
	ctx      Context
	isSilent bool
}

func (c *departedConn) Context() *Context { return &c.ctx }
func (c *departedConn) silent() *bool     { return &c.isSilent }

func TestUpdateCompletesAfterRecipientLogout(t *testing.T) {
	for _, stage := range []string{"before enqueue", "before write"} {
		t.Run(stage, func(t *testing.T) {
			responses := make(chan imap.WriterTo)
			loggedOut := make(chan struct{})
			c := &departedConn{ctx: Context{Responses: responses, LoggedOut: loggedOut}}
			updates := make(chan backend.Update, 1)
			s := &Server{conns: map[Conn]struct{}{c: {}}, Updates: updates}
			update := &backend.ExpungeUpdate{Update: backend.NewUpdate("", ""), SeqNum: 1}
			done := update.Done() // The backend initializes completion before publishing.
			updates <- update
			close(updates)
			if stage == "before enqueue" {
				close(loggedOut)
			}
			go s.listenUpdates()
			if stage == "before write" {
				select {
				case <-responses:
					close(loggedOut)
				case <-time.After(time.Second):
					t.Fatal("update not queued")
				}
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("departed recipient blocked update completion")
			}
		})
	}
}
