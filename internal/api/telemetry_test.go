package api

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOperationalMetricsReadLiveQueue(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	store, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s.store = store
	if _, _, err = store.Store(&storage.JournalEntry{Data: []byte("abc"), Tier: "out", Status: "held", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.buildMux().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `mailhub_messages{state="held"} 1`) || !strings.Contains(w.Body.String(), "mailhub_queue_bytes 3") {
		t.Fatal(w.Body.String())
	}
}
