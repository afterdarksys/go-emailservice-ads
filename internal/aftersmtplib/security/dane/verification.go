package dane

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"fmt"

	"go.uber.org/zap"
)

// RFC 6698 Section 2.1 - Certificate Usage
// Certificate verification against TLSA records

// CertificateMatch represents the result of matching a certificate to TLSA
type CertificateMatch struct {
	Matched      bool
	TLSARecord   *TLSARecord
	MatchedUsage uint8
	ErrorReason  string
}

// VerifyCertificate verifies a certificate against TLSA records
// Returns the first matching record or error if none match
// RFC 7672 Section 3.1 - DANE Certificate Verification
func VerifyCertificate(cert *x509.Certificate, chain []*x509.Certificate, tlsaRecords []*TLSARecord, logger *zap.Logger) (*CertificateMatch, error) {
	if cert == nil {
		return nil, fmt.Errorf("certificate is nil")
	}

	if len(tlsaRecords) == 0 {
		return nil, fmt.Errorf("no TLSA records to verify against")
	}

	logger.Debug("Verifying certificate against TLSA records",
		zap.String("subject", cert.Subject.String()),
		zap.Int("tlsa_count", len(tlsaRecords)))

	// Try each TLSA record until we find a match
	for _, tlsa := range tlsaRecords {
		match, err := matchCertificateToTLSA(cert, chain, tlsa, logger)
		if err != nil {
			logger.Debug("TLSA match failed",
				zap.Uint8("usage", tlsa.Usage),
				zap.Uint8("selector", tlsa.Selector),
				zap.Uint8("matching_type", tlsa.MatchingType),
				zap.Error(err))
			continue
		}

		if match {
			logger.Info("Certificate matched TLSA record",
				zap.String("subject", cert.Subject.String()),
				zap.Uint8("usage", tlsa.Usage),
				zap.Uint8("selector", tlsa.Selector),
				zap.Uint8("matching_type", tlsa.MatchingType))

			return &CertificateMatch{
				Matched:      true,
				TLSARecord:   tlsa,
				MatchedUsage: tlsa.Usage,
			}, nil
		}
	}

	return &CertificateMatch{
		Matched:     false,
		ErrorReason: "certificate did not match any TLSA records",
	}, fmt.Errorf("no matching TLSA record found")
}

// matchCertificateToTLSA matches a certificate to a specific TLSA record
func matchCertificateToTLSA(cert *x509.Certificate, chain []*x509.Certificate, tlsa *TLSARecord, logger *zap.Logger) (bool, error) {
	// Select which certificate to verify based on Usage
	var certToVerify *x509.Certificate

	switch tlsa.Usage {
	case TLSAUsagePKIXTA:
		// PKIX-TA: Match against CA in the chain (trust anchor)
		return verifyPKIXTA(cert, chain, tlsa, logger)

	case TLSAUsagePKIXEE:
		// PKIX-EE: Match against end-entity certificate
		// Also requires standard PKIX validation
		certToVerify = cert

	case TLSAUsageDANETA:
		// DANE-TA: Match against trust anchor in chain
		return verifyDANETA(cert, chain, tlsa, logger)

	case TLSAUsageDANEEE:
		// DANE-EE: Match against end-entity certificate (most common)
		// No PKIX validation required
		certToVerify = cert

	default:
		return false, fmt.Errorf("unknown TLSA usage: %d", tlsa.Usage)
	}

	// Extract the data to match based on Selector
	dataToMatch, err := extractCertificateData(certToVerify, tlsa.Selector)
	if err != nil {
		return false, fmt.Errorf("failed to extract certificate data: %w", err)
	}

	// Match the data using the specified Matching Type
	return matchData(dataToMatch, tlsa.Certificate, tlsa.MatchingType, logger)
}

// extractCertificateData extracts the data to match based on TLSA selector
func extractCertificateData(cert *x509.Certificate, selector uint8) ([]byte, error) {
	switch selector {
	case TLSASelectorCert:
		// Selector 0: Full certificate (DER-encoded)
		return cert.Raw, nil

	case TLSASelectorSPKI:
		// Selector 1: SubjectPublicKeyInfo (DER-encoded)
		return extractSPKI(cert)

	default:
		return nil, fmt.Errorf("unknown TLSA selector: %d", selector)
	}
}

// extractSPKI extracts the SubjectPublicKeyInfo from a certificate
func extractSPKI(cert *x509.Certificate) ([]byte, error) {
	// The SPKI is available as RawSubjectPublicKeyInfo in modern Go
	// This contains the DER-encoded SubjectPublicKeyInfo
	if len(cert.RawSubjectPublicKeyInfo) > 0 {
		return cert.RawSubjectPublicKeyInfo, nil
	}

	// Fallback: marshal the public key manually
	spkiBytes, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SPKI: %w", err)
	}

	return spkiBytes, nil
}

// matchData matches certificate data against TLSA record using matching type
func matchData(certData, tlsaData []byte, matchingType uint8, logger *zap.Logger) (bool, error) {
	switch matchingType {
	case TLSAMatchingFull:
		// Matching Type 0: Exact match
		match := bytes.Equal(certData, tlsaData)
		logger.Debug("Full certificate match",
			zap.Bool("matched", match),
			zap.Int("cert_len", len(certData)),
			zap.Int("tlsa_len", len(tlsaData)))
		return match, nil

	case TLSAMatchingSHA256:
		// Matching Type 1: SHA-256 hash
		hash := sha256.Sum256(certData)
		match := bytes.Equal(hash[:], tlsaData)
		logger.Debug("SHA-256 hash match",
			zap.Bool("matched", match))
		return match, nil

	case TLSAMatchingSHA512:
		// Matching Type 2: SHA-512 hash
		hash := sha512.Sum512(certData)
		match := bytes.Equal(hash[:], tlsaData)
		logger.Debug("SHA-512 hash match",
			zap.Bool("matched", match))
		return match, nil

	default:
		return false, fmt.Errorf("unknown TLSA matching type: %d", matchingType)
	}
}

// verifyPKIXTA verifies certificate against PKIX-TA TLSA record
// Usage 0: Certificate must chain to the specified CA
func verifyPKIXTA(cert *x509.Certificate, chain []*x509.Certificate, tlsa *TLSARecord, logger *zap.Logger) (bool, error) {
	logger.Debug("Verifying PKIX-TA (CA constraint)")

	// Check each certificate in the chain (looking for CA)
	certs := append([]*x509.Certificate{cert}, chain...)

	for i, chainCert := range certs {
		// Skip non-CA certificates
		if !chainCert.IsCA {
			continue
		}

		// Extract data based on selector
		data, err := extractCertificateData(chainCert, tlsa.Selector)
		if err != nil {
			logger.Debug("Failed to extract CA cert data",
				zap.Int("chain_index", i),
				zap.Error(err))
			continue
		}

		// Match the data
		matched, err := matchData(data, tlsa.Certificate, tlsa.MatchingType, logger)
		if err != nil {
			continue
		}

		if matched {
			logger.Info("PKIX-TA matched CA in chain",
				zap.Int("chain_index", i),
				zap.String("ca_subject", chainCert.Subject.String()))
			return true, nil
		}
	}

	return false, fmt.Errorf("no CA in chain matched PKIX-TA record")
}

// verifyDANETA verifies certificate against DANE-TA TLSA record
// Usage 2: Certificate must chain to this trust anchor
func verifyDANETA(cert *x509.Certificate, chain []*x509.Certificate, tlsa *TLSARecord, logger *zap.Logger) (bool, error) {
	logger.Debug("Verifying DANE-TA (trust anchor)")

	// Similar to PKIX-TA but doesn't require standard PKIX validation
	// Check the entire chain including the end-entity cert
	certs := append([]*x509.Certificate{cert}, chain...)

	for i, chainCert := range certs {
		// Extract data based on selector
		data, err := extractCertificateData(chainCert, tlsa.Selector)
		if err != nil {
			logger.Debug("Failed to extract cert data",
				zap.Int("chain_index", i),
				zap.Error(err))
			continue
		}

		// Match the data
		matched, err := matchData(data, tlsa.Certificate, tlsa.MatchingType, logger)
		if err != nil {
			continue
		}

		if matched {
			logger.Info("DANE-TA matched certificate in chain",
				zap.Int("chain_index", i),
				zap.String("subject", chainCert.Subject.String()))
			return true, nil
		}
	}

	return false, fmt.Errorf("no certificate in chain matched DANE-TA record")
}

// ComputeTLSAHash computes the hash for a certificate based on selector and matching type
// Useful for generating TLSA records
func ComputeTLSAHash(cert *x509.Certificate, selector, matchingType uint8) ([]byte, error) {
	// Extract data based on selector
	data, err := extractCertificateData(cert, selector)
	if err != nil {
		return nil, err
	}

	// Apply matching type
	switch matchingType {
	case TLSAMatchingFull:
		return data, nil

	case TLSAMatchingSHA256:
		hash := sha256.Sum256(data)
		return hash[:], nil

	case TLSAMatchingSHA512:
		hash := sha512.Sum512(data)
		return hash[:], nil

	default:
		return nil, fmt.Errorf("unknown matching type: %d", matchingType)
	}
}

// GenerateTLSARecord generates a TLSA record from a certificate
// This is useful for testing and for generating TLSA records to publish in DNS
func GenerateTLSARecord(cert *x509.Certificate, usage, selector, matchingType uint8, port int, domain string) (*TLSARecord, error) {
	hash, err := ComputeTLSAHash(cert, selector, matchingType)
	if err != nil {
		return nil, fmt.Errorf("failed to compute hash: %w", err)
	}

	record := &TLSARecord{
		Usage:        usage,
		Selector:     selector,
		MatchingType: matchingType,
		Certificate:  hash,
		Port:         port,
		Domain:       domain,
		TTL:          3600, // 1 hour default
	}

	return record, record.IsValid()
}

// ValidateCertificateChain performs basic certificate chain validation
// This is used for PKIX-TA and PKIX-EE usage types
func ValidateCertificateChain(cert *x509.Certificate, chain []*x509.Certificate, logger *zap.Logger) error {
	// Build certificate pool from chain
	pool := x509.NewCertPool()
	for _, c := range chain {
		pool.AddCert(c)
	}

	// Verify certificate
	opts := x509.VerifyOptions{
		Roots:         pool,
		Intermediates: pool,
		DNSName:       "", // Don't verify DNS name (DANE handles that)
	}

	if _, err := cert.Verify(opts); err != nil {
		logger.Debug("Certificate chain validation failed",
			zap.Error(err))
		return fmt.Errorf("certificate chain validation failed: %w", err)
	}

	logger.Debug("Certificate chain validation succeeded")
	return nil
}

// ShouldPerformPKIXValidation determines if PKIX validation is required
func ShouldPerformPKIXValidation(usage uint8) bool {
	return usage == TLSAUsagePKIXTA || usage == TLSAUsagePKIXEE
}

// GetCertificateFingerprint returns a human-readable certificate fingerprint
func GetCertificateFingerprint(cert *x509.Certificate) string {
	hash := sha256.Sum256(cert.Raw)
	return fmt.Sprintf("%x", hash)
}
