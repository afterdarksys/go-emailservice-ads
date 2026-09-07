package auth

import (
	"context"
	"go.uber.org/zap"
	"path/filepath"
	"testing"
)

func TestPersonalDataIncludesMetadataWithoutCredentials(t *testing.T) {
	ctx := context.Background()
	r, err := NewUserRepository(filepath.Join(t.TempDir(), "users.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, name := range []string{"alice", "bob"} {
		if err = r.SaveUser(ctx, &User{Username: name, Email: name + "@example.test", PasswordHash: "secret-hash"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = r.db.Exec(`UPDATE users SET metadata='{"case":"123"}', last_login=CURRENT_TIMESTAMP WHERE username='alice'`); err != nil {
		t.Fatal(err)
	}
	if _, err = r.db.Exec(`INSERT INTO user_domain_entitlements(username,domain,granted_by,notes) VALUES('alice','example.test','operator','requested access')`); err != nil {
		t.Fatal(err)
	}
	data, err := r.PersonalData(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(data["users"]) != 1 {
		t.Fatal(data)
	}
	u := data["users"][0]
	if _, ok := u["password_hash"]; ok {
		t.Fatal("credential exported")
	}
	if u["metadata"] != `{"case":"123"}` || u["created_at"] == nil || u["last_login"] == nil {
		t.Fatal(u)
	}
	if len(data["user_domain_entitlements"]) != 1 || data["user_domain_entitlements"][0]["notes"] != "requested access" {
		t.Fatal(data)
	}
}
