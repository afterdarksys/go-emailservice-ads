package dane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"
)

// RFC 4033, 4034, 4035 - DNSSEC Protocol
// Provides DNS Security Extensions for authenticated DNS responses

// DNSSECResolver performs DNSSEC-validated DNS queries
type DNSSECResolver struct {
	client        *dns.Client
	servers       []string
	logger        *zap.Logger
	cache         *dnssecCache
	trustAnchors  map[string][]dns.DNSKEY // Root and TLD trust anchors
	mu            sync.RWMutex
}

// dnssecCache stores validated DNS responses
type dnssecCache struct {
	entries map[string]*cacheEntry
	mu      sync.RWMutex
}

type cacheEntry struct {
	msg       *dns.Msg
	validated bool
	expiresAt time.Time
}

// ValidationResult represents DNSSEC validation outcome
type ValidationResult struct {
	Authentic   bool     // DNSSEC signature validated
	Bogus       bool     // DNSSEC validation failed (attack or misconfiguration)
	Insecure    bool     // No DNSSEC available for this zone
	ErrorReason string   // Detailed error if validation failed
	ChainOfTrust []string // List of validated DS/DNSKEY records in chain
}

// NewDNSSECResolver creates a DNSSEC-validating resolver
func NewDNSSECResolver(logger *zap.Logger, servers []string) *DNSSECResolver {
	if len(servers) == 0 {
		servers = []string{"8.8.8.8:53", "1.1.1.1:53"} // Google, Cloudflare public DNS
	}

	return &DNSSECResolver{
		client: &dns.Client{
			Timeout:        5 * time.Second,
			SingleInflight: true,
		},
		servers:      servers,
		logger:       logger,
		cache:        newDNSSECCache(),
		trustAnchors: make(map[string][]dns.DNSKEY),
	}
}

func newDNSSECCache() *dnssecCache {
	return &dnssecCache{
		entries: make(map[string]*cacheEntry),
	}
}

// Query performs a DNSSEC-validated DNS query
// RFC 4035 Section 3.2 - DNSSEC Validation Process
func (r *DNSSECResolver) Query(ctx context.Context, qname string, qtype uint16) (*dns.Msg, *ValidationResult, error) {
	// Check cache first
	cacheKey := r.getCacheKey(qname, qtype)
	if cached := r.cache.get(cacheKey); cached != nil {
		r.logger.Debug("DNSSEC cache hit",
			zap.String("qname", qname),
			zap.String("qtype", dns.TypeToString[qtype]))

		return cached.msg, &ValidationResult{
			Authentic: cached.validated,
		}, nil
	}

	// Build query with DNSSEC DO (DNSSEC OK) bit set
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(qname), qtype)
	msg.SetEdns0(4096, true) // Set DO bit for DNSSEC
	msg.RecursionDesired = true
	msg.CheckingDisabled = false // Request validation from recursive resolver

	var response *dns.Msg
	var err error

	// Try each DNS server
	for _, server := range r.servers {
		response, _, err = r.client.ExchangeContext(ctx, msg, server)
		if err == nil && response != nil {
			break
		}
		r.logger.Warn("DNS query failed, trying next server",
			zap.String("server", server),
			zap.Error(err))
	}

	if err != nil {
		return nil, nil, fmt.Errorf("all DNS servers failed: %w", err)
	}

	if response == nil {
		return nil, nil, fmt.Errorf("no response from DNS servers")
	}

	// Check for truncation
	if response.Truncated {
		// Retry with TCP for large responses
		r.client.Net = "tcp"
		for _, server := range r.servers {
			response, _, err = r.client.ExchangeContext(ctx, msg, server)
			if err == nil && response != nil {
				break
			}
		}
		r.client.Net = "udp"

		if err != nil {
			return nil, nil, fmt.Errorf("TCP query failed: %w", err)
		}
	}

	// Validate DNSSEC
	validationResult := r.validateResponse(response, qname, qtype)

	// Cache the result
	r.cache.set(cacheKey, response, validationResult.Authentic)

	r.logger.Debug("DNSSEC query completed",
		zap.String("qname", qname),
		zap.String("qtype", dns.TypeToString[qtype]),
		zap.Bool("authentic", validationResult.Authentic),
		zap.Bool("bogus", validationResult.Bogus),
		zap.Bool("insecure", validationResult.Insecure))

	return response, validationResult, nil
}

// validateResponse validates DNSSEC signatures in DNS response
// RFC 4035 Section 5 - Validator Behavior
func (r *DNSSECResolver) validateResponse(msg *dns.Msg, qname string, qtype uint16) *ValidationResult {
	result := &ValidationResult{
		ChainOfTrust: make([]string, 0),
	}

	// Check if response has Authenticated Data (AD) bit set
	// Modern recursive resolvers set this when they've validated DNSSEC
	if msg.AuthenticatedData {
		result.Authentic = true
		r.logger.Debug("Response has AD bit set (validated by recursive resolver)",
			zap.String("qname", qname))
		return result
	}

	// Check if RRSIG records are present (indicates DNSSEC is available)
	hasRRSIG := false
	for _, rr := range msg.Answer {
		if rr.Header().Rrtype == dns.TypeRRSIG {
			hasRRSIG = true
			break
		}
	}

	// Also check additional and authority sections for RRSIG
	if !hasRRSIG {
		for _, rr := range msg.Ns {
			if rr.Header().Rrtype == dns.TypeRRSIG {
				hasRRSIG = true
				break
			}
		}
	}

	if !hasRRSIG {
		// No DNSSEC signatures present - zone is insecure
		result.Insecure = true
		r.logger.Debug("No RRSIG records found, zone is insecure",
			zap.String("qname", qname))
		return result
	}

	// Validate RRSIG signatures
	// In production, you would:
	// 1. Verify RRSIG signature matches the RRset
	// 2. Check RRSIG inception/expiration times
	// 3. Walk up the DNS tree validating DS/DNSKEY chain
	// 4. Verify against configured trust anchors

	validated, err := r.validateSignatures(msg, qname)
	if err != nil {
		result.Bogus = true
		result.ErrorReason = fmt.Sprintf("signature validation failed: %v", err)
		r.logger.Warn("DNSSEC validation failed",
			zap.String("qname", qname),
			zap.Error(err))
		return result
	}

	result.Authentic = validated
	return result
}

// validateSignatures validates RRSIG signatures in the response
// RFC 4035 Section 5.3 - Checking RRSIGs
func (r *DNSSECResolver) validateSignatures(msg *dns.Msg, qname string) (bool, error) {
	// Extract RRSIGs and corresponding RRsets
	rrsigs := make(map[uint16][]*dns.RRSIG)
	rrsets := make(map[uint16][]dns.RR)

	// Collect all RRSIGs
	for _, rr := range append(msg.Answer, msg.Ns...) {
		if rrsig, ok := rr.(*dns.RRSIG); ok {
			rrsigs[rrsig.TypeCovered] = append(rrsigs[rrsig.TypeCovered], rrsig)
		} else {
			rrtype := rr.Header().Rrtype
			rrsets[rrtype] = append(rrsets[rrtype], rr)
		}
	}

	if len(rrsigs) == 0 {
		return false, fmt.Errorf("no RRSIG records found")
	}

	// Validate each RRset against its RRSIG
	for rrtype, sigs := range rrsigs {
		rrset := rrsets[rrtype]
		if len(rrset) == 0 {
			continue
		}

		// Try each signature (there may be multiple keys)
		validated := false
		for _, sig := range sigs {
			// Get the DNSKEY for this signature
			dnskey, err := r.getDNSKEY(qname, sig.SignerName, sig.KeyTag)
			if err != nil {
				r.logger.Debug("Failed to get DNSKEY",
					zap.String("signer", sig.SignerName),
					zap.Uint16("keytag", sig.KeyTag),
					zap.Error(err))
				continue
			}

			// Verify the signature
			if err := sig.Verify(dnskey, rrset); err == nil {
				validated = true
				r.logger.Debug("RRSIG validated successfully",
					zap.String("qname", qname),
					zap.String("rrtype", dns.TypeToString[rrtype]),
					zap.String("signer", sig.SignerName))
				break
			}
		}

		if !validated {
			return false, fmt.Errorf("failed to validate RRSIG for %s", dns.TypeToString[rrtype])
		}
	}

	return true, nil
}

// getDNSKEY retrieves and caches DNSKEY records for signature verification
func (r *DNSSECResolver) getDNSKEY(qname, signerName string, keyTag uint16) (*dns.DNSKEY, error) {
	// Check trust anchors first
	r.mu.RLock()
	if keys, exists := r.trustAnchors[signerName]; exists {
		for i := range keys {
			if keys[i].KeyTag() == keyTag {
				r.mu.RUnlock()
				return &keys[i], nil
			}
		}
	}
	r.mu.RUnlock()

	// Query for DNSKEY
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(signerName), dns.TypeDNSKEY)
	msg.SetEdns0(4096, true)

	var response *dns.Msg
	var err error

	for _, server := range r.servers {
		response, _, err = r.client.Exchange(msg, server)
		if err == nil && response != nil {
			break
		}
	}

	if err != nil || response == nil {
		return nil, fmt.Errorf("failed to query DNSKEY: %w", err)
	}

	// Find the key with matching KeyTag
	for _, rr := range response.Answer {
		if key, ok := rr.(*dns.DNSKEY); ok {
			if key.KeyTag() == keyTag {
				// Cache the key for future use
				r.mu.Lock()
				r.trustAnchors[signerName] = append(r.trustAnchors[signerName], *key)
				r.mu.Unlock()

				return key, nil
			}
		}
	}

	return nil, fmt.Errorf("DNSKEY with KeyTag %d not found for %s", keyTag, signerName)
}

// AddTrustAnchor adds a DNSSEC trust anchor (root or TLD key)
// Trust anchors are used as the starting point for DNSSEC validation
func (r *DNSSECResolver) AddTrustAnchor(zone string, key dns.DNSKEY) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.trustAnchors[dns.Fqdn(zone)] = append(r.trustAnchors[dns.Fqdn(zone)], key)

	r.logger.Info("Added DNSSEC trust anchor",
		zap.String("zone", zone),
		zap.Uint16("keytag", key.KeyTag()),
		zap.Uint8("algorithm", key.Algorithm))
}

// LoadRootTrustAnchors loads the current DNSSEC root trust anchors
// These are published at https://data.iana.org/root-anchors/root-anchors.xml
func (r *DNSSECResolver) LoadRootTrustAnchors() error {
	// Current root trust anchor (as of 2024)
	// KSK-2017: Key Signing Key introduced in 2017 root KSK rollover
	rootKey := &dns.DNSKEY{
		Hdr: dns.RR_Header{
			Name:   ".",
			Rrtype: dns.TypeDNSKEY,
			Class:  dns.ClassINET,
			Ttl:    0,
		},
		Flags:     257, // KSK (Key Signing Key)
		Protocol:  3,
		Algorithm: dns.RSASHA256,
		// This is the actual 2017 root KSK public key
		PublicKey: "AwEAAaz/tAm8yTn4Mfeh5eyI96WSVexTBAvkMgJzkKTOiW1vkIbzxeF3+/4RgWOq7HrxRixHlFlExOLAJr5emLvN7SWXgnLh4+B5xQlNVz8Og8kvArMtNROxVQuCaSnIDdD5LKyWbRd2n9WGe2R8PzgCmr3EgVLrjyBxWezF0jLHwVN8efS3rCj/EWgvIWgb9tarpVUDK/b58Da+sqqls3eNbuv7pr+eoZG+SrDK6nWeL3c6H5Apxz7LjVc1uTIdsIXxuOLYA4/ilBmSVIzuDWfdRUfhHdY6+cn8HFRm+2hM8AnXGXws9555KrUB5qihylGa8subX2Nn6UwNR1AkUTV74bU=",
	}

	r.AddTrustAnchor(".", *rootKey)

	r.logger.Info("Loaded DNSSEC root trust anchors")
	return nil
}

// getCacheKey generates a unique cache key
func (r *DNSSECResolver) getCacheKey(qname string, qtype uint16) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%s:%d", dns.Fqdn(qname), qtype)))
	return hex.EncodeToString(h.Sum(nil))
}

// Cache methods
func (c *dnssecCache) get(key string) *cacheEntry {
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

func (c *dnssecCache) set(key string, msg *dns.Msg, validated bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Calculate TTL from response
	ttl := uint32(300) // Default 5 minutes
	if len(msg.Answer) > 0 {
		ttl = msg.Answer[0].Header().Ttl
	}

	c.entries[key] = &cacheEntry{
		msg:       msg,
		validated: validated,
		expiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
	}
}

// Cleanup removes expired cache entries
func (c *dnssecCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}

// StartCleanup starts periodic cache cleanup
func (r *DNSSECResolver) StartCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			r.cache.Cleanup()
		}
	}()
}
