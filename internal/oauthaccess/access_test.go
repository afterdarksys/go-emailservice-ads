package oauthaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntrospectionClaimsAndScope(t *testing.T) {
	t.Setenv("MAILHUB_TEST_OAUTH_SECRET", "secret")
	claims := map[string]any{"active": true, "sub": "officer", "iss": "https://issuer.test", "aud": []string{"mailhub"}, "exp": time.Now().Add(time.Minute).Unix(), "scope": "compliance:read"}
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "hub" || password != "secret" {
			t.Error("missing client authentication")
		}
		if r.Method != "POST" || r.FormValue("token") != "opaque" {
			t.Error("invalid introspection request")
		}
		json.NewEncoder(w).Encode(claims)
	}))
	defer endpoint.Close()
	cfg := Config{Enabled: true, IntrospectionURL: endpoint.URL, ClientID: "hub", ClientSecretEnv: "MAILHUB_TEST_OAUTH_SECRET", Issuer: "https://issuer.test", Audience: "mailhub"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if actor, err := validateToken(context.Background(), cfg, "opaque", "compliance:read", endpoint.Client()); err != nil || actor != "oauth:officer" {
		t.Fatal(actor, err)
	}
	if _, err := validateToken(context.Background(), cfg, "opaque", "compliance:write", endpoint.Client()); err == nil {
		t.Fatal("scope escalated")
	}
	for key, value := range map[string]any{"active": false, "iss": "wrong", "aud": "other", "exp": int64(1), "nbf": time.Now().Add(time.Hour).Unix()} {
		original, exists := claims[key]
		claims[key] = value
		if _, err := validateToken(context.Background(), cfg, "opaque", "compliance:read", endpoint.Client()); err == nil {
			t.Fatalf("invalid %s accepted", key)
		}
		if exists {
			claims[key] = original
		} else {
			delete(claims, key)
		}
	}
}
