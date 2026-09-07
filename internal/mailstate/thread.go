package mailstate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/mail"
	"regexp"
)

var threadMessageID = regexp.MustCompile(`<[^<>\s]+@[^<>\s]+>`)

// ThreadID uses the first References ID, then In-Reply-To, then Message-ID.
// It depends only on immutable message headers, so removing an ancestor never
// changes an existing thread ID. Subjects alone never merge unrelated mail.
func ThreadID(id string, raw []byte) string {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err == nil {
		for _, key := range []string{"References", "In-Reply-To", "Message-ID"} {
			if root := threadMessageID.FindString(m.Header.Get(key)); root != "" {
				return fmt.Sprintf("t:%x", sha256.Sum256([]byte(root)))
			}
		}
	}
	return id
}
