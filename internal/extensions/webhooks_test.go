package extensions

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestWebhookDurabilitySignatureAndRetry(t *testing.T) {
	t.Setenv("TEST_HOOK_SECRET", "01234567890123456789012345678901")
	ctx := context.Background()
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("01234567890123456789012345678901"))
		mac.Write([]byte(r.Header.Get("X-Mailhub-Timestamp") + "." + string(body)))
		if r.Header.Get("X-Mailhub-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			t.Error("bad signature")
		}
		if calls == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()
	hooks := []Webhook{{Name: "audit", URL: srv.URL, SecretEnv: "TEST_HOOK_SECRET"}}
	path := filepath.Join(t.TempDir(), "webhooks.db")
	o, e := OpenOutbox(path, hooks)
	if e != nil {
		t.Fatal(e)
	}
	id, e := o.Begin(ctx, "operator", "POST", "/api/v1/mailboxes")
	if e != nil {
		t.Fatal(e)
	}
	if e = o.Finish(ctx, id, 201); e != nil {
		t.Fatal(e)
	}
	o.Close()
	o, e = OpenOutbox(path, hooks)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	o.client = srv.Client()
	if e = o.Dispatch(ctx); e != nil {
		t.Fatal(e)
	}
	stats, _ := o.Stats(ctx)
	if stats["pending"] != 1 {
		t.Fatal(stats)
	}
	o.db.Exec(`UPDATE deliveries SET due=0`)
	if e = o.Dispatch(ctx); e != nil {
		t.Fatal(e)
	}
	stats, _ = o.Stats(ctx)
	if stats["sent"] != 1 {
		t.Fatal(stats)
	}
	if e = o.Dispatch(ctx); e != nil || calls != 2 {
		t.Fatal(e, calls)
	}
}
