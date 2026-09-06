package filtering

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestScannerVerdictsAndInvalidResponses(t *testing.T) {
	for _, raw := range []string{`{"action":"reject"}`, `{"action":"unknown"}`, `not json`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/checkv2" || r.Header.Get("IP") != "192.0.2.1" {
				t.Error("wrong scanner request")
			}
			w.Write([]byte(raw))
		}))
		v, e := Scan(context.Background(), s.URL, "a@test", "192.0.2.1", []string{"b@test"}, []byte("body"))
		s.Close()
		if raw == `{"action":"reject"}` {
			if e != nil || v.Action != "reject" {
				t.Fatal(e)
			}
		} else if e == nil {
			t.Fatal("invalid scanner response accepted")
		}
	}
}
func TestARCHeadersAreOrderedAndRejectInjection(t *testing.T) {
	raw := `{"action":"no action","milter":{"add_headers":{"ARC-Seal":{"value":"i=1; b=seal","order":0},"ARC-Message-Signature":{"value":"i=1; b=sig","order":0},"ARC-Authentication-Results":{"value":"i=1; test; dkim=pass","order":0}}}}`
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(raw)) }))
	defer s.Close()
	v, _, e := ScanFinal(context.Background(), s.URL, "a@test", "192.0.2.1", "helo", "user", []string{"b@test"}, []byte("Subject: test\r\n\r\nbody"), true)
	if e != nil || len(v.Headers) != 3 || v.Headers[0].Name != "ARC-Authentication-Results" || v.Headers[2].Name != "ARC-Seal" {
		t.Fatalf("wrong ARC prepend order: %+v %v", v, e)
	}
	raw = `{"action":"no action","milter":{"add_headers":{"X-Test":{"value":"ok\r\nInjected: bad","order":0}}}}`
	if _, _, e = ScanFinal(context.Background(), s.URL, "a@test", "192.0.2.1", "helo", "", nil, nil, false); e == nil {
		t.Fatal("header injection accepted")
	}
}

func TestScannerReplacementIsNotModifiedTwice(t *testing.T) {
	payload := []byte(`{"action":"no action","milter":{"add_headers":{"ARC-Seal":{"value":"i=1; b=sig","order":0}}}}`)
	message := []byte("ARC-Seal: i=1; b=sig\r\nSubject: signed\r\n\r\nbody")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Message-Offset", strconv.Itoa(len(payload)))
		w.Write(payload)
		w.Write(message)
	}))
	defer srv.Close()
	v, got, err := ScanFinal(context.Background(), srv.URL, "a@test", "192.0.2.1", "helo", "", nil, message, true)
	if err != nil || len(v.Headers) != 0 || !bytes.Equal(got, message) {
		t.Fatalf("replacement mutated: %v", err)
	}
}
func TestScannerHeaderRemovalAndPosition(t *testing.T) {
	raw := []byte("X-Test: first\r\n folded\r\nSubject: keep\r\nX-Test: last\r\n\r\nX-Test: body\r\n")
	got, err := ApplyHeaders(raw, Verdict{RemoveHeaders: map[string]int{"X-Test": -1}, Headers: []Header{{Name: "X-New", Value: "new", Order: 1}}})
	want := []byte("X-Test: first\r\n folded\r\nX-New: new\r\nSubject: keep\r\n\r\nX-Test: body\r\n")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("bad header application: %q %v", got, err)
	}
}
