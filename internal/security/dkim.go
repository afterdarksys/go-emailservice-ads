package security

import (
	"bytes"
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
	logger     *zap.Logger
	domain     string
	selector   string
	privateKey *rsa.PrivateKey
}

// NewSigner initializes a DKIM signer. If keyPath is empty, signing is disabled.
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

	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8
		parsedKey, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("failed to parse DKIM private key (PKCS1/8): %w", err)
		}
		var ok bool
		privKey, ok = parsedKey.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("DKIM key is not RSA")
		}
	}

	logger.Info("DKIM Signer configured", zap.String("domain", domain), zap.String("selector", selector))

	return &Signer{
		logger:     logger,
		domain:     domain,
		selector:   selector,
		privateKey: privKey,
	}, nil
}

// SignOptions defines the headers and body hash settings
func (s *Signer) GetOptions() *dkim.SignOptions {
	if s.privateKey == nil {
		return nil
	}
	return &dkim.SignOptions{
		Domain:   s.domain,
		Selector: s.selector,
		Signer:   s.privateKey,
		HeaderKeys: []string{
			"From", "To", "Subject", "Date", "Message-ID",
		},
	}
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
func (v *Verifier) VerifyDKIM(msg []byte) (string, error) {
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
