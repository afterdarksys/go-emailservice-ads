package jmap

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPushInitialStateIsolationAndLimits(t *testing.T) {
	j, _, _ := compositionServer(t)
	request := httptest.NewRequest("GET", "/jmap/events/?types=Email&closeafter=state&ping=0", nil)
	request = request.WithContext(context.WithValue(request.Context(), authUserKey, "alice"))
	w := httptest.NewRecorder()
	j.handleEvents(w, request)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "event: state") || strings.Contains(w.Body.String(), "EmailSubmission") {
		t.Fatal(w.Code, w.Body.String())
	}
	token := strings.Split(strings.TrimPrefix(w.Body.String(), "id: "), "\n")[0]
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(ctx)
	request.Header.Set("Last-Event-ID", token)
	w = httptest.NewRecorder()
	j.handleEvents(w, request)
	if strings.Contains(w.Body.String(), "event: state") {
		t.Fatal("replayed unchanged state")
	}
	releases := []func(){}
	for i := 0; i < 4; i++ {
		r, ok := j.acquirePush("alice")
		if !ok {
			t.Fatal(i)
		}
		releases = append(releases, r)
	}
	if _, ok := j.acquirePush("alice"); ok {
		t.Fatal("connection cap bypassed")
	}
	for _, r := range releases {
		r()
	}
	if j.pushTotal != 0 || len(j.pushUsers) != 0 {
		t.Fatal("connection leak")
	}
}
