package storage

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"strings"
	"testing"
)

func BenchmarkThreadMetadata64KiB(b *testing.B) {
	raw := []byte("Message-ID: <root@example.test>\r\n\r\n" + strings.Repeat("x", 64*1024))
	s := &MessageStore{index: map[string]*JournalEntry{"message": {MessageID: "message", Data: raw}}}
	b.Run("copy_body_and_parse", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			e, err := s.Get("message")
			if err != nil {
				b.Fatal(err)
			}
			mailstate.ThreadID("message", e.Data)
		}
	})
	b.Run("cached_header_identity", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := s.ThreadIdentity("message"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
