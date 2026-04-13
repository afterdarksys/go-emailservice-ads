package security

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/dns"
)

// RFC 8463 - Authenticated Received Chain (ARC)
// ARC provides an authenticated "chain of custody" for email messages
// allowing intermediaries to sign messages while preserving original authentication

// ARCManager handles ARC signing and verification
type ARCManager struct {
	logger     *zap.Logger
	domain     string
	selector   string
	privateKey *rsa.PrivateKey
	resolver   *dns.Resolver
}

// ARCSet represents a complete ARC set (one entry in the chain)
type ARCSet struct {
	Instance       int
	ARCAuthResults string // ARC-Authentication-Results header
	ARCMessageSig  string // ARC-Message-Signature header
	ARCSeal        string // ARC-Seal header
}

// ARCResult represents the result of ARC chain validation
type ARCResult string

const (
	ARCResultNone ARCResult = "none"  // No ARC headers found
	ARCResultPass ARCResult = "pass"  // All signatures valid
	ARCResultFail ARCResult = "fail"  // One or more signatures invalid
)

// NewARCManager creates a new ARC manager
func NewARCManager(logger *zap.Logger, domain, selector string, privateKey *rsa.PrivateKey, resolver *dns.Resolver) *ARCManager {
	return &ARCManager{
		logger:     logger,
		domain:     domain,
		selector:   selector,
		privateKey: privateKey,
		resolver:   resolver,
	}
}

// AddARCSet adds a new ARC set to the message headers
// RFC 8463 Section 4 - ARC Set Creation
func (a *ARCManager) AddARCSet(headers []string, instance int, authResults string) ([]string, error) {
	if instance < 1 {
		return nil, fmt.Errorf("invalid instance number: %d", instance)
	}

	// Create ARC-Authentication-Results header
	// RFC 8463 Section 4.1.1
	arcAuthResults := fmt.Sprintf("i=%d; %s;", instance, a.domain)
	if authResults != "" {
		arcAuthResults = fmt.Sprintf("i=%d; %s; %s", instance, a.domain, authResults)
	}

	// Create ARC-Message-Signature header
	// RFC 8463 Section 4.1.2 - Similar to DKIM
	arcMessageSig, err := a.createMessageSignature(headers, instance)
	if err != nil {
		return nil, fmt.Errorf("failed to create message signature: %w", err)
	}

	// Create ARC-Seal header
	// RFC 8463 Section 4.1.3 - Signs the previous ARC sets
	arcSeal, err := a.createSeal(headers, instance, arcAuthResults, arcMessageSig)
	if err != nil {
		return nil, fmt.Errorf("failed to create seal: %w", err)
	}

	// Add new headers at the top (they must be in order)
	newHeaders := make([]string, 0, len(headers)+3)
	newHeaders = append(newHeaders, "ARC-Authentication-Results: "+arcAuthResults)
	newHeaders = append(newHeaders, "ARC-Message-Signature: "+arcMessageSig)
	newHeaders = append(newHeaders, "ARC-Seal: "+arcSeal)
	newHeaders = append(newHeaders, headers...)

	a.logger.Info("ARC set added",
		zap.Int("instance", instance),
		zap.String("domain", a.domain))

	return newHeaders, nil
}

// createMessageSignature creates an ARC-Message-Signature header
func (a *ARCManager) createMessageSignature(headers []string, instance int) (string, error) {
	// Select headers to sign (similar to DKIM)
	// RFC 8463 Section 4.1.2.5 - Recommended headers
	headersToSign := []string{
		"from", "to", "cc", "subject", "date",
		"message-id", "in-reply-to", "references",
		"mime-version", "content-type",
	}

	// Create canonicalized header string
	canonicalHeaders := a.canonicalizeHeaders(headers, headersToSign)

	// Create signature input
	signatureInput := fmt.Sprintf("i=%d; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; t=%d; h=%s; bh=%s; b=",
		instance,
		a.domain,
		a.selector,
		time.Now().Unix(),
		strings.Join(headersToSign, ":"),
		a.bodyHash(headers),
	)

	// Calculate signature
	h := sha256.New()
	h.Write([]byte(canonicalHeaders + signatureInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.privateKey, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return "", fmt.Errorf("failed to sign: %w", err)
	}

	signatureB64 := base64.StdEncoding.EncodeToString(signature)
	return signatureInput + signatureB64, nil
}

// createSeal creates an ARC-Seal header
// RFC 8463 Section 4.1.3 - ARC-Seal signs the entire ARC chain
func (a *ARCManager) createSeal(headers []string, instance int, arcAuthResults, arcMessageSig string) (string, error) {
	// Collect all previous ARC headers
	arcHeaders := a.collectPreviousARCHeaders(headers, instance-1)

	// Add current headers
	arcHeaders = append(arcHeaders, "ARC-Authentication-Results: "+arcAuthResults)
	arcHeaders = append(arcHeaders, "ARC-Message-Signature: "+arcMessageSig)

	// Canonicalize
	canonical := strings.Join(arcHeaders, "\r\n")

	// Create seal input
	sealInput := fmt.Sprintf("i=%d; a=rsa-sha256; d=%s; s=%s; t=%d; cv=%s; b=",
		instance,
		a.domain,
		a.selector,
		time.Now().Unix(),
		a.getChainValidation(instance),
	)

	// Sign
	h := sha256.New()
	h.Write([]byte(canonical + sealInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.privateKey, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return "", fmt.Errorf("failed to sign seal: %w", err)
	}

	signatureB64 := base64.StdEncoding.EncodeToString(signature)
	return sealInput + signatureB64, nil
}

// VerifyChain verifies the entire ARC chain
// RFC 8463 Section 5 - ARC Validation
func (a *ARCManager) VerifyChain(headers []string) ARCResult {
	// Find all ARC sets
	sets := a.parseARCSets(headers)

	if len(sets) == 0 {
		return ARCResultNone
	}

	// Verify each set in order
	for i, set := range sets {
		expectedInstance := i + 1
		if set.Instance != expectedInstance {
			a.logger.Warn("ARC instance number mismatch",
				zap.Int("expected", expectedInstance),
				zap.Int("got", set.Instance))
			return ARCResultFail
		}

		// Verify ARC-Seal
		if !a.verifySeal(set, sets[:i]) {
			a.logger.Warn("ARC-Seal verification failed",
				zap.Int("instance", set.Instance))
			return ARCResultFail
		}

		// Verify ARC-Message-Signature
		if !a.verifyMessageSignature(set, headers) {
			a.logger.Warn("ARC-Message-Signature verification failed",
				zap.Int("instance", set.Instance))
			return ARCResultFail
		}
	}

	// Check chain validation status
	lastSet := sets[len(sets)-1]
	cv := a.extractCV(lastSet.ARCSeal)

	a.logger.Info("ARC chain verified",
		zap.Int("sets", len(sets)),
		zap.String("cv", cv))

	if cv == "pass" || cv == "none" {
		return ARCResultPass
	}

	return ARCResultFail
}

// Helper methods

func (a *ARCManager) canonicalizeHeaders(headers []string, headersToSign []string) string {
	// Simplified canonicalization (relaxed/relaxed)
	var canonical []string
	for _, header := range headers {
		for _, name := range headersToSign {
			if strings.HasPrefix(strings.ToLower(header), strings.ToLower(name)+":") {
				// Remove header name, normalize whitespace
				value := strings.TrimSpace(header[len(name)+1:])
				canonical = append(canonical, strings.ToLower(name)+":"+value)
				break
			}
		}
	}
	return strings.Join(canonical, "\r\n")
}

func (a *ARCManager) bodyHash(headers []string) string {
	// Find body (everything after empty line)
	body := ""
	foundEmpty := false
	for _, line := range headers {
		if foundEmpty {
			body += line + "\r\n"
		} else if line == "" {
			foundEmpty = true
		}
	}

	h := sha256.Sum256([]byte(body))
	return base64.StdEncoding.EncodeToString(h[:])
}

func (a *ARCManager) collectPreviousARCHeaders(headers []string, maxInstance int) []string {
	var arcHeaders []string
	for _, header := range headers {
		if strings.HasPrefix(header, "ARC-") {
			// Extract instance number
			if instance := a.extractInstance(header); instance > 0 && instance <= maxInstance {
				arcHeaders = append(arcHeaders, header)
			}
		}
	}
	return arcHeaders
}

func (a *ARCManager) getChainValidation(instance int) string {
	// RFC 8463 Section 4.1.3.3
	// cv=none for first set, cv=pass for subsequent if previous validates
	if instance == 1 {
		return "none"
	}
	return "pass" // Simplified - should verify previous chain
}

func (a *ARCManager) parseARCSets(headers []string) []ARCSet {
	sets := make(map[int]*ARCSet)

	for _, header := range headers {
		instance := a.extractInstance(header)
		if instance == 0 {
			continue
		}

		if sets[instance] == nil {
			sets[instance] = &ARCSet{Instance: instance}
		}

		switch {
		case strings.HasPrefix(header, "ARC-Authentication-Results:"):
			sets[instance].ARCAuthResults = header
		case strings.HasPrefix(header, "ARC-Message-Signature:"):
			sets[instance].ARCMessageSig = header
		case strings.HasPrefix(header, "ARC-Seal:"):
			sets[instance].ARCSeal = header
		}
	}

	// Convert to ordered slice
	var result []ARCSet
	for i := 1; i <= len(sets); i++ {
		if set := sets[i]; set != nil {
			result = append(result, *set)
		}
	}

	return result
}

func (a *ARCManager) extractInstance(header string) int {
	// Extract i=X from header
	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "i=") {
			var instance int
			if _, err := fmt.Sscanf(part, "i=%d", &instance); err == nil {
				return instance
			}
		}
	}
	return 0
}

func (a *ARCManager) extractCV(arcSeal string) string {
	// Extract cv= from ARC-Seal
	parts := strings.Split(arcSeal, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "cv=") {
			return strings.TrimPrefix(part, "cv=")
		}
	}
	return ""
}

// fetchPublicKey retrieves the RSA public key for the given selector and domain from DNS.
func (a *ARCManager) fetchPublicKey(ctx context.Context, selector, domain string) (*rsa.PublicKey, error) {
	txtName := selector + "._domainkey." + domain
	records, err := a.resolver.LookupTXT(ctx, txtName)
	if err != nil {
		return nil, fmt.Errorf("DNS TXT lookup for %s failed: %w", txtName, err)
	}
	for _, record := range records {
		// Find p= field
		for _, field := range strings.Split(record, ";") {
			field = strings.TrimSpace(field)
			if !strings.HasPrefix(field, "p=") {
				continue
			}
			keyB64 := strings.TrimPrefix(field, "p=")
			keyDER, err := base64.StdEncoding.DecodeString(keyB64)
			if err != nil {
				return nil, fmt.Errorf("failed to decode public key base64: %w", err)
			}
			pub, err := x509.ParsePKIXPublicKey(keyDER)
			if err != nil {
				return nil, fmt.Errorf("failed to parse public key: %w", err)
			}
			rsaPub, ok := pub.(*rsa.PublicKey)
			if !ok {
				return nil, fmt.Errorf("public key is not RSA")
			}
			return rsaPub, nil
		}
	}
	return nil, fmt.Errorf("no public key found in DNS for %s", txtName)
}

// parseTagValue extracts the value of a named tag (e.g. "b") from a semicolon-separated header.
func parseTagValue(header, tag string) string {
	for _, field := range strings.Split(header, ";") {
		field = strings.TrimSpace(field)
		if strings.HasPrefix(field, tag+"=") {
			return strings.TrimSpace(strings.TrimPrefix(field, tag+"="))
		}
	}
	return ""
}

func (a *ARCManager) verifySeal(set ARCSet, previousSets []ARCSet) bool {
	if a.resolver == nil {
		a.logger.Warn("ARC seal verification skipped: no DNS resolver configured")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	selector := parseTagValue(set.ARCSeal, "s")
	domain := parseTagValue(set.ARCSeal, "d")
	sigB64 := parseTagValue(set.ARCSeal, "b")
	if selector == "" || domain == "" || sigB64 == "" {
		a.logger.Warn("ARC-Seal missing required tags", zap.Int("instance", set.Instance))
		return false
	}

	pubKey, err := a.fetchPublicKey(ctx, selector, domain)
	if err != nil {
		a.logger.Warn("Failed to fetch ARC public key", zap.Int("instance", set.Instance), zap.Error(err))
		return false
	}

	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		a.logger.Warn("Failed to decode ARC-Seal signature", zap.Error(err))
		return false
	}

	// Signed data is the ARC-Seal value with the b= field value stripped (up to and including "b=")
	bIdx := strings.Index(set.ARCSeal, "b=")
	if bIdx < 0 {
		return false
	}
	signedData := set.ARCSeal[:bIdx+2] // include "b=" but not the value

	h := sha256.New()
	h.Write([]byte(signedData))
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, h.Sum(nil), sigBytes); err != nil {
		a.logger.Warn("ARC-Seal signature verification failed", zap.Int("instance", set.Instance), zap.Error(err))
		return false
	}
	return true
}

func (a *ARCManager) verifyMessageSignature(set ARCSet, headers []string) bool {
	if a.resolver == nil {
		a.logger.Warn("ARC message signature verification skipped: no DNS resolver configured")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	selector := parseTagValue(set.ARCMessageSig, "s")
	domain := parseTagValue(set.ARCMessageSig, "d")
	sigB64 := parseTagValue(set.ARCMessageSig, "b")
	if selector == "" || domain == "" || sigB64 == "" {
		a.logger.Warn("ARC-Message-Signature missing required tags", zap.Int("instance", set.Instance))
		return false
	}

	pubKey, err := a.fetchPublicKey(ctx, selector, domain)
	if err != nil {
		a.logger.Warn("Failed to fetch ARC public key for message sig", zap.Int("instance", set.Instance), zap.Error(err))
		return false
	}

	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		a.logger.Warn("Failed to decode ARC-Message-Signature", zap.Error(err))
		return false
	}

	// Hash the header set canonicalized for signing (up to but not including b= value)
	bIdx := strings.Index(set.ARCMessageSig, "b=")
	if bIdx < 0 {
		return false
	}
	signedHeaders := a.canonicalizeHeaders(headers, []string{
		"from", "to", "cc", "subject", "date", "message-id",
		"in-reply-to", "references", "mime-version", "content-type",
	})
	signedData := signedHeaders + "\r\n" + set.ARCMessageSig[:bIdx+2]

	h := sha256.New()
	h.Write([]byte(signedData))
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, h.Sum(nil), sigBytes); err != nil {
		a.logger.Warn("ARC-Message-Signature verification failed", zap.Int("instance", set.Instance), zap.Error(err))
		return false
	}
	return true
}
