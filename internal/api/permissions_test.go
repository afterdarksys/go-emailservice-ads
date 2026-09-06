package api

import (
	"net/http"
	"testing"
)

func TestReadKeyCannotModifyOrUseLegacyMutatingGET(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	s.config.API.APIKeys[0].Permissions = []string{"mailboxes:read", "queue:read"}
	for _, r := range []*http.Request{{Method: "POST"}, {Method: "DELETE"}} {
		r2, _ := http.NewRequest(r.Method, "http://local/api/v1/mailboxes/user", nil)
		if _, ok := s.authorizeKey(testAPIKey, requiredScope(r2)); ok {
			t.Fatal("read key can write")
		}
	}
	r, _ := http.NewRequest("GET", "http://local/api/v1/dlq/retry/id", nil)
	if _, ok := s.authorizeKey(testAPIKey, requiredScope(r)); ok {
		t.Fatal("mutating GET bypass")
	}
	s.config.API.APIKeys[0].Permissions = nil
	if _, ok := s.authorizeKey(testAPIKey, "mailboxes:read"); ok {
		t.Fatal("empty scopes grant access")
	}
}
