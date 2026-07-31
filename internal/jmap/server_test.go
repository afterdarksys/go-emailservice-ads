package jmap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

func TestProcessRequestReturnsOpaqueSessionState(t *testing.T) {
	server := &JMAPServer{logger: zap.NewNop()}
	response := server.processRequest(context.Background(), &Request{
		Using: []string{"urn:ietf:params:jmap:core"},
		MethodCalls: []MethodCall{{
			Name: "unknown",
			ID:   "a",
		}},
	})

	if response.SessionState != "0" {
		t.Fatalf("sessionState = %q, want opaque initial state 0", response.SessionState)
	}
}

func TestEmailGetDoesNotReturnAnotherUsersMessage(t *testing.T) {
	store, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatalf("NewMessageStore() error = %v", err)
	}
	defer store.Close()

	adapter := storage.NewIMAPAdapter(store)
	messageID, err := adapter.StoreMessage(context.Background(), "alice", "INBOX", []byte("Subject: private\r\n\r\nbody"))
	if err != nil {
		t.Fatalf("StoreMessage() error = %v", err)
	}

	server := &JMAPServer{logger: zap.NewNop(), store: adapter}
	response := server.handleEmailGet(context.Background(), "bob", map[string]interface{}{
		"ids": []interface{}{messageID},
	}, "a")

	if response.Name != "Email/get" {
		t.Fatalf("response name = %q, want Email/get", response.Name)
	}
	notFound, ok := response.Arguments["notFound"].([]string)
	if !ok || len(notFound) != 1 || notFound[0] != messageID {
		t.Fatalf("notFound = %#v, want [%q]", response.Arguments["notFound"], messageID)
	}
}

func TestHandleJMAPAPIRejectsTooManyMethodCalls(t *testing.T) {
	calls := make([]MethodCall, maxJMAPCalls+1)
	for i := range calls {
		calls[i] = MethodCall{Name: "unknown", ID: "call"}
	}
	body, err := json.Marshal(Request{MethodCalls: calls})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/jmap/", strings.NewReader(string(body)))
	recorder := httptest.NewRecorder()
	(&JMAPServer{logger: zap.NewNop(), requestSem: make(chan struct{}, maxJMAPConcurrent)}).handleJMAPAPI(recorder, req)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandleJMAPAPIRejectsConcurrentRequestsOverAdvertisedLimit(t *testing.T) {
	server := &JMAPServer{logger: zap.NewNop(), requestSem: make(chan struct{}, maxJMAPConcurrent)}
	for i := 0; i < cap(server.requestSem); i++ {
		server.requestSem <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(server.requestSem); i++ {
			<-server.requestSem
		}
	}()

	req := httptest.NewRequest(http.MethodPost, "/jmap/", strings.NewReader(`{"using":[],"methodCalls":[]}`))
	resp := httptest.NewRecorder()
	server.handleJMAPAPI(resp, req)
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusTooManyRequests)
	}
}
