package api

import (
	"encoding/json"
	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"go.uber.org/zap"
	"net/http"
	"path/filepath"
	"testing"
)

func TestSCIMLifecyclePersistenceAndScope(t *testing.T) {
	s, users := newMailboxTestServer(t, false)
	repo, err := auth.NewUserRepository(filepath.Join(t.TempDir(), "users.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err = users.SetRepository(repo); err != nil {
		t.Fatal(err)
	}
	body := map[string]interface{}{"schemas": []string{scimUserSchema}, "userName": "alice@example.test", "externalId": "directory-123", "active": true}
	if r := doMailboxRequest(s, "POST", "/api/v1/scim/v2/Users", testAPIKey, body); r.Code != 403 {
		t.Fatal("mailbox scope granted SCIM", r.Code)
	}
	s.config.API.APIKeys[0].Permissions = []string{"scim:read", "scim:write"}
	r := doMailboxRequest(s, "POST", "/api/v1/scim/v2/Users", testAPIKey, body)
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	var obj map[string]interface{}
	json.Unmarshal(r.Body.Bytes(), &obj)
	id := obj["id"].(string)
	if _, err = users.Authenticate("alice@example.test", ""); err == nil {
		t.Fatal("empty credential allowed")
	}
	if r := doMailboxRequest(s, "POST", "/api/v1/scim/v2/Users", testAPIKey, body); r.Code != 409 {
		t.Fatal("duplicate", r.Code)
	}
	patch := map[string]interface{}{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"}, "Operations": []map[string]interface{}{{"op": "Replace", "path": "active", "value": false}}}
	r = doMailboxRequest(s, "PATCH", "/api/v1/scim/v2/Users/"+id, testAPIKey, patch)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	restored := auth.NewUserStore()
	if err = restored.SetRepository(repo); err != nil {
		t.Fatal(err)
	}
	u, ok := restored.GetUser("alice@example.test")
	if !ok || u.Enabled || u.SCIMID != id || u.ExternalID != "directory-123" {
		t.Fatal(u, ok)
	}
	s.userStore = restored
	r = doMailboxRequest(s, http.MethodGet, "/api/v1/scim/v2/Users?filter=userName%20eq%20%22alice@example.test%22", testAPIKey, nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	json.Unmarshal(r.Body.Bytes(), &obj)
	if obj["totalResults"] != float64(1) {
		t.Fatal(obj)
	}
	r = doMailboxRequest(s, "DELETE", "/api/v1/scim/v2/Users/"+id, testAPIKey, nil)
	if r.Code != 204 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = doMailboxRequest(s, "GET", "/api/v1/scim/v2/Users/"+id, testAPIKey, nil); r.Code != 404 {
		t.Fatal(r.Code)
	}
}
