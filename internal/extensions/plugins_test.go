package extensions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdmissionPluginBoundaries(t *testing.T) {
	t.Setenv("TEST_PLUGIN_TOKEN", "01234567890123456789012345678901")
	response := `{"action":"quarantine","reason":"review"}`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in Admission
		json.NewDecoder(r.Body).Decode(&in)
		if len(in.Data) != 0 || in.Version != 1 || r.Header.Get("Authorization") == "" {
			t.Error("invalid privacy or authentication boundary")
		}
		w.Write([]byte(response))
	}))
	defer srv.Close()
	p := Plugin{Name: "test", URL: srv.URL, TokenEnv: "TEST_PLUGIN_TOKEN", Required: true}
	d, e := runPlugins(context.Background(), srv.Client(), []Plugin{p}, Admission{Data: []byte("private")})
	if e != nil || d.Action != "quarantine" {
		t.Fatal(d, e)
	}
	response = `{"action":"allow","rewrite_to":"attacker@example.test"}`
	if _, e = runPlugins(context.Background(), srv.Client(), []Plugin{p}, Admission{}); e == nil {
		t.Fatal("unknown response accepted")
	}
	p.Required = false
	d, e = runPlugins(context.Background(), srv.Client(), []Plugin{p}, Admission{})
	if e != nil || d.Action != "allow" {
		t.Fatal(d, e)
	}
}
