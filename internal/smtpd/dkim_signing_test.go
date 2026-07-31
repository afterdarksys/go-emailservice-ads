package smtpd

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/emersion/go-msgauth/dkim"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/security"
)

// newTestDKIMSigner writes a throwaway Ed25519 key to a temp file and builds
// a *security.Signer configured for the given domain, plus a LookupTXT stub
// publishing the matching public key at selector "mail" so tests can perform
// full cryptographic verification without a real DNS record.
func newTestDKIMSigner(t *testing.T, domain string) (*security.Signer, func(string) ([]string, error)) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	keyPath := filepath.Join(t.TempDir(), "dkim.pem")
	if err := os.WriteFile(keyPath, pemBytes, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	signer, err := security.NewSigner(zap.NewNop(), domain, "mail", keyPath)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}

	txtRecord := "v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(pub)
	wantName := "mail._domainkey." + domain
	lookupTXT := func(name string) ([]string, error) {
		if name != wantName {
			return nil, fmt.Errorf("no such host")
		}
		return []string{txtRecord}, nil
	}

	return signer, lookupTXT
}

func TestSignOutboundSkipsWhenNoSignerConfigured(t *testing.T) {
	qm := &QueueManager{logger: zap.NewNop()}
	msg := &Message{From: "user@gomeow.media", Data: []byte("From: user@gomeow.media\r\nTo: a@b.test\r\nSubject: hi\r\nDate: Fri, 31 Jul 2026 12:00:00 +0000\r\nMessage-ID: <1@gomeow.media>\r\n\r\nbody\r\n")}

	signed, err := qm.signOutbound(msg)
	if err != nil {
		t.Fatalf("signOutbound() error = %v", err)
	}
	if signed != nil {
		t.Fatalf("signOutbound() = %v, want nil (no signer configured)", signed)
	}
}

func TestSignOutboundSkipsOnDomainMismatch(t *testing.T) {
	signer, _ := newTestDKIMSigner(t, "gomeow.media")
	qm := &QueueManager{logger: zap.NewNop(), dkimSigner: signer}
	msg := &Message{From: "user@some-other-domain.test", Data: []byte("From: user@some-other-domain.test\r\nTo: a@b.test\r\nSubject: hi\r\nDate: Fri, 31 Jul 2026 12:00:00 +0000\r\nMessage-ID: <1@some-other-domain.test>\r\n\r\nbody\r\n")}

	signed, err := qm.signOutbound(msg)
	if err != nil {
		t.Fatalf("signOutbound() error = %v", err)
	}
	if signed != nil {
		t.Fatalf("signOutbound() signed a message for a domain the signer does not own: %s", signed)
	}
}

func TestSignOutboundSignsMatchingDomainCaseInsensitively(t *testing.T) {
	signer, lookupTXT := newTestDKIMSigner(t, "gomeow.media")
	qm := &QueueManager{logger: zap.NewNop(), dkimSigner: signer}
	msg := &Message{
		From: "user@GoMeow.Media",
		Data: []byte("From: user@GoMeow.Media\r\nTo: a@b.test\r\nSubject: hi\r\nDate: Fri, 31 Jul 2026 12:00:00 +0000\r\nMessage-ID: <1@gomeow.media>\r\n\r\nbody\r\n"),
	}

	signed, err := qm.signOutbound(msg)
	if err != nil {
		t.Fatalf("signOutbound() error = %v", err)
	}
	if signed == nil {
		t.Fatal("signOutbound() = nil, want a signed message for the configured domain")
	}
	if !bytes.Contains(signed, []byte("DKIM-Signature:")) {
		t.Fatalf("signed message missing DKIM-Signature header:\n%s", signed)
	}

	verifications, err := dkim.VerifyWithOptions(bytes.NewReader(signed), &dkim.VerifyOptions{LookupTXT: lookupTXT})
	if err != nil {
		t.Fatalf("dkim.VerifyWithOptions() error = %v", err)
	}
	if len(verifications) != 1 {
		t.Fatalf("verifications = %d, want 1", len(verifications))
	}
	if verifications[0].Err != nil {
		t.Fatalf("signature did not verify: %v", verifications[0].Err)
	}
	if verifications[0].Domain != "gomeow.media" {
		t.Fatalf("verified domain = %q, want %q", verifications[0].Domain, "gomeow.media")
	}
}
