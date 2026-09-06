package api

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	"net/http/httptest"
	"testing"
)

func TestOrdinaryQueueReadCannotExportComplianceEvidence(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	s.config.API.APIKeys[0].Permissions = []string{"queue:read"}
	store, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s.store = store
	id, _, err := store.Store(&storage.JournalEntry{Data: []byte("private"), Status: "compliance", Tier: "compliance", Metadata: map[string]string{"compliance": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/message/"+id, nil)
	r.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	s.buildMux().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("evidence exposed: %d", w.Code)
	}
}
