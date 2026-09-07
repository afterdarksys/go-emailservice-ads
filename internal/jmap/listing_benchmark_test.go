package jmap

import (
	"context"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
	"strings"
	"testing"
	"time"
)

type benchmarkListingStore struct {
	messages []imap.MessageSummary
	raw      []byte
	fetches  int
}

func (s *benchmarkListingStore) GetMessages(context.Context, string, string) ([]imap.MessageSummary, error) {
	return s.messages, nil
}
func (s *benchmarkListingStore) FetchMessage(context.Context, string) ([]byte, error) {
	s.fetches++
	return append([]byte(nil), s.raw...), nil
}
func BenchmarkMailboxListing1000x64KiB(b *testing.B) {
	s := &benchmarkListingStore{raw: []byte("Subject: benchmark\r\n\r\n" + strings.Repeat("x", 64*1024))}
	for i := 0; i < 1000; i++ {
		s.messages = append(s.messages, imap.MessageSummary{ID: fmt.Sprint(i), Date: time.Unix(int64(i), 0)})
	}
	j := &JMAPServer{store: s}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := j.emailQuery(context.Background(), "alice", map[string]interface{}{"limit": float64(50)}, "q")
		if r.Name == "error" {
			b.Fatal(r)
		}
	}
	b.ReportMetric(float64(s.fetches)/float64(b.N), "body_reads/op")
}

func TestMetadataListingDoesNotReadBodies(t *testing.T) {
	s := &benchmarkListingStore{messages: []imap.MessageSummary{{ID: "one", Date: time.Now()}}, raw: []byte("Subject: needle\r\n\r\nbody")}
	j := &JMAPServer{store: s}
	r := j.emailQuery(context.Background(), "alice", map[string]interface{}{}, "q")
	if r.Name == "error" || s.fetches != 0 {
		t.Fatal(r, s.fetches)
	}
	r = j.emailQuery(context.Background(), "alice", map[string]interface{}{"filter": map[string]interface{}{"subject": "needle"}}, "q")
	if r.Name == "error" || r.Arguments["total"] != 1 || s.fetches != 1 {
		t.Fatal(r, s.fetches)
	}
}
