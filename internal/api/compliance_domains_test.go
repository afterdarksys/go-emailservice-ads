package api

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/compliance"
	"github.com/afterdarksys/go-emailservice-ads/internal/smtpd"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComplianceAPIHidesOtherDomains(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	store, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s.store = store
	s.qm = &smtpd.QueueManager{}
	s.config.API.APIKeys[0].Name = "officer-a"
	s.config.API.APIKeys[0].Permissions = []string{"compliance:read", "compliance:export"}
	s.config.Platform.Compliance = compliance.Config{EnforceDomainAccess: true, Access: []compliance.Grant{{Principal: "officer-a", Domains: []string{"a.test"}, Actions: []string{"read", "export"}}}}
	for _, domain := range []string{"a.test", "b.test", "a.test,b.test"} {
		entry := &storage.JournalEntry{MessageID: domain, From: "sender@test", To: []string{"recipient@test"}, Data: []byte("private"), Tier: "compliance", Status: "compliance", Metadata: map[string]string{"compliance": "true", "domain": domain}}
		entry.Metadata["evidence_sha256"] = storage.EvidenceHash(entry.From, entry.To, entry.Data)
		if _, _, err := store.Store(entry); err != nil {
			t.Fatal(err)
		}
	}
	mux := s.buildMux()
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+testAPIKey)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	list := request("/api/v1/compliance")
	if list.Code != 200 || strings.Contains(list.Body.String(), "b.test") {
		t.Fatal("listing leaked another domain", list.Code, list.Body.String())
	}
	if w := request("/api/v1/compliance/b.test/export"); w.Code != 404 {
		t.Fatal("cross-domain export permitted", w.Code)
	}
	if w := request("/api/v1/compliance/a.test/export"); w.Code != 200 || w.Body.String() != "private" {
		t.Fatal("authorized export failed", w.Code)
	}
}
