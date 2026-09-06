package server

import (
	"bytes"
	"strings"
	"sync"
	"testing"

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
