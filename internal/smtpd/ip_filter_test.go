package smtpd

import (
	"errors"
	"net"
	stdsmtp "net/smtp"
	"net/textproto"
	"testing"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/emersion/go-smtp"
	"go.uber.org/zap"
)

func TestSMTPDenylistRejectsAndReleasesConnection(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.IPFilter.Denylist = []string{"127.0.0.0/8"}
	cfg.Server.IPFilter.Allowlist = []string{"127.0.0.1"}
	limiter := newConnectionLimiter(1, 1)
	backend := &Backend{config: cfg, logger: zap.NewNop(), limiter: limiter}
	server := smtp.NewServer(backend)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve(listener)
	for i := 0; i < 2; i++ {
		conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		client, err := stdsmtp.NewClient(conn, "localhost")
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
		err = client.Hello("sender.example")
		client.Close()
		var smtpErr *textproto.Error
		if !errors.As(err, &smtpErr) || smtpErr.Code != 554 {
			t.Fatalf("got %v, want 554", err)
		}
	}
}
