package dane

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RFC 7672 - SMTP Security via Opportunistic DNS-Based Authentication of Named Entities (DANE)
// Complete DANE implementation for SMTP TLS validation

// DANEValidator provides DANE-based TLS certificate validation
type DANEValidator struct {
	resolver    *DNSSECResolver
	logger      *zap.Logger
	strictMode  bool // If true, reject connections when DANE validation fails
	cache       *tlsaCache
	mu          sync.RWMutex

	// Metrics
	lookupCount    uint64
	successCount   uint64
	failureCount   uint64
	cacheHitCount  uint64
}

// tlsaCache caches TLSA lookup results
type tlsaCache struct {
	entries map[string]*tlsaCacheEntry
	mu      sync.RWMutex
}

type tlsaCacheEntry struct {
	result    *TLSALookupResult
	expiresAt time.Time
}

// DANEResult represents the outcome of DANE validation
type DANEResult struct {
	Valid           bool
	DNSSECValid     bool
	DNSSECBogus     bool
	DNSSECInsecure  bool
	TLSARecords     []*TLSARecord
	MatchedRecord   *TLSARecord
	MatchedUsage    uint8
	Enforced        bool   // Was DANE enforcement applied?
	ErrorReason     string
	ValidationTime  time.Duration
}

// NewDANEValidator creates a new DANE validator
func NewDANEValidator(logger *zap.Logger, dnsServers []string, strictMode bool) *DANEValidator {
	resolver := NewDNSSECResolver(logger, dnsServers)

	// Load DNSSEC root trust anchors
	if err := resolver.LoadRootTrustAnchors(); err != nil {
		logger.Error("Failed to load DNSSEC root trust anchors", zap.Error(err))
	}

	// Start cache cleanup
	resolver.StartCleanup(5 * time.Minute)

	validator := &DANEValidator{
		resolver:   resolver,
		logger:     logger,
		strictMode: strictMode,
		cache:      newTLSACache(),
	}

	// Start cache cleanup
	go validator.cleanupCache(10 * time.Minute)

	return validator
}

func newTLSACache() *tlsaCache {
	return &tlsaCache{
		entries: make(map[string]*tlsaCacheEntry),
	}
}

// ValidateTLS performs DANE validation for a TLS connection
// This is the main entry point for DANE validation
func (v *DANEValidator) ValidateTLS(ctx context.Context, hostname string, port int, conn *tls.Conn) (*DANEResult, error) {
	startTime := time.Now()

	v.mu.Lock()
	v.lookupCount++
	v.mu.Unlock()

	v.logger.Debug("Starting DANE validation",
		zap.String("hostname", hostname),
		zap.Int("port", port))

	// Lookup TLSA records
	tlsaResult, err := v.LookupTLSA(ctx, hostname, port)
	if err != nil {
		v.logger.Error("TLSA lookup failed",
			zap.String("hostname", hostname),
			zap.Error(err))

		v.mu.Lock()
		v.failureCount++
		v.mu.Unlock()

		return &DANEResult{
			Valid:       false,
			ErrorReason: fmt.Sprintf("TLSA lookup failed: %v", err),
			ValidationTime: time.Since(startTime),
		}, err
	}

	// Check if DANE should be enforced
	requirement := GetDANERequirement(tlsaResult)

	result := &DANEResult{
		DNSSECValid:    tlsaResult.DNSSECValid,
		DNSSECBogus:    tlsaResult.DNSSECBogus,
		DNSSECInsecure: tlsaResult.DNSSECInsecure,
		TLSARecords:    tlsaResult.Records,
		Enforced:       requirement == DANEMandatory,
		ErrorReason:    tlsaResult.ErrorReason,
		ValidationTime: time.Since(startTime),
	}

	// No DANE available or insecure - allow connection
	if requirement == DANENone {
		v.logger.Debug("No DANE available, allowing connection",
			zap.String("hostname", hostname))
		result.Valid = true
		return result, nil
	}

	// DNSSEC validation failed (BOGUS) - this is suspicious
	if tlsaResult.DNSSECBogus {
		v.logger.Error("DNSSEC validation failed (BOGUS) - possible attack",
			zap.String("hostname", hostname),
			zap.String("reason", tlsaResult.ErrorReason))

		result.Valid = false
		result.ErrorReason = "DNSSEC validation failed (BOGUS)"

		v.mu.Lock()
		v.failureCount++
		v.mu.Unlock()

		if v.strictMode || requirement == DANEMandatory {
			return result, fmt.Errorf("DNSSEC validation failed: %s", tlsaResult.ErrorReason)
		}

		return result, nil
	}

	// Get the peer certificate
	connState := conn.ConnectionState()
	if len(connState.PeerCertificates) == 0 {
		result.Valid = false
		result.ErrorReason = "no peer certificates"

		v.mu.Lock()
		v.failureCount++
		v.mu.Unlock()

		return result, fmt.Errorf("no peer certificates")
	}

	cert := connState.PeerCertificates[0]
	chain := connState.PeerCertificates[1:]

	// Verify certificate against TLSA records
	match, err := VerifyCertificate(cert, chain, tlsaResult.Records, v.logger)
	if err != nil {
		v.logger.Warn("Certificate verification failed",
			zap.String("hostname", hostname),
			zap.Error(err))

		result.Valid = false
		result.ErrorReason = fmt.Sprintf("certificate verification failed: %v", err)

		v.mu.Lock()
		v.failureCount++
		v.mu.Unlock()

		if v.strictMode || requirement == DANEMandatory {
			return result, fmt.Errorf("DANE validation failed: %v", err)
		}

		return result, nil
	}

	// Validation succeeded
	result.Valid = match.Matched
	result.MatchedRecord = match.TLSARecord
	result.MatchedUsage = match.MatchedUsage

	v.mu.Lock()
	v.successCount++
	v.mu.Unlock()

	v.logger.Info("DANE validation successful",
		zap.String("hostname", hostname),
		zap.Int("port", port),
		zap.Uint8("usage", match.MatchedUsage),
		zap.Duration("validation_time", result.ValidationTime))

	return result, nil
}

// LookupTLSA queries TLSA records with caching
func (v *DANEValidator) LookupTLSA(ctx context.Context, hostname string, port int) (*TLSALookupResult, error) {
	// Check cache first
	cacheKey := v.getCacheKey(hostname, port)
	if cached := v.cache.get(cacheKey); cached != nil {
		v.mu.Lock()
		v.cacheHitCount++
		v.mu.Unlock()

		v.logger.Debug("TLSA cache hit",
			zap.String("hostname", hostname),
			zap.Int("port", port))
		return cached.result, nil
	}

	// Query TLSA records
	result, err := LookupTLSA(ctx, v.resolver, hostname, port, v.logger)
	if err != nil {
		return result, err
	}

	// Cache the result
	v.cache.set(cacheKey, result)

	return result, nil
}

// VerifyHostname performs DANE validation for a specific hostname/port
// without requiring an active connection (for pre-flight checks)
func (v *DANEValidator) VerifyHostname(ctx context.Context, hostname string, port int, cert *x509.Certificate, chain []*x509.Certificate) (*DANEResult, error) {
	startTime := time.Now()

	// Lookup TLSA records
	tlsaResult, err := v.LookupTLSA(ctx, hostname, port)
	if err != nil {
		return &DANEResult{
			Valid:       false,
			ErrorReason: fmt.Sprintf("TLSA lookup failed: %v", err),
			ValidationTime: time.Since(startTime),
		}, err
	}

	result := &DANEResult{
		DNSSECValid:    tlsaResult.DNSSECValid,
		DNSSECBogus:    tlsaResult.DNSSECBogus,
		DNSSECInsecure: tlsaResult.DNSSECInsecure,
		TLSARecords:    tlsaResult.Records,
		ValidationTime: time.Since(startTime),
	}

	// No TLSA records - no DANE
	if len(tlsaResult.Records) == 0 {
		result.Valid = true
		return result, nil
	}

	// Verify certificate
	match, err := VerifyCertificate(cert, chain, tlsaResult.Records, v.logger)
	if err != nil {
		result.Valid = false
		result.ErrorReason = fmt.Sprintf("verification failed: %v", err)
		return result, err
	}

	result.Valid = match.Matched
	result.MatchedRecord = match.TLSARecord
	result.MatchedUsage = match.MatchedUsage

	return result, nil
}

// GetTLSConfig returns a TLS config with DANE verification callback
// This integrates DANE into the TLS handshake
func (v *DANEValidator) GetTLSConfig(hostname string, port int) *tls.Config {
	return &tls.Config{
		ServerName:         hostname,
		InsecureSkipVerify: false, // Still verify with standard PKI
		MinVersion:         tls.VersionTLS12,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			// Parse certificates
			if len(rawCerts) == 0 {
				return fmt.Errorf("no certificates presented")
			}

			cert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return fmt.Errorf("failed to parse certificate: %w", err)
			}

			chain := make([]*x509.Certificate, 0, len(rawCerts)-1)
			for i := 1; i < len(rawCerts); i++ {
				c, err := x509.ParseCertificate(rawCerts[i])
				if err != nil {
					v.logger.Warn("Failed to parse chain certificate",
						zap.Int("index", i),
						zap.Error(err))
					continue
				}
				chain = append(chain, c)
			}

			// Perform DANE validation
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result, err := v.VerifyHostname(ctx, hostname, port, cert, chain)
			if err != nil {
				return fmt.Errorf("DANE validation failed: %w", err)
			}

			if !result.Valid && (v.strictMode || result.Enforced) {
				return fmt.Errorf("DANE validation failed: %s", result.ErrorReason)
			}

			return nil
		},
	}
}

// SetStrictMode enables or disables strict DANE enforcement
func (v *DANEValidator) SetStrictMode(strict bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.strictMode = strict

	v.logger.Info("DANE strict mode updated", zap.Bool("strict", strict))
}

// GetStats returns DANE validation statistics
func (v *DANEValidator) GetStats() map[string]interface{} {
	v.mu.RLock()
	defer v.mu.RUnlock()

	successRate := 0.0
	if v.lookupCount > 0 {
		successRate = float64(v.successCount) / float64(v.lookupCount) * 100
	}

	cacheHitRate := 0.0
	if v.lookupCount > 0 {
		cacheHitRate = float64(v.cacheHitCount) / float64(v.lookupCount) * 100
	}

	return map[string]interface{}{
		"lookups_total":      v.lookupCount,
		"success_total":      v.successCount,
		"failure_total":      v.failureCount,
		"cache_hits_total":   v.cacheHitCount,
		"success_rate_pct":   successRate,
		"cache_hit_rate_pct": cacheHitRate,
		"strict_mode":        v.strictMode,
	}
}

// getCacheKey generates a cache key for TLSA results
func (v *DANEValidator) getCacheKey(hostname string, port int) string {
	return fmt.Sprintf("%s:%d", hostname, port)
}

// Cache methods
func (c *tlsaCache) get(key string) *tlsaCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil
	}

	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return nil
	}

	return entry
}

func (c *tlsaCache) set(key string, result *TLSALookupResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Calculate TTL from TLSA records
	ttl := uint32(3600) // Default 1 hour
	if len(result.Records) > 0 {
		ttl = result.Records[0].TTL
	}

	c.entries[key] = &tlsaCacheEntry{
		result:    result,
		expiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
	}
}

func (c *tlsaCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}

// cleanupCache periodically removes expired cache entries
func (v *DANEValidator) cleanupCache(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		v.cache.cleanup()
	}
}

// CheckDANEAvailability checks if DANE is available for a domain
// This is useful for pre-flight checks before connecting
func (v *DANEValidator) CheckDANEAvailability(ctx context.Context, hostname string, port int) (*TLSALookupResult, error) {
	return v.LookupTLSA(ctx, hostname, port)
}

// String returns a human-readable summary of DANE result
func (r *DANEResult) String() string {
	if !r.Valid {
		return fmt.Sprintf("DANE validation failed: %s", r.ErrorReason)
	}

	if len(r.TLSARecords) == 0 {
		return "No DANE (TLSA records not found)"
	}

	if r.MatchedRecord != nil {
		return fmt.Sprintf("DANE validated (Usage %d, DNSSEC: %v)",
			r.MatchedUsage, r.DNSSECValid)
	}

	return "DANE available but not validated"
}
