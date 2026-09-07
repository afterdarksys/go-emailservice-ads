package api

import (
	"bytes"
	"context"
	"github.com/afterdarksys/go-emailservice-ads/internal/extensions"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestWebhookRecordingPreservesLargeMutationResponses(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	o, e := extensions.OpenOutbox(filepath.Join(t.TempDir(), "webhooks.db"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	s.outbox = o
	body := bytes.Repeat([]byte("x"), 4<<20)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/compliance/export", nil)
	s.recordMutation(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "message/rfc822")
		w.WriteHeader(200)
		w.Write(body)
	})(w, r)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), body) {
		t.Fatal("webhooks truncated export")
	}
	if _, e = o.Stats(context.Background()); e != nil {
		t.Fatal(e)
	}
}
