package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComplianceOAuthCannotBeBypassedByWildcardKey(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	s.config.API.APIKeys[0].Permissions = []string{"*"}
	s.config.API.OAuth.RequireForCompliance = true
	called := false
	handler := s.authMiddleware(func(http.ResponseWriter, *http.Request) { called = true })
	r := httptest.NewRequest("GET", "/api/v1/compliance", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	handler(w, r)
	if called || w.Code != 401 {
		t.Fatal("API key bypassed required OAuth")
	}
}
