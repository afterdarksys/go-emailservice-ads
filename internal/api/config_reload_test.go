package api

import (
	"errors"
	"testing"
)

func TestConfigReloadValidationAndPermission(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	calls := 0
	s.SetConfigReload(func() error { calls++; return errors.New("invalid") })
	if r := doMailboxRequest(s, "POST", "/api/v1/config/reload", testAPIKey, nil); r.Code != 403 || calls != 0 {
		t.Fatal(r.Code, calls)
	}
	s.config.API.APIKeys[0].Permissions = []string{"config:write"}
	if r := doMailboxRequest(s, "POST", "/api/v1/config/reload", testAPIKey, nil); r.Code != 409 || calls != 1 {
		t.Fatal(r.Code, calls)
	}
	s.SetConfigReload(func() error { calls++; return nil })
	if r := doMailboxRequest(s, "POST", "/api/v1/config/reload", testAPIKey, nil); r.Code != 202 {
		t.Fatal(r.Code)
	}
}
