package api

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"path/filepath"
	"testing"
)

func TestPolicyAPIChangesActiveAndPersistentPolicies(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	s.config.API.APIKeys[0].Permissions = []string{"policies:read", "policies:write"}
	m, err := policy.NewManager(&policy.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policies.yaml")
	if err = m.UsePersistentFile(path); err != nil {
		t.Fatal(err)
	}
	s.policyMgr = m
	w := doMailboxRequest(s, "POST", "/api/v1/policies", testAPIKey, map[string]interface{}{"name": "test", "type": "starlark", "enabled": true, "script": "reject(\"blocked\")", "scope": map[string]string{"type": "global"}})
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = doMailboxRequest(s, "POST", "/api/v1/policies/test/test", testAPIKey, map[string]interface{}{"From": "a@test", "To": []string{"b@test"}, "Subject": "test", "Body": "body"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	restored, err := policy.NewManager(&policy.ManagerConfig{ConfigPath: path})
	if err != nil || len(restored.ListPolicies()) != 1 {
		t.Fatal(err)
	}
	w = doMailboxRequest(s, "DELETE", "/api/v1/policies/test", testAPIKey, nil)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err = m.Reload(); err != nil || len(m.ListPolicies()) != 0 {
		t.Fatal(err)
	}
}
