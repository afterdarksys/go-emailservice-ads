package dane

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/miekg/dns"
	"go.uber.org/zap"
)

// RFC 6698 - TLSA RR for DNS-Based Authentication of Named Entities
// RFC 7671 - DANE Operations
// RFC 7672 - SMTP Security via Opportunistic DNS-Based Authentication

// TLSA Usage Values (RFC 6698 Section 2.1.1)
const (
	TLSAUsagePKIXTA   = 0 // CA constraint (PKIX Trust Anchor)
	TLSAUsagePKIXEE   = 1 // Service certificate constraint (PKIX End Entity)
	TLSAUsageDANETA   = 2 // Trust anchor assertion (DANE-TA)
	TLSAUsageDANEEE   = 3 // Domain-issued certificate (DANE-EE) - Most common
)

// TLSA Selector Values (RFC 6698 Section 2.1.2)
const (
	TLSASelectorCert    = 0 // Full certificate
	TLSASelectorSPKI    = 1 // SubjectPublicKeyInfo
)

// TLSA Matching Type Values (RFC 6698 Section 2.1.3)
const (
	TLSAMatchingFull     = 0 // Exact match
	TLSAMatchingSHA256   = 1 // SHA-256 hash
	TLSAMatchingSHA512   = 2 // SHA-512 hash
)

// TLSARecord represents a parsed TLSA DNS record
// RFC 6698 Section 2.1 - TLSA RDATA Wire Format
type TLSARecord struct {
	Usage        uint8  // 0-3: How to verify
	Selector     uint8  // 0-1: What to match
	MatchingType uint8  // 0-2: How to match
	Certificate  []byte // Certificate or hash data
	TTL          uint32 // Time to live
	Domain       string // Domain this TLSA protects
	Port         int    // Port number
}

// TLSALookupResult contains TLSA query results with DNSSEC validation
type TLSALookupResult struct {
	Records      []*TLSARecord
	DNSSECValid  bool
	DNSSECBogus  bool
	DNSSECInsecure bool
	ErrorReason  string
}

// String returns a human-readable description of the TLSA record
func (t *TLSARecord) String() string {
	usageStr := "Unknown"
	switch t.Usage {
	case TLSAUsagePKIXTA:
		usageStr = "PKIX-TA (CA Constraint)"
	case TLSAUsagePKIXEE:
		usageStr = "PKIX-EE (Service Cert)"
	case TLSAUsageDANETA:
		usageStr = "DANE-TA (Trust Anchor)"
	case TLSAUsageDANEEE:
		usageStr = "DANE-EE (Domain Cert)"
	}

	selectorStr := "Unknown"
	switch t.Selector {
	case TLSASelectorCert:
		selectorStr = "Full Certificate"
	case TLSASelectorSPKI:
		selectorStr = "SubjectPublicKeyInfo"
	}

	matchingStr := "Unknown"
	switch t.MatchingType {
	case TLSAMatchingFull:
		matchingStr = "Exact Match"
	case TLSAMatchingSHA256:
		matchingStr = "SHA-256"
	case TLSAMatchingSHA512:
		matchingStr = "SHA-512"
	}

	certData := hex.EncodeToString(t.Certificate)
	if len(certData) > 64 {
		certData = certData[:64] + "..."
	}

	return fmt.Sprintf("TLSA %s %s %s [%s]", usageStr, selectorStr, matchingStr, certData)
}

// IsValid checks if TLSA record has valid field values
func (t *TLSARecord) IsValid() error {
	// Validate Usage
	if t.Usage > 3 {
		return fmt.Errorf("invalid usage value: %d (must be 0-3)", t.Usage)
	}

	// Validate Selector
	if t.Selector > 1 {
		return fmt.Errorf("invalid selector value: %d (must be 0-1)", t.Selector)
	}

	// Validate Matching Type
	if t.MatchingType > 2 {
		return fmt.Errorf("invalid matching type: %d (must be 0-2)", t.MatchingType)
	}

	// Validate certificate data
	if len(t.Certificate) == 0 {
		return fmt.Errorf("empty certificate data")
	}

	// Validate hash length for hashed matching types
	switch t.MatchingType {
	case TLSAMatchingSHA256:
		if len(t.Certificate) != 32 {
			return fmt.Errorf("SHA-256 hash must be 32 bytes, got %d", len(t.Certificate))
		}
	case TLSAMatchingSHA512:
		if len(t.Certificate) != 64 {
			return fmt.Errorf("SHA-512 hash must be 64 bytes, got %d", len(t.Certificate))
		}
	}

	return nil
}

// LookupTLSA queries TLSA records for a given hostname and port with DNSSEC validation
// RFC 7672 Section 2.1 - SMTP Server Identity Verification
func LookupTLSA(ctx context.Context, resolver *DNSSECResolver, hostname string, port int, logger *zap.Logger) (*TLSALookupResult, error) {
	// Construct TLSA query name: _<port>._tcp.<hostname>
	// Example: _25._tcp.mail.example.com
	tlsaName := fmt.Sprintf("_%d._tcp.%s", port, hostname)

	logger.Debug("Looking up TLSA records",
		zap.String("query", tlsaName),
		zap.String("hostname", hostname),
		zap.Int("port", port))

	// Query with DNSSEC validation
	msg, validationResult, err := resolver.Query(ctx, tlsaName, dns.TypeTLSA)
	if err != nil {
		logger.Warn("TLSA lookup failed",
			zap.String("query", tlsaName),
			zap.Error(err))
		return &TLSALookupResult{
			ErrorReason: fmt.Sprintf("DNS query failed: %v", err),
		}, err
	}

	result := &TLSALookupResult{
		Records:        make([]*TLSARecord, 0),
		DNSSECValid:    validationResult.Authentic,
		DNSSECBogus:    validationResult.Bogus,
		DNSSECInsecure: validationResult.Insecure,
		ErrorReason:    validationResult.ErrorReason,
	}

	// Check for DNSSEC validation failure
	if validationResult.Bogus {
		logger.Error("DNSSEC validation failed (BOGUS)",
			zap.String("query", tlsaName),
			zap.String("reason", validationResult.ErrorReason))
		return result, fmt.Errorf("DNSSEC validation failed: %s", validationResult.ErrorReason)
	}

	// Check response code
	if msg.Rcode != dns.RcodeSuccess {
		if msg.Rcode == dns.RcodeNameError {
			logger.Debug("TLSA record not found (NXDOMAIN)",
				zap.String("query", tlsaName))
			return result, nil // Not an error, just no DANE
		}
		return result, fmt.Errorf("DNS query failed with rcode: %s", dns.RcodeToString[msg.Rcode])
	}

	// Parse TLSA records from response
	for _, rr := range msg.Answer {
		tlsa, ok := rr.(*dns.TLSA)
		if !ok {
			continue
		}

		record := &TLSARecord{
			Usage:        tlsa.Usage,
			Selector:     tlsa.Selector,
			MatchingType: tlsa.MatchingType,
			Certificate:  []byte(tlsa.Certificate),
			TTL:          tlsa.Hdr.Ttl,
			Domain:       hostname,
			Port:         port,
		}

		// Validate record
		if err := record.IsValid(); err != nil {
			logger.Warn("Invalid TLSA record",
				zap.String("query", tlsaName),
				zap.Error(err))
			continue
		}

		result.Records = append(result.Records, record)

		logger.Info("TLSA record found",
			zap.String("hostname", hostname),
			zap.Int("port", port),
			zap.Uint8("usage", record.Usage),
			zap.Uint8("selector", record.Selector),
			zap.Uint8("matching_type", record.MatchingType),
			zap.Bool("dnssec_valid", result.DNSSECValid))
	}

	if len(result.Records) == 0 {
		logger.Debug("No TLSA records found in response",
			zap.String("query", tlsaName))
	}

	return result, nil
}

// GetPreferredRecord selects the best TLSA record from multiple options
// Preference order: DANE-EE > DANE-TA > PKIX-EE > PKIX-TA
func GetPreferredRecord(records []*TLSARecord) *TLSARecord {
	if len(records) == 0 {
		return nil
	}

	// Preference order (most secure first)
	usagePreference := []uint8{
		TLSAUsageDANEEE, // Domain-issued cert (no CA needed)
		TLSAUsageDANETA, // Trust anchor
		TLSAUsagePKIXEE, // Service cert with CA
		TLSAUsagePKIXTA, // CA constraint
	}

	for _, usage := range usagePreference {
		for _, record := range records {
			if record.Usage == usage {
				return record
			}
		}
	}

	// Return first record if no preference match
	return records[0]
}

// GetUsageDescription returns human-readable description of TLSA usage
func GetUsageDescription(usage uint8) string {
	switch usage {
	case TLSAUsagePKIXTA:
		return "PKIX-TA: Certificate must chain to this CA (traditional PKI with DANE constraint)"
	case TLSAUsagePKIXEE:
		return "PKIX-EE: Certificate must match and be valid per PKIX (CA validation required)"
	case TLSAUsageDANETA:
		return "DANE-TA: Certificate must chain to this trust anchor (DANE-only, no external CA)"
	case TLSAUsageDANEEE:
		return "DANE-EE: Certificate must match exactly (most common, no CA validation)"
	default:
		return fmt.Sprintf("Unknown usage: %d", usage)
	}
}

// ParseTLSAFromString parses a TLSA record from string format
// Format: "usage selector matching-type certificate-data"
// Example: "3 1 1 0C72AC70B745AC19998811B131D662C9AC69DBDBE7CB23E5B514B56664C5D3D6"
func ParseTLSAFromString(s string) (*TLSARecord, error) {
	parts := strings.Fields(s)
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid TLSA format, expected 4 fields, got %d", len(parts))
	}

	var usage, selector, matchingType uint8
	if _, err := fmt.Sscanf(parts[0], "%d", &usage); err != nil {
		return nil, fmt.Errorf("invalid usage: %w", err)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &selector); err != nil {
		return nil, fmt.Errorf("invalid selector: %w", err)
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &matchingType); err != nil {
		return nil, fmt.Errorf("invalid matching type: %w", err)
	}

	certData, err := hex.DecodeString(parts[3])
	if err != nil {
		return nil, fmt.Errorf("invalid certificate data: %w", err)
	}

	record := &TLSARecord{
		Usage:        usage,
		Selector:     selector,
		MatchingType: matchingType,
		Certificate:  certData,
	}

	return record, record.IsValid()
}

// FormatTLSA formats a TLSA record as a DNS zone file entry
func (t *TLSARecord) FormatTLSA() string {
	return fmt.Sprintf("_%d._tcp.%s. %d IN TLSA %d %d %d %s",
		t.Port,
		t.Domain,
		t.TTL,
		t.Usage,
		t.Selector,
		t.MatchingType,
		hex.EncodeToString(t.Certificate))
}

// ShouldEnforceDANE determines if DANE should be enforced based on TLSA records
// RFC 7672 Section 2.1 - SMTP Security via Opportunistic DANE
func ShouldEnforceDANE(result *TLSALookupResult) bool {
	// Don't enforce if no DNSSEC or if DNSSEC failed
	if !result.DNSSECValid || result.DNSSECBogus {
		return false
	}

	// Don't enforce if no TLSA records
	if len(result.Records) == 0 {
		return false
	}

	// Enforce DANE if we have valid TLSA records with DNSSEC
	return true
}

// GetDANERequirement returns the security requirement level
type DANERequirement int

const (
	DANENone       DANERequirement = iota // No DANE available
	DANEOpportunistic                      // DANE available, try but don't enforce
	DANEMandatory                          // DANE with DNSSEC, must succeed
)

func GetDANERequirement(result *TLSALookupResult) DANERequirement {
	// No TLSA records or insecure zone
	if len(result.Records) == 0 || result.DNSSECInsecure {
		return DANENone
	}

	// DNSSEC validation failed (BOGUS)
	if result.DNSSECBogus {
		// This is a security issue - should not connect
		return DANEMandatory // Fail closed
	}

	// DNSSEC validated TLSA records
	if result.DNSSECValid {
		return DANEMandatory
	}

	// TLSA exists but no DNSSEC validation
	return DANEOpportunistic
}
