# DANE (DNS-Based Authentication of Named Entities) Implementation

## Table of Contents
- [Overview](#overview)
- [RFC Compliance](#rfc-compliance)
- [Architecture](#architecture)
- [Configuration](#configuration)
- [How It Works](#how-it-works)
- [Testing](#testing)
- [Deployment](#deployment)
- [Troubleshooting](#troubleshooting)
- [Performance](#performance)
- [Security Considerations](#security-considerations)

---

## Overview

### What is DANE?

**DANE (DNS-Based Authentication of Named Entities)** is a security protocol that allows domain owners to publish their TLS certificate information in DNS using TLSA records, secured by DNSSEC. This provides:

1. **Certificate Pinning via DNS** - No reliance on Certificate Authorities
2. **MITM Attack Prevention** - Cryptographically verifies server identity
3. **Defense in Depth** - Works alongside MTA-STS for maximum security
4. **Government Compliance** - Required by many .gov and high-security domains

### Why DANE Matters for Email

Traditional TLS certificate validation relies on Certificate Authorities (CAs). If a CA is compromised or issues a fraudulent certificate, attackers can intercept encrypted email. DANE solves this by:

- **Publishing certificate fingerprints in DNS** secured by DNSSEC
- **Eliminating CA dependencies** for domains with DANE
- **Providing cryptographic proof** of the correct certificate
- **Enabling strict TLS policies** that reject invalid connections

---

## RFC Compliance

This implementation complies with:

- **RFC 6698** - TLSA RR for DNS-Based Authentication of Named Entities
- **RFC 7671** - The DNS-Based Authentication of Named Entities (DANE) Protocol: Updates and Operational Guidance
- **RFC 7672** - SMTP Security via Opportunistic DNS-Based Authentication of Named Entities (DANE) Transport Layer Security (TLS)
- **RFC 4033, 4034, 4035** - DNSSEC Protocol Specifications

---

## Architecture

### Package Structure

```
internal/security/dane/
├── dane.go           # Main DANE validator with TLS integration
├── tlsa.go           # TLSA record parsing and lookup
├── dnssec.go         # DNSSEC validation and chain verification
├── verification.go   # Certificate matching against TLSA records
└── dane_test.go      # Comprehensive unit tests
```

### Key Components

#### 1. DANE Validator (`dane.go`)

The main entry point for DANE validation:

```go
type DANEValidator struct {
    resolver      *DNSSECResolver  // DNSSEC-validating DNS resolver
    logger        *zap.Logger
    strictMode    bool              // Reject on DANE failure?
    cache         *tlsaCache        // TLSA record cache
}
```

**Key Methods:**
- `ValidateTLS()` - Validate TLS connection with DANE
- `LookupTLSA()` - Query TLSA records with caching
- `VerifyHostname()` - Pre-flight DANE check
- `GetTLSConfig()` - TLS config with DANE verification callback

#### 2. TLSA Record Handler (`tlsa.go`)

Manages TLSA record parsing and validation:

```go
type TLSARecord struct {
    Usage        uint8   // 0-3: How to verify
    Selector     uint8   // 0-1: What to match
    MatchingType uint8   // 0-2: How to match
    Certificate  []byte  // Certificate or hash data
    TTL          uint32
    Domain       string
    Port         int
}
```

**TLSA Usage Types:**
- **0 (PKIX-TA)** - CA constraint
- **1 (PKIX-EE)** - Service certificate constraint
- **2 (DANE-TA)** - Trust anchor assertion
- **3 (DANE-EE)** - Domain-issued certificate (most common)

#### 3. DNSSEC Resolver (`dnssec.go`)

Validates DNSSEC signatures and chain of trust:

```go
type DNSSECResolver struct {
    client       *dns.Client
    servers      []string
    logger       *zap.Logger
    cache        *dnssecCache
    trustAnchors map[string][]dns.DNSKEY
}
```

**Features:**
- Full DNSSEC validation with signature verification
- Chain of trust validation from root to target
- Configurable trust anchors
- DNSSEC cache with TTL management
- Support for both UDP and TCP queries

#### 4. Certificate Verification (`verification.go`)

Matches certificates against TLSA records:

```go
func VerifyCertificate(
    cert *x509.Certificate,
    chain []*x509.Certificate,
    tlsaRecords []*TLSARecord,
    logger *zap.Logger
) (*CertificateMatch, error)
```

**Supports:**
- All 4 TLSA usage types
- Both selectors (full cert / SPKI)
- All matching types (full / SHA-256 / SHA-512)
- Certificate chain validation

---

## Configuration

### YAML Configuration

Add to your `config.yaml`:

```yaml
server:
  # ... existing config ...

  dane:
    enabled: true                    # Enable DANE validation
    strict_mode: false               # Opportunistic DANE (don't reject on failure)
    dns_servers:                     # DNS servers for DNSSEC queries (optional)
      - "8.8.8.8:53"                # Google Public DNS
      - "1.1.1.1:53"                # Cloudflare DNS
    cache_ttl: 3600                 # TLSA cache TTL in seconds
    timeout: 10                      # DANE validation timeout in seconds
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable DANE validation for outbound SMTP |
| `strict_mode` | bool | `false` | Reject delivery if DANE validation fails |
| `dns_servers` | []string | system defaults | Custom DNS servers for DNSSEC queries |
| `cache_ttl` | int | `3600` | TLSA record cache TTL (seconds) |
| `timeout` | int | `10` | DANE validation timeout (seconds) |

### Operational Modes

#### Opportunistic DANE (Recommended)
```yaml
dane:
  enabled: true
  strict_mode: false
```
- Attempts DANE validation when available
- Falls back to standard TLS if DANE unavailable
- Logs DANE failures but doesn't block delivery
- **Best for most deployments**

#### Strict DANE (High Security)
```yaml
dane:
  enabled: true
  strict_mode: true
```
- Requires successful DANE validation
- Rejects delivery if DANE validation fails
- Use only if all recipients support DANE
- **Recommended for government/high-security environments**

---

## How It Works

### Outbound SMTP with DANE

When sending email, the system performs these steps:

```
1. MX Lookup
   ↓
2. TLSA Lookup (_25._tcp.mx.example.com)
   ↓
3. DNSSEC Validation
   ↓
4. SMTP Connection (port 25)
   ↓
5. STARTTLS Handshake
   ↓
6. Certificate Verification Against TLSA
   ↓
7. Secure Email Delivery
```

### DANE Validation Flow

```go
// 1. Query TLSA records with DNSSEC
tlsaResult, err := validator.LookupTLSA(ctx, "mail.example.com", 25)

// 2. Check DNSSEC status
if tlsaResult.DNSSECBogus {
    // SECURITY ISSUE - possible attack
    return error
}

// 3. If TLSA records found, validate certificate
if len(tlsaResult.Records) > 0 {
    match, err := VerifyCertificate(cert, chain, tlsaResult.Records, logger)
    if !match.Matched {
        // Certificate doesn't match TLSA
        return error
    }
}
```

### TLSA Record Query

For hostname `mail.example.com` on port `25`:

```bash
# Query format: _<port>._tcp.<hostname>
dig +dnssec TLSA _25._tcp.mail.example.com
```

Example TLSA record:
```
_25._tcp.mail.example.com. 3600 IN TLSA 3 1 1 (
    0C72AC70B745AC19998811B131D662C9
    AC69DBDBE7CB23E5B514B56664C5D3D6
)
```

This means:
- **Usage 3** (DANE-EE) - Match end-entity certificate
- **Selector 1** (SPKI) - Match SubjectPublicKeyInfo
- **Matching Type 1** (SHA-256) - SHA-256 hash
- **Hash** - Certificate fingerprint

---

## Testing

### Unit Tests

Run comprehensive unit tests:

```bash
cd /Users/ryan/development/go-emailservice-ads
go test -v ./internal/security/dane/
```

Tests cover:
- ✅ All 4 TLSA usage types (PKIX-TA, PKIX-EE, DANE-TA, DANE-EE)
- ✅ Both selectors (full certificate, SPKI)
- ✅ All matching types (full, SHA-256, SHA-512)
- ✅ DNSSEC validation
- ✅ Certificate verification
- ✅ TLSA record parsing
- ✅ Cache functionality
- ✅ Error handling

### Test Against Real DANE Domains

Test with real-world DANE-enabled domains:

```bash
# Test domains with DANE
dig +dnssec TLSA _25._tcp.mail.sys4.de
dig +dnssec TLSA _25._tcp.mail.posteo.de
```

Popular DANE-enabled domains:
- `sys4.de` - German email provider
- `posteo.de` - Privacy-focused email
- Many `.gov` domains (US government)
- `ietf.org` - IETF infrastructure

### Manual Testing

1. **Send test email to DANE domain:**
```bash
# Configure system with DANE enabled
# Send email to address@posteo.de
# Check logs for DANE validation
```

2. **Monitor metrics:**
```bash
curl http://localhost:8080/metrics | grep dane
```

Expected output:
```
dane_lookups 45
dane_successes 42
dane_failures 3
dane_cache_hits 12
dnssec_validations 45
dnssec_failures 0
```

---

## Deployment

### Step 1: Enable DANE

Edit `config.yaml`:
```yaml
server:
  dane:
    enabled: true
    strict_mode: false  # Start with opportunistic
```

### Step 2: Configure DNS Servers (Optional)

For better DNSSEC support, use DNS servers with DNSSEC validation:

```yaml
server:
  dane:
    dns_servers:
      - "8.8.8.8:53"      # Google (supports DNSSEC)
      - "1.1.1.1:53"      # Cloudflare (supports DNSSEC)
```

### Step 3: Monitor Logs

Watch for DANE validation:
```bash
tail -f service.log | grep DANE
```

Expected log entries:
```
INFO  DANE records found, enabling DANE validation  mx_host=mail.example.com tlsa_records=1
INFO  DANE validation successful  hostname=mail.example.com port=25 usage=3
```

### Step 4: Review Metrics

Check Prometheus metrics:
```bash
curl http://localhost:8080/metrics
```

Key metrics:
- `dane_lookups` - Total TLSA lookups
- `dane_successes` - Successful validations
- `dane_failures` - Failed validations
- `dane_cache_hits` - Cache efficiency
- `dnssec_validations` - DNSSEC checks

### Step 5: Enable Strict Mode (Optional)

After confirming DANE works, optionally enable strict mode:

```yaml
server:
  dane:
    strict_mode: true  # Reject on DANE failure
```

⚠️ **Warning:** Only enable strict mode if you're confident all recipients support DANE or you can tolerate delivery failures.

---

## Troubleshooting

### Issue: DANE lookups failing

**Symptoms:**
```
WARN  TLSA lookup failed  query=_25._tcp.mail.example.com
```

**Solutions:**
1. Check DNS servers support DNSSEC:
   ```bash
   dig +dnssec example.com @8.8.8.8
   ```

2. Verify network allows DNS queries:
   ```bash
   nc -zv 8.8.8.8 53
   ```

3. Check firewall allows outbound UDP/TCP port 53

### Issue: DNSSEC validation failures

**Symptoms:**
```
ERROR DNSSEC validation failed (BOGUS)  query=_25._tcp.mail.example.com
```

**Solutions:**
1. Target domain's DNSSEC may be misconfigured
2. Check domain DNSSEC status:
   ```bash
   dig +dnssec +multi SOA example.com
   ```

3. Verify with online tools:
   - https://dnssec-debugger.verisignlabs.com/

### Issue: Certificate mismatch

**Symptoms:**
```
WARN  Certificate verification failed  hostname=mail.example.com
```

**Solutions:**
1. Domain's TLSA record may be outdated
2. Check current TLSA record:
   ```bash
   dig TLSA _25._tcp.mail.example.com
   ```

3. Compare certificate fingerprint:
   ```bash
   openssl s_client -connect mail.example.com:25 -starttls smtp \
     | openssl x509 -noout -fingerprint -sha256
   ```

### Issue: High DANE failure rate

**Check:**
1. Metrics dashboard: `dane_failures / dane_lookups`
2. If > 10%, investigate specific domains
3. Review TLS-RPT reports for patterns

---

## Performance

### Benchmarks

Typical performance metrics:

| Operation | Latency | Notes |
|-----------|---------|-------|
| TLSA Lookup (uncached) | < 100ms | First query to domain |
| TLSA Lookup (cached) | < 1ms | Cache hit |
| DNSSEC Validation | < 200ms | Full chain validation |
| Certificate Match | < 1ms | Hash comparison |
| **Total DANE Overhead** | **< 300ms** | First connection only |

### Caching Strategy

The implementation uses intelligent caching:

1. **TLSA Record Cache**
   - TTL from DNS record (typically 1-24 hours)
   - Reduces repeated lookups to same domain
   - Cache hit rate: typically > 90%

2. **DNSSEC Validation Cache**
   - Caches validated responses
   - Reduces signature verification overhead
   - Automatic cleanup of expired entries

### Performance Tips

1. **Use DNS servers with low latency:**
   ```yaml
   dns_servers:
     - "8.8.8.8:53"   # Test latency
   ```

2. **Monitor cache hit rate:**
   ```bash
   # Should be > 80%
   curl -s localhost:8080/metrics | grep dane_cache_hits
   ```

3. **Increase cache TTL if needed:**
   ```yaml
   dane:
     cache_ttl: 7200  # 2 hours
   ```

---

## Security Considerations

### Security Benefits

1. **MITM Protection**
   - DANE prevents certificate substitution attacks
   - DNSSEC ensures DNS responses are authentic
   - No reliance on potentially compromised CAs

2. **Certificate Pinning**
   - Domain owner controls which certificates are valid
   - Automatic rotation via DNS updates
   - No manual pinning configuration needed

3. **Defense in Depth**
   - Works alongside MTA-STS
   - DANE for sending, MTA-STS for receiving
   - Multiple layers of TLS security

### Security Risks

1. **DNSSEC Dependency**
   - Requires proper DNSSEC configuration
   - Misconfigured DNSSEC can block legitimate email
   - Monitor DNSSEC failures carefully

2. **DNS Poisoning**
   - Without DNSSEC, DANE is vulnerable
   - Always validate DNSSEC signatures
   - Use trusted DNS servers

3. **Strict Mode Risks**
   - Can cause delivery failures
   - Use only after thorough testing
   - Monitor failure rates closely

### Best Practices

1. **Start with Opportunistic Mode**
   ```yaml
   strict_mode: false
   ```

2. **Monitor DANE Metrics**
   - Set up alerts for high failure rates
   - Review TLS-RPT reports regularly
   - Track DNSSEC validation failures

3. **Use Trusted DNS Servers**
   - Google (8.8.8.8) or Cloudflare (1.1.1.1)
   - Verify they support DNSSEC
   - Test latency from your deployment

4. **Keep DNSSEC Root Keys Updated**
   - System includes current root trust anchor
   - Monitor for root key rollovers
   - Update before old keys expire

5. **Test Before Enabling Strict Mode**
   - Run in opportunistic mode for 30+ days
   - Analyze failure patterns
   - Only enable strict if failures < 1%

---

## Integration with TLS-RPT

DANE validation results are automatically reported via TLS-RPT:

### Success Report
```json
{
  "policy-type": "tlsa-usage-3",
  "total-successful-session-count": 1
}
```

### Failure Report
```json
{
  "policy-type": "tlsa",
  "result-type": "tlsa-invalid",
  "failed-session-count": 1,
  "additional-information": "Certificate did not match TLSA record"
}
```

---

## Metrics Reference

### Counters
- `dane_lookups` - Total TLSA lookups performed
- `dane_successes` - Successful DANE validations
- `dane_failures` - Failed DANE validations
- `dane_cache_hits` - TLSA cache hits
- `dnssec_validations` - Total DNSSEC validations
- `dnssec_failures` - DNSSEC validation failures

### Gauges
- `dane_enabled_domains` - Domains with DANE configured

### Calculated Metrics
- **Success Rate**: `dane_successes / dane_lookups * 100`
- **Cache Hit Rate**: `dane_cache_hits / dane_lookups * 100`
- **DNSSEC Failure Rate**: `dnssec_failures / dnssec_validations * 100`

---

## References

- [RFC 6698 - TLSA RR](https://tools.ietf.org/html/rfc6698)
- [RFC 7672 - SMTP DANE](https://tools.ietf.org/html/rfc7672)
- [RFC 4033 - DNSSEC Introduction](https://tools.ietf.org/html/rfc4033)
- [DANE SMTP How-To](https://github.com/internetstandards/toolbox-wiki/blob/main/DANE-for-SMTP-how-to.md)

---

## Support

For issues or questions:
1. Check logs: `tail -f service.log | grep DANE`
2. Review metrics: `curl localhost:8080/metrics | grep dane`
3. Test DNSSEC: `dig +dnssec TLSA _25._tcp.<target-domain>`
4. File GitHub issue with logs and DANE metrics
