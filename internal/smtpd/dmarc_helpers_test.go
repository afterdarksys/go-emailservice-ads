package smtpd

import (
	"strings"
	"testing"
)

func TestAddressDomain(t *testing.T) {
	cases := map[string]string{
		"user@example.com":   "example.com",
		"<user@Example.COM>": "example.com",
		"  bob@sub.test.org": "sub.test.org",
		"<>":                 "",
		"malformed":          "",
		"trailing@":          "",
	}
	for in, want := range cases {
		if got := addressDomain(in); got != want {
			t.Errorf("addressDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeaderFromDomain(t *testing.T) {
	t.Run("single from", func(t *testing.T) {
		raw := []byte("From: Alice <alice@example.com>\r\nSubject: hi\r\n\r\nbody")
		if got := headerFromDomain(raw); got != "example.com" {
			t.Errorf("got %q, want example.com", got)
		}
	})
	t.Run("missing from", func(t *testing.T) {
		raw := []byte("Subject: hi\r\n\r\nbody")
		if got := headerFromDomain(raw); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("multiple from is ambiguous", func(t *testing.T) {
		raw := []byte("From: a@x.com, b@y.com\r\n\r\nbody")
		if got := headerFromDomain(raw); got != "" {
			t.Errorf("got %q, want empty for multi-From", got)
		}
	})
}

func TestPrependAuthResults(t *testing.T) {
	orig := []byte("From: a@x.com\r\nSubject: hi\r\n\r\nbody")
	out := prependAuthResults(orig, "mx.test", "pass", "fail", "none", "a@x.com")
	s := string(out)

	if !strings.HasPrefix(s, "Authentication-Results: mx.test; spf=pass smtp.mailfrom=a@x.com; dkim=fail; dmarc=none\r\n") {
		t.Fatalf("unexpected header: %q", s[:strings.Index(s, "\r\n")])
	}
	// Original message must be preserved intact after the new header.
	if !strings.HasSuffix(s, string(orig)) {
		t.Errorf("original message body not preserved")
	}
}

func TestPrependAuthResultsDefaultsAndInjection(t *testing.T) {
	// Empty verdicts default to "none"; a CRLF-injecting MAIL FROM is neutralized.
	orig := "X: y\r\n\r\n"
	out := prependAuthResults([]byte(orig), "", "", "", "",
		"evil@x.com\r\nInjected: header")
	s := string(out)

	// Security property: the injected CRLF must be stripped so no new header line
	// is created. The Authentication-Results header (everything before the
	// original message) must therefore be a single line.
	header := strings.TrimSuffix(s, orig)
	if strings.Count(header, "\r\n") != 1 || !strings.HasSuffix(header, "\r\n") {
		t.Errorf("CRLF injection created extra header lines: %q", header)
	}
	// Defaults applied.
	if !strings.Contains(header, "spf=none") || !strings.Contains(header, "dkim=none") || !strings.Contains(header, "dmarc=none") {
		t.Errorf("defaults not applied: %q", header)
	}
}
