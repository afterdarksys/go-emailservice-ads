package smtpd

import (
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

func TestTrustedContentFilterProxy(t *testing.T) {
	cfg := &config.Config{}
	cfg.ContentFilter.Enabled = true
	cfg.ContentFilter.TrustedProxyNetworks = []string{"172.30.0.0/24"}
	b := &Backend{config: cfg}
	if !b.isTrustedContentFilterProxy("172.30.0.4") {
		t.Fatal("trusted proxy address was not accepted")
	}
	if b.isTrustedContentFilterProxy("172.31.0.4") || b.isTrustedContentFilterProxy("not-an-ip") {
		t.Fatal("untrusted proxy address was accepted")
	}
}

func TestQuarantineMarkerIsCaseInsensitiveAndRemoved(t *testing.T) {
	raw := []byte("From: sender@example.net\r\nX-MailScript-Quarantine: TRUE\r\nSubject: test\r\n\r\nbody\r\n")
	if !hasHeader(raw, "x-mailscript-quarantine", "true") {
		t.Fatal("marker was not found")
	}
	got := string(removeHeader(raw, "X-MailScript-Quarantine"))
	if got != "From: sender@example.net\r\nSubject: test\r\n\r\nbody\r\n" {
		t.Fatalf("unexpected filtered message: %q", got)
	}
}
