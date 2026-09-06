package delivery

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/dns"
	smtpd "github.com/emersion/go-smtp"
	"go.uber.org/zap"
)

type testBackend struct{ received atomic.Int32 }

func (b *testBackend) NewSession(*smtpd.Conn) (smtpd.Session, error) { return &testSession{b: b}, nil }

type testSession struct{ b *testBackend }

func (s *testSession) Reset()                                {}
func (s *testSession) Logout() error                         { return nil }
func (s *testSession) Mail(string, *smtpd.MailOptions) error { return nil }
func (s *testSession) Rcpt(to string, _ *smtpd.RcptOptions) error {
	if to == "temp@example.test" {
		return &smtpd.SMTPError{Code: 451, Message: "retry"}
	}
	if to == "perm@example.test" {
		return &smtpd.SMTPError{Code: 550, Message: "unknown"}
	}
	return nil
}
func (s *testSession) Data(r io.Reader) error {
	io.Copy(io.Discard, r)
	s.b.received.Add(1)
	return nil
}
func startTransport(t *testing.T, secure bool) (string, string, *testBackend) {
	t.Helper()
	b := &testBackend{}
	server := smtpd.NewServer(b)
	server.Domain = "example.test"
	ca := ""
	if secure {
		httpServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		cert := httpServer.TLS.Certificates[0]
		httpServer.Close()
		server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		ca = filepath.Join(t.TempDir(), "ca.pem")
		os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return listener.Addr().String(), ca, b
}
func TestRouteTLSFailoverAndRecipientOutcomes(t *testing.T) {
	addr, ca, b := startTransport(t, true)
	d := NewMailDelivery(zap.NewNop(), dns.NewResolver(zap.NewNop()), "sender.example.test")
	d.SetRoutes([]Route{{Domain: "example.test", NextHops: []NextHop{{Address: "127.0.0.1:1", RequireTLS: true}, {Address: addr, RequireTLS: true, ServerName: "example.com", CAFile: ca}}}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := d.Deliver(ctx, "sender@example.test", []string{"ok@example.test", "temp@example.test", "perm@example.test"}, []byte("Subject: test\r\n\r\nbody"))
	if e == nil || r == nil || len(r.Recipients) != 3 || b.received.Load() != 1 {
		t.Fatalf("result=%+v err=%v delivered=%d", r, e, b.received.Load())
	}
	if !r.Recipients[0].Success || r.Recipients[1].Success || !r.Recipients[2].IsPermanent {
		t.Fatalf("wrong recipient outcomes: %+v", r.Recipients)
	}
}
func TestRequiredConnectorTLSRejectsCleartext(t *testing.T) {
	addr, _, b := startTransport(t, false)
	d := NewMailDelivery(zap.NewNop(), dns.NewResolver(zap.NewNop()), "sender.test")
	d.SetRoutes([]Route{{Domain: "example.test", NextHops: []NextHop{{Address: addr, RequireTLS: true}}}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e := d.Deliver(ctx, "a@example.test", []string{"b@example.test"}, []byte("body")); e == nil || b.received.Load() != 0 {
		t.Fatal("TLS requirement bypassed")
	}
}
func TestWildcardDoesNotOverrideLocalDomain(t *testing.T) {
	d := NewMailDelivery(zap.NewNop(), nil, "local")
	d.SetRoutes([]Route{{Domain: "*", NextHops: []NextHop{{Address: "relay.test:25"}}}})
	if !d.HasRoute("example.test") || d.HasExplicitRoute("example.test") {
		t.Fatal("wildcard treated as local override")
	}
}
