package security

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/emersion/go-msgauth/dkim"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/dns"
)

// Signer handles adding DKIM signatures to outbound messages
type Signer struct {
	logger    *zap.Logger
	domain    string
	selector  string
	rsaKey    *rsa.PrivateKey
	ed25519Key ed25519.PrivateKey
}

// NewSigner initializes a DKIM signer. If keyPath is empty, signing is disabled.
// Supports RSA (PKCS1 or PKCS8) and Ed25519 (PKCS8) private keys.
func NewSigner(logger *zap.Logger, domain, selector, keyPath string) (*Signer, error) {
	if keyPath == "" {
		return &Signer{logger: logger}, nil
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read DKIM key: %w", err)
	}

	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block from DKIM key")
	}

	s := &Signer{logger: logger, domain: domain, selector: selector}

	// Try PKCS1 RSA first
	if rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		s.rsaKey = rsaKey
		logger.Info("DKIM Signer configured (RSA)", zap.String("domain", domain), zap.String("selector", selector))
		return s, nil
	}

	// Try PKCS8 (RSA or Ed25519)
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DKIM private key (PKCS1/8): %w", err)
	}
	switch k := parsedKey.(type) {
	case *rsa.PrivateKey:
		s.rsaKey = k
		logger.Info("DKIM Signer configured (RSA/PKCS8)", zap.String("domain", domain), zap.String("selector", selector))
	case ed25519.PrivateKey:
		s.ed25519Key = k
		logger.Info("DKIM Signer configured (Ed25519)", zap.String("domain", domain), zap.String("selector", selector))
	default:
		return nil, fmt.Errorf("unsupported DKIM key type: %T", parsedKey)
	}

	return s, nil
}

// GetOptions returns dkim.SignOptions populated for the loaded key type.
func (s *Signer) GetOptions() *dkim.SignOptions {
	headerKeys := []string{"From", "To", "Subject", "Date", "Message-ID"}

	if s.ed25519Key != nil {
		return &dkim.SignOptions{
			Domain:     s.domain,
			Selector:   s.selector,
			Signer:     s.ed25519Key,
			HeaderKeys: headerKeys,
		}
	}
	if s.rsaKey != nil {
		return &dkim.SignOptions{
			Domain:     s.domain,
			Selector:   s.selector,
			Signer:     s.rsaKey,
			HeaderKeys: headerKeys,
		}
	}
	return nil
}

// Verifier handles DKIM signature verification for incoming messages
type Verifier struct {
	logger   *zap.Logger
	resolver *dns.Resolver
}

// NewVerifier creates a new DKIM verifier
func NewVerifier(logger *zap.Logger, resolver *dns.Resolver) *Verifier {
	return &Verifier{
		logger:   logger,
		resolver: resolver,
	}
}

// VerifyDKIM verifies DKIM signatures in an email message
// RFC 6376 - DomainKeys Identified Mail (DKIM) Signatures
func (v *Verifier) VerifyDKIM(_ context.Context, msg []byte) (string, error) {
	v.logger.Debug("Verifying DKIM signature")

	// Use emersion/go-msgauth for DKIM verification
	// Need to convert []byte to io.Reader
	verifications, err := dkim.Verify(bytes.NewReader(msg))
	if err != nil {
		v.logger.Warn("DKIM verification failed", zap.Error(err))
		return "fail", err
	}

	// Check if any signature passed
	for _, verification := range verifications {
		if verification.Err == nil {
			v.logger.Info("DKIM signature verified",
				zap.String("domain", verification.Domain))
			return "pass", nil
		} else {
			v.logger.Warn("DKIM signature failed",
				zap.String("domain", verification.Domain),
				zap.Error(verification.Err))
		}
	}

	return "fail", fmt.Errorf("no valid DKIM signatures found")
}

// VerifyWithDetails performs DKIM verification and returns detailed results
func (v *Verifier) VerifyWithDetails(msg []byte) ([]*dkim.Verification, error) {
	v.logger.Debug("Verifying DKIM signature with details")

	verifications, err := dkim.Verify(bytes.NewReader(msg))
	if err != nil {
		v.logger.Warn("DKIM verification failed", zap.Error(err))
		return nil, err
	}

	for _, verification := range verifications {
		if verification.Err == nil {
			v.logger.Info("DKIM signature verified",
				zap.String("domain", verification.Domain))
		} else {
			v.logger.Warn("DKIM signature failed",
				zap.String("domain", verification.Domain),
				zap.Error(verification.Err))
		}
	}

	return verifications, nil
}
