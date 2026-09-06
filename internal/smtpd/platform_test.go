package smtpd

import (
	"bytes"
	"context"
	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstorm"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"go.uber.org/zap"
	"net"
	"net/http"
	"net/http/httptest"
	stdsmtp "net/smtp"
	"path/filepath"
	"testing"
	"time"
)

func TestHeaderRemovalPreservesBodyAndUnrelatedFields(t *testing.T) {
	raw := []byte("Authentication-Results: forged\r\n folded\r\nSubject: keep\r\n\r\nAuthentication-Results: body text\r\n")
	want := []byte("Subject: keep\r\n\r\nAuthentication-Results: body text\r\n")
	if got := removeHeader(raw, "Authentication-Results"); !bytes.Equal(got, want) {
		t.Fatalf("corrupted message: %q", got)
	}
	if hasHeader([]byte("Subject: ok\r\n\r\nX-Quarantine: true\r\n"), "X-Quarantine", "true") {
		t.Fatal("body controls quarantine")
	}
}
func TestLiveRecipientAndAliasValidation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Platform.ValidateRecipients = true
	cfg.Server.LocalDomains = []string{"example.test"}
	cfg.Platform.Aliases = map[string][]string{"group@example.test": {"user@example.test"}}
	v := auth.NewValidator(zap.NewNop())
	s := &Session{config: cfg, validator: v}
	if _, e := s.resolveRecipient("group@example.test", map[string]bool{}); e == nil {
		t.Fatal("unknown alias target accepted")
	}
	v.GetUserStore().AddUser("login", "test-password-123", "user@example.test")
	if targets, e := s.resolveRecipient("group@example.test", map[string]bool{}); e != nil || len(targets) != 1 || targets[0] != "user@example.test" {
		t.Fatalf("live provisioning not visible: %v %v", targets, e)
	}
	disabled := false
	if err := v.GetUserStore().UpdateAccount("login", "new-password-123", "", &disabled); err != nil {
		t.Fatal(err)
	}
	if _, err := s.resolveRecipient("group@example.test", map[string]bool{}); err == nil {
		t.Fatal("disabled recipient accepted")
	}
	if err := v.GetUserStore().UpdateAccount("login", "another-password-123", "", nil); err != nil {
		t.Fatal(err)
	}
	if u, _ := v.GetUserStore().GetUser("login"); u.Enabled {
		t.Fatal("password change re-enabled user")
	}
	v.GetUserStore().DeleteUser("login")
	if _, e := s.resolveRecipient("group@example.test", map[string]bool{}); e == nil {
		t.Fatal("deleted recipient accepted")
	}
}
func TestHeldMessageNotDispatchedAndReleaseRescans(t *testing.T) {
	q, store := newPersistenceTestQueue(t)
	msg := &Message{From: "sender@example.test", To: []string{"a@example.test"}, Data: []byte("Subject: held\r\n\r\nbody"), Tier: TierOut, Quarantine: true, ClientIP: "192.0.2.4"}
	if e := q.Enqueue(msg); e != nil {
		t.Fatal(e)
	}
	if len(q.out) != 0 || len(store.ListByStatus("held", "")) != 1 {
		t.Fatal("held message dispatched")
	}
	scanner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("IP") != "192.0.2.4" {
			t.Error("rescan lost IP")
		}
		w.Write([]byte(`{"action":"no action"}`))
	}))
	defer scanner.Close()
	q.platform.ScannerURL = scanner.URL
	if e := q.ReleaseHeld(context.Background(), msg.ID, "admin"); e != nil {
		t.Fatal(e)
	}
	entry, _ := store.Get(msg.ID)
	if entry.Status != "pending" || messageFromEntry(entry).Quarantine {
		t.Fatal("release did not clear hold")
	}
}
func TestProxyIdentityKeepsPhysicalPeer(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		p := &lazyProxyConn{Conn: c, config: config.ProxyProtocolConfig{Networks: []string{"127.0.0.0/8"}}}
		var b [1]byte
		_, e = p.Read(b[:])
		if e == nil && (p.RemoteAddr().String() != "192.0.2.5:1234" || peerIP(p) != "127.0.0.1") {
			t.Error("peer and source conflated")
		}
		done <- e
	}()
	c, e := net.Dial("tcp", listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write([]byte("PROXY TCP4 192.0.2.5 127.0.0.1 1234 25\r\nX"))
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}

func TestSMTPAcceptanceDeliversToPersistentMailbox(t *testing.T) {
	logger := zap.NewNop()
	dir := t.TempDir()
	store, err := storage.NewMessageStore(filepath.Join(dir, "messages"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mailbox, err := storage.NewMailboxStore(storage.NewIMAPAdapter(store), filepath.Join(dir, "mailbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mailbox.Close()
	cfg := &config.Config{}
	cfg.Server.Domain = "hub.example.test"
	cfg.Server.LocalDomains = []string{"example.test"}
	cfg.Server.MaxMessageBytes = 1 << 20
	cfg.Server.MaxRecipients = 10
	cfg.Server.RequireAuth = true
	cfg.Server.AllowInsecureAuth = true
	cfg.Platform.ValidateRecipients = true
	cfg.Platform.DataDir = dir
	v := auth.NewValidator(logger)
	if err = v.GetUserStore().AddUser("alice", "test-password-123", "alice@example.test"); err != nil {
		t.Fatal(err)
	}
	q := NewQueueManager(logger, store, mailbox, cfg.Server.Domain, cfg.Server.LocalDomains, nil)
	defer q.Shutdown()
	if err = q.ConfigurePlatform(cfg, v.GetUserStore()); err != nil {
		t.Fatal(err)
	}
	srv := NewServerWithValidator(cfg, logger, q, nil, v)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.smtpServer.Close()
	go srv.smtpServer.Serve(listener)
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	c, err := stdsmtp.NewClient(conn, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.Auth(stdsmtp.PlainAuth("", "alice", "test-password-123", "localhost")); err != nil {
		t.Fatal(err)
	}
	if err = c.Mail("alice@example.test"); err != nil {
		t.Fatal(err)
	}
	if err = c.Rcpt("missing@example.test"); err == nil {
		t.Fatal("unknown recipient accepted")
	}
	if err = c.Rcpt("alice@example.test"); err != nil {
		t.Fatal(err)
	}
	w, err := c.Data()
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Write([]byte("From: alice@example.test\r\nTo: alice@example.test\r\nSubject: end to end\r\n\r\nmailbox payload\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := mailbox.GetMessages(context.Background(), "alice", "INBOX")
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) == 1 {
			entry, err := store.Get(messages[0].ID)
			if err != nil || !bytes.Contains(entry.Data, []byte("mailbox payload")) {
				t.Fatalf("bad stored message: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("accepted SMTP message did not reach mailbox")
}

func TestMailstormPauseDefersQueueWithoutSpendingAttempt(t *testing.T) {
	q, store := newPersistenceTestQueue(t)
	g, err := mailstorm.New(mailstorm.Config{Enabled: true}, filepath.Join(t.TempDir(), "storm.json"))
	if err != nil {
		t.Fatal(err)
	}
	q.StormGuard = g
	msg := &Message{AdmissionKey: "user:app", From: "a@test", To: []string{"b@test"}, Data: []byte("Subject: test\r\n\r\nbody"), Tier: TierOut}
	if err = q.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	if err = g.Pause("user:app", "incident", "operator", time.Hour); err != nil {
		t.Fatal(err)
	}
	q.processMessage("out", msg)
	entry, err := store.Get(msg.ID)
	if err != nil || entry.Status != "pending" || entry.Attempts != 0 {
		t.Fatalf("paused message lost/spent retries: %+v %v", entry, err)
	}
}
func TestRequiredMalwareScannerOutageDefers(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	p := config.PlatformConfig{ClamAVAddress: addr, MalwareRequired: true}
	if err := scanMalware(context.Background(), p, []byte("body")); err == nil {
		t.Fatal("unscanned message accepted")
	}
}
func TestRequiredARCDoesNotSilentlySkipSealing(t *testing.T) {
	scanner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"action":"no action"}`)) }))
	defer scanner.Close()
	cfg := &config.Config{}
	cfg.Platform.ARC = true
	cfg.Platform.ScannerRequired = true
	cfg.Platform.ScannerURL = scanner.URL
	s := &Session{config: cfg, msg: &Message{Data: []byte("Subject: test\r\n\r\nbody")}}
	if err := s.scanFinal(); err == nil {
		t.Fatal("missing ARC sealing accepted")
	}
}
