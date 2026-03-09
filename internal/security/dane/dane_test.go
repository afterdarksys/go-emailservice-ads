package dane

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestTLSARecordValidation tests TLSA record validation
func TestTLSARecordValidation(t *testing.T) {
	tests := []struct {
		name    string
		record  *TLSARecord
		wantErr bool
	}{
		{
			name: "Valid DANE-EE SHA-256",
			record: &TLSARecord{
				Usage:        TLSAUsageDANEEE,
				Selector:     TLSASelectorSPKI,
				MatchingType: TLSAMatchingSHA256,
				Certificate:  make([]byte, 32), // 32 bytes for SHA-256
			},
			wantErr: false,
		},
		{
			name: "Valid DANE-TA SHA-512",
			record: &TLSARecord{
				Usage:        TLSAUsageDANETA,
				Selector:     TLSASelectorCert,
				MatchingType: TLSAMatchingSHA512,
				Certificate:  make([]byte, 64), // 64 bytes for SHA-512
			},
			wantErr: false,
		},
		{
			name: "Invalid usage value",
			record: &TLSARecord{
				Usage:        4, // Invalid
				Selector:     TLSASelectorSPKI,
				MatchingType: TLSAMatchingSHA256,
				Certificate:  make([]byte, 32),
			},
			wantErr: true,
		},
		{
			name: "Invalid selector value",
			record: &TLSARecord{
				Usage:        TLSAUsageDANEEE,
				Selector:     2, // Invalid
				MatchingType: TLSAMatchingSHA256,
				Certificate:  make([]byte, 32),
			},
			wantErr: true,
		},
		{
			name: "Invalid matching type",
			record: &TLSARecord{
				Usage:        TLSAUsageDANEEE,
				Selector:     TLSASelectorSPKI,
				MatchingType: 3, // Invalid
				Certificate:  make([]byte, 32),
			},
			wantErr: true,
		},
		{
			name: "Wrong SHA-256 hash length",
			record: &TLSARecord{
				Usage:        TLSAUsageDANEEE,
				Selector:     TLSASelectorSPKI,
				MatchingType: TLSAMatchingSHA256,
				Certificate:  make([]byte, 16), // Should be 32
			},
			wantErr: true,
		},
		{
			name: "Wrong SHA-512 hash length",
			record: &TLSARecord{
				Usage:        TLSAUsageDANEEE,
				Selector:     TLSASelectorSPKI,
				MatchingType: TLSAMatchingSHA512,
				Certificate:  make([]byte, 32), // Should be 64
			},
			wantErr: true,
		},
		{
			name: "Empty certificate data",
			record: &TLSARecord{
				Usage:        TLSAUsageDANEEE,
				Selector:     TLSASelectorSPKI,
				MatchingType: TLSAMatchingSHA256,
				Certificate:  []byte{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.record.IsValid()
			if (err != nil) != tt.wantErr {
				t.Errorf("IsValid() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestTLSARecordString tests TLSA record string representation
func TestTLSARecordString(t *testing.T) {
	record := &TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorSPKI,
		MatchingType: TLSAMatchingSHA256,
		Certificate:  make([]byte, 32),
	}

	str := record.String()
	if str == "" {
		t.Error("String() returned empty string")
	}

	// Should contain usage description
	if len(str) < 10 {
		t.Errorf("String() too short: %s", str)
	}
}

// TestGetPreferredRecord tests TLSA record preference ordering
func TestGetPreferredRecord(t *testing.T) {
	records := []*TLSARecord{
		{Usage: TLSAUsagePKIXTA, Selector: 0, MatchingType: 1, Certificate: make([]byte, 32)},
		{Usage: TLSAUsageDANEEE, Selector: 1, MatchingType: 1, Certificate: make([]byte, 32)},
		{Usage: TLSAUsagePKIXEE, Selector: 0, MatchingType: 1, Certificate: make([]byte, 32)},
		{Usage: TLSAUsageDANETA, Selector: 1, MatchingType: 1, Certificate: make([]byte, 32)},
	}

	preferred := GetPreferredRecord(records)
	if preferred == nil {
		t.Fatal("GetPreferredRecord returned nil")
	}

	// Should prefer DANE-EE (usage 3)
	if preferred.Usage != TLSAUsageDANEEE {
		t.Errorf("Expected DANE-EE (3), got usage %d", preferred.Usage)
	}
}

// TestGetPreferredRecordEmpty tests with no records
func TestGetPreferredRecordEmpty(t *testing.T) {
	records := []*TLSARecord{}
	preferred := GetPreferredRecord(records)
	if preferred != nil {
		t.Error("GetPreferredRecord should return nil for empty slice")
	}
}

// TestParseTLSAFromString tests parsing TLSA records from strings
func TestParseTLSAFromString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "Valid TLSA record",
			input:   "3 1 1 0C72AC70B745AC19998811B131D662C9AC69DBDBE7CB23E5B514B56664C5D3D6",
			wantErr: false,
		},
		{
			name:    "Valid DANE-TA record",
			input:   "2 0 1 E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855",
			wantErr: false,
		},
		{
			name:    "Invalid - too few fields",
			input:   "3 1 1",
			wantErr: true,
		},
		{
			name:    "Invalid - too many fields",
			input:   "3 1 1 AABBCC extra",
			wantErr: true,
		},
		{
			name:    "Invalid - bad usage",
			input:   "X 1 1 AABBCC",
			wantErr: true,
		},
		{
			name:    "Invalid - bad hex",
			input:   "3 1 1 ZZZZZ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := ParseTLSAFromString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTLSAFromString() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && record == nil {
				t.Error("Expected valid record, got nil")
			}
		})
	}
}

// TestDANERequirement tests DANE requirement determination
func TestDANERequirement(t *testing.T) {
	tests := []struct {
		name   string
		result *TLSALookupResult
		want   DANERequirement
	}{
		{
			name: "No TLSA records",
			result: &TLSALookupResult{
				Records:        []*TLSARecord{},
				DNSSECInsecure: true,
			},
			want: DANENone,
		},
		{
			name: "DNSSEC validated with TLSA",
			result: &TLSALookupResult{
				Records:     []*TLSARecord{{Usage: 3, Selector: 1, MatchingType: 1, Certificate: make([]byte, 32)}},
				DNSSECValid: true,
			},
			want: DANEMandatory,
		},
		{
			name: "DNSSEC bogus",
			result: &TLSALookupResult{
				Records:     []*TLSARecord{{Usage: 3, Selector: 1, MatchingType: 1, Certificate: make([]byte, 32)}},
				DNSSECBogus: true,
			},
			want: DANEMandatory, // Fail closed
		},
		{
			name: "TLSA without DNSSEC",
			result: &TLSALookupResult{
				Records:        []*TLSARecord{{Usage: 3, Selector: 1, MatchingType: 1, Certificate: make([]byte, 32)}},
				DNSSECValid:    false,
				DNSSECInsecure: false,
			},
			want: DANEOpportunistic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetDANERequirement(tt.result)
			if got != tt.want {
				t.Errorf("GetDANERequirement() = %v, want %v", got, tt.want)
			}
		})
	}
}

// generateTestCertificate creates a self-signed certificate for testing
func generateTestCertificate(t *testing.T) *x509.Certificate {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
			CommonName:   "mail.example.com",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"mail.example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	return cert
}

// TestGenerateTLSARecord tests TLSA record generation
func TestGenerateTLSARecord(t *testing.T) {
	cert := generateTestCertificate(t)

	tests := []struct {
		name         string
		usage        uint8
		selector     uint8
		matchingType uint8
		wantErr      bool
	}{
		{
			name:         "DANE-EE SPKI SHA-256",
			usage:        TLSAUsageDANEEE,
			selector:     TLSASelectorSPKI,
			matchingType: TLSAMatchingSHA256,
			wantErr:      false,
		},
		{
			name:         "DANE-EE Cert SHA-512",
			usage:        TLSAUsageDANEEE,
			selector:     TLSASelectorCert,
			matchingType: TLSAMatchingSHA512,
			wantErr:      false,
		},
		{
			name:         "DANE-TA SPKI Full",
			usage:        TLSAUsageDANETA,
			selector:     TLSASelectorSPKI,
			matchingType: TLSAMatchingFull,
			wantErr:      false,
		},
		{
			name:         "Invalid usage",
			usage:        4,
			selector:     TLSASelectorSPKI,
			matchingType: TLSAMatchingSHA256,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := GenerateTLSARecord(cert, tt.usage, tt.selector, tt.matchingType, 25, "example.com")
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateTLSARecord() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if record == nil {
					t.Error("Expected valid record, got nil")
					return
				}

				// Verify hash length
				switch tt.matchingType {
				case TLSAMatchingSHA256:
					if len(record.Certificate) != 32 {
						t.Errorf("SHA-256 hash should be 32 bytes, got %d", len(record.Certificate))
					}
				case TLSAMatchingSHA512:
					if len(record.Certificate) != 64 {
						t.Errorf("SHA-512 hash should be 64 bytes, got %d", len(record.Certificate))
					}
				}
			}
		})
	}
}

// TestCertificateVerification tests certificate matching against TLSA
func TestCertificateVerification(t *testing.T) {
	cert := generateTestCertificate(t)
	logger := zap.NewNop()

	// Generate matching TLSA record
	tlsaRecord, err := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingSHA256, 25, "example.com")
	if err != nil {
		t.Fatalf("Failed to generate TLSA record: %v", err)
	}

	tests := []struct {
		name       string
		cert       *x509.Certificate
		tlsaRecord *TLSARecord
		wantMatch  bool
	}{
		{
			name:       "Matching certificate",
			cert:       cert,
			tlsaRecord: tlsaRecord,
			wantMatch:  true,
		},
		{
			name:       "Non-matching certificate",
			cert:       generateTestCertificate(t), // Different cert
			tlsaRecord: tlsaRecord,
			wantMatch:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := VerifyCertificate(tt.cert, nil, []*TLSARecord{tt.tlsaRecord}, logger)

			if tt.wantMatch && err != nil {
				t.Errorf("Expected match but got error: %v", err)
			}

			if result != nil && result.Matched != tt.wantMatch {
				t.Errorf("VerifyCertificate() matched = %v, want %v", result.Matched, tt.wantMatch)
			}
		})
	}
}

// TestGetCertificateFingerprint tests certificate fingerprinting
func TestGetCertificateFingerprint(t *testing.T) {
	cert := generateTestCertificate(t)

	fingerprint := GetCertificateFingerprint(cert)
	if fingerprint == "" {
		t.Error("GetCertificateFingerprint returned empty string")
	}

	// Should be hex encoded SHA-256 (64 characters)
	if len(fingerprint) != 64 {
		t.Errorf("Expected fingerprint length 64, got %d", len(fingerprint))
	}

	// Should be consistent
	fingerprint2 := GetCertificateFingerprint(cert)
	if fingerprint != fingerprint2 {
		t.Error("Fingerprint should be consistent")
	}
}

// TestDANEValidatorCreation tests DANE validator creation
func TestDANEValidatorCreation(t *testing.T) {
	logger := zap.NewNop()

	validator := NewDANEValidator(logger, nil, false)
	if validator == nil {
		t.Fatal("NewDANEValidator returned nil")
	}

	if validator.resolver == nil {
		t.Error("DANE validator should have a resolver")
	}

	if validator.cache == nil {
		t.Error("DANE validator should have a cache")
	}
}

// TestDANEValidatorStats tests statistics tracking
func TestDANEValidatorStats(t *testing.T) {
	logger := zap.NewNop()
	validator := NewDANEValidator(logger, nil, false)

	stats := validator.GetStats()
	if stats == nil {
		t.Fatal("GetStats returned nil")
	}

	// Check expected keys
	expectedKeys := []string{
		"lookups_total",
		"success_total",
		"failure_total",
		"cache_hits_total",
		"success_rate_pct",
		"cache_hit_rate_pct",
		"strict_mode",
	}

	for _, key := range expectedKeys {
		if _, exists := stats[key]; !exists {
			t.Errorf("Stats missing key: %s", key)
		}
	}
}

// TestDANEStrictMode tests strict mode toggling
func TestDANEStrictMode(t *testing.T) {
	logger := zap.NewNop()
	validator := NewDANEValidator(logger, nil, false)

	// Should start with strict mode off
	stats := validator.GetStats()
	if stats["strict_mode"].(bool) {
		t.Error("Expected strict mode to be false initially")
	}

	// Enable strict mode
	validator.SetStrictMode(true)
	stats = validator.GetStats()
	if !stats["strict_mode"].(bool) {
		t.Error("Expected strict mode to be true after SetStrictMode(true)")
	}

	// Disable strict mode
	validator.SetStrictMode(false)
	stats = validator.GetStats()
	if stats["strict_mode"].(bool) {
		t.Error("Expected strict mode to be false after SetStrictMode(false)")
	}
}

// TestShouldPerformPKIXValidation tests PKIX validation requirement
func TestShouldPerformPKIXValidation(t *testing.T) {
	tests := []struct {
		usage uint8
		want  bool
	}{
		{TLSAUsagePKIXTA, true},
		{TLSAUsagePKIXEE, true},
		{TLSAUsageDANETA, false},
		{TLSAUsageDANEEE, false},
	}

	for _, tt := range tests {
		t.Run(GetUsageDescription(tt.usage), func(t *testing.T) {
			got := ShouldPerformPKIXValidation(tt.usage)
			if got != tt.want {
				t.Errorf("ShouldPerformPKIXValidation(%d) = %v, want %v", tt.usage, got, tt.want)
			}
		})
	}
}

// TestDANEResultString tests DANEResult string representation
func TestDANEResultString(t *testing.T) {
	tests := []struct {
		name   string
		result *DANEResult
	}{
		{
			name: "Successful validation",
			result: &DANEResult{
				Valid:       true,
				DNSSECValid: true,
				TLSARecords: []*TLSARecord{{Usage: 3, Selector: 1, MatchingType: 1, Certificate: make([]byte, 32)}},
				MatchedRecord: &TLSARecord{Usage: 3, Selector: 1, MatchingType: 1, Certificate: make([]byte, 32)},
				MatchedUsage: 3,
			},
		},
		{
			name: "Failed validation",
			result: &DANEResult{
				Valid:       false,
				ErrorReason: "Certificate mismatch",
			},
		},
		{
			name: "No DANE available",
			result: &DANEResult{
				Valid:       true,
				TLSARecords: []*TLSARecord{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			str := tt.result.String()
			if str == "" {
				t.Error("String() returned empty string")
			}
		})
	}
}

// BenchmarkTLSALookup benchmarks TLSA lookups (with mock resolver)
func BenchmarkTLSALookup(b *testing.B) {
	logger := zap.NewNop()
	resolver := NewDNSSECResolver(logger, nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = LookupTLSA(ctx, resolver, "example.com", 25, logger)
	}
}

// BenchmarkCertificateVerification benchmarks certificate verification
func BenchmarkCertificateVerification(b *testing.B) {
	cert := generateTestCertificate(&testing.T{})
	logger := zap.NewNop()

	tlsaRecord, _ := GenerateTLSARecord(cert, TLSAUsageDANEEE, TLSASelectorSPKI, TLSAMatchingSHA256, 25, "example.com")
	tlsaRecords := []*TLSARecord{tlsaRecord}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = VerifyCertificate(cert, nil, tlsaRecords, logger)
	}
}
