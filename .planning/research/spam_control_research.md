# Spam Control Module — Research

**Researched:** 2026-04-13
**Domain:** Anti-spam, threat intelligence, email flow control — Go SMTP MTA
**Confidence:** HIGH (SMTP/DNS/algorithm fundamentals) / MEDIUM (specific commercial thresholds)

---

## Summary

The existing system has a solid foundation: SPF/DKIM/DMARC/ARC for authentication, DANE/MTA-STS for transport security, greylisting for behavioral gating, and a SHA256-based burst detection engine. The gap is that the current spread prevention uses exact-match hashing — meaning near-duplicate spam variants (common in snowshoe and botnet campaigns) are invisible to it. The current system also has no reputation layer (DNSBL/URIBL), no behavioral scoring pipeline, no fuzzy hashing for near-duplicate detection, and no threat intelligence feed integration.

The single highest-impact addition is a **multi-DNSBL lookup engine with weighted scoring**. DNSBL lookups at connect time (before DATA) reject the majority of spam at near-zero CPU cost. The second-highest-impact addition is replacing SHA256 in the outbreak engine with **TLSH or SimHash** for near-duplicate clustering. Third is a **behavioral scoring pipeline** that aggregates signals across the connection lifecycle into a composite score used for accept/defer/reject decisions.

**Primary recommendation:** Build a `SpamScorer` struct that accumulates weighted signals from each phase (connect → EHLO → MAIL FROM → RCPT TO → DATA) and emits an `Action` (accept / defer / reject / quarantine) at each decision gate. Use TLSH for fuzzy outbreak detection, multi-DNSBL for IP/domain reputation, and Redis pub/sub for cross-node state sharing of outbreak clusters.

---

## Project Constraints (from CLAUDE.md)

- Use `code-review-graph` MCP tools before Grep/Glob/Read for codebase exploration.
- No locked decisions from CONTEXT.md (no CONTEXT.md exists for this phase).
- Research is greenfield for a new module; existing packages (`internal/security`, `internal/greylisting`, `internal/cluster`) are integration points.

---

## Existing Infrastructure Baseline

| Component | Location | Status | Gap |
|-----------|----------|--------|-----|
| SPF/DMARC | `internal/security/spf_dmarc.go` | Implemented | Results not fed into scoring pipeline |
| DKIM verify | `internal/security/dkim.go` | Implemented | Results not fed into scoring pipeline |
| ARC | `internal/security/arc.go` | Implemented | — |
| Greylisting | `internal/greylisting/greylisting.go` | Implemented | Hash is triplet key only, no scoring |
| Spread prevention | `internal/security/spread_prevention.go` | SHA256 exact-match | No fuzzy hashing; misses variants |
| Cluster state | `internal/cluster/state/redis.go` | Stub (TODO) | Needs implementation for cross-node sharing |
| DNSBL | — | **Missing** | Highest-impact gap |
| URIBL/SURBL | — | **Missing** | — |
| Bayesian filter | — | **Missing** | — |
| Behavioral scoring | — | **Missing** | — |
| Threat intel feeds | — | **Missing** | — |

---

## 1. Reputation Systems

### 1.1 IP Reputation — DNSBL Lookup Mechanics

DNSBLs are queried by reversing the client IP octets and appending the DNSBL zone as a DNS A-record lookup.

```
Client IP: 192.0.2.1
Query:     1.2.0.192.zen.spamhaus.org  →  A record lookup
Listed:    Returns 127.0.0.x (x encodes the list reason)
Not listed: NXDOMAIN
```

[ASSUMED] — standard DNSBL return code meanings; verify against each operator's documentation before hard-coding.

**Spamhaus ZEN** (union of SBL + XBL + PBL) return codes:
| Return | Meaning | Recommended Action |
|--------|---------|-------------------|
| 127.0.0.2 | SBL — Known spammer IP | Reject 550 |
| 127.0.0.3 | SBL CSS — Snowshoe spam | Reject 550 |
| 127.0.0.4–7 | XBL — Exploited/botnet | Reject 550 |
| 127.0.0.10–11 | PBL ISP-listed (dynamic) | Reject or high-score |
| 127.255.255.252–255 | Query limit hit (unauthenticated) | Switch to API key mode |

**SORBS** (sorbs.net) — broader categories including spam history, open relays, zombies. Return 127.0.0.2–127.0.0.10.

**Barracuda BRBL** (b.barracudacentral.org) — single-zone IP blacklist. Return 127.0.0.2 = listed. [ASSUMED] Free for low-volume; registration required.

**Senderscore** (senderscore.com) — unlike traditional DNSBLs, returns a score 0–100. Query: `{reversed_ip}.score.senderscore.com` returns 127.0.{score_byte}.0 where the third octet encodes the score. [ASSUMED] — verify current encoding against SenderScore documentation.

**Validity/ReturnPath** (formerly Return Path) — similar score mechanism.

### 1.2 Domain Reputation — DNSBL (URIBL/SURBL/DBL)

Applied to domains extracted from message body URLs and the MAIL FROM domain.

| List | Zone | What It Lists | Query Format |
|------|------|--------------|--------------|
| URIBL | multi.uribl.com | Domains in spam URLs | `{domain}.multi.uribl.com` |
| SURBL | multi.surbl.org | Domains in spam URLs | `{domain}.multi.surbl.org` |
| Spamhaus DBL | dbl.spamhaus.org | Spam/phish/malware domains | `{domain}.dbl.spamhaus.org` |
| IVMURI | uribl.ivmuri.com | URI spam domains | `{domain}.uribl.ivmuri.com` |

SURBL multi.surbl.org return codes use a bitmask in the last octet:
- 2 = SC (SpamCop)
- 4 = WS (website spam)
- 8 = PH (phishing)
- 16 = MW (malware)
- 64 = AB (AbuseButler)
- 128 = CR (Cruzeiro)

[ASSUMED] — bitmask details may change; check SURBL documentation.

### 1.3 Weighted Scoring Architecture

The correct pattern is a weighted composite score, not binary block/allow:

```go
// [ASSUMED] — scoring weights are heuristic; calibrate against your traffic
type DNSBLWeight struct {
    Zone   string
    Weight float64  // contribution to composite score
    Action string   // "score" | "reject" (some lists justify hard reject)
}

var DefaultDNSBLWeights = []DNSBLWeight{
    {Zone: "zen.spamhaus.org",   Weight: 8.0, Action: "reject"}, // Very reliable
    {Zone: "b.barracudacentral.org", Weight: 5.0, Action: "score"},
    {Zone: "bl.spamcop.net",    Weight: 3.0, Action: "score"},   // Higher false positive rate
    {Zone: "dnsbl.sorbs.net",   Weight: 2.0, Action: "score"},
}
// Score >= 10.0 → reject; 5.0–9.9 → defer/tarpit; < 5.0 → continue pipeline
```

### 1.4 Caching Strategy

Raw DNSBL lookups add 50–200ms latency per listed check. Cache results to avoid repeated DNS round-trips.

```go
type ReputationCache struct {
    cache  *lru.Cache[string, *CacheEntry]  // hashicorp/golang-lru already in go.mod
    ttl    time.Duration  // 30 minutes for negative (not listed), 5 minutes for positive
}
```

The project already has `github.com/hashicorp/golang-lru/v2` in go.mod. [VERIFIED: go.mod line 27]

**Critical:** Parallel DNSBL lookup with `errgroup`/goroutines is essential. Sequential lookups multiply latency.

```go
// Parallel lookup pattern
var wg sync.WaitGroup
results := make([]float64, len(zones))
for i, zone := range zones {
    wg.Add(1)
    go func(i int, zone string) {
        defer wg.Done()
        results[i] = lookupDNSBL(ctx, ip, zone)
    }(i, zone)
}
wg.Wait()
```

### 1.5 Rate Limiting DNS Queries

Spamhaus and others throttle unauthenticated queries at ~300k/day. Obtain a Data Query Service (DQS) API key for production. The key modifies the query zone:

```
{reversed_ip}.{api_key}.zen.dq.spamhaus.net
```

[ASSUMED] — DQS key format; verify current format from Spamhaus documentation at https://docs.spamhaus.com/

---

## 2. Content Analysis Techniques

### 2.1 Modern Bayesian Filtering

Classic Naive Bayes spam filtering (Paul Graham's "A Plan for Spam", 2002) remains competitive when modernized. The key improvements in 2024 practice:

**Token normalization:**
- Case-fold all tokens
- Expand HTML entities before tokenizing (`&nbsp;` → space)
- Strip zero-width characters (U+200B, U+FEFF) — commonly used to defeat token analysis
- Normalize unicode confusables (Cyrillic "а" ≠ Latin "a" — flag mixed-script as suspicious)
- Extract URLs as separate tokens, add the domain as a token

**Chi-squared combination** (Gary Robinson's variant) outperforms simple product method:
```
score = 1 - chi2_inv(2 * sum(ln(1-p_i)), 2n) / 2
where p_i = per-token spam probability
```
[ASSUMED] — this is well-established in SpamAssassin/Bogofilter literature; implementation details are stable.

**Orthogonal sparse bigram** (OSB): rather than single-token analysis, create bigrams with skip-grams (word at position i + word at position i+2, i+4). Dramatically improves phrase detection without combinatorial explosion. [ASSUMED based on SpamAssassin/Bogofilter documentation]

**Storage:** For a Go implementation without external dependencies, use a BoltDB-style or SQLite (already in go.mod) word frequency table. The project already has `modernc.org/sqlite` [VERIFIED: go.mod line 36].

Minimum training corpus: ~1000 spam + ~1000 ham messages before classifier is usable. Production systems retrain daily.

### 2.2 URL Reputation Scanning

Algorithm:
1. Extract all URLs from body (text/plain and text/html parts)
2. Decode HTML entities, follow single-hop redirects (HTTP HEAD, 3s timeout)
3. Extract apex domain (e.g., `https://evil.example.com/path` → `example.com`)
4. Query URIBL/SURBL/DBL for extracted domain
5. Check URL shorteners (bit.ly, tinyurl.com, etc.) — always expand, then check destination
6. Flag: IP-literal URLs (e.g., `http://192.168.1.1/...`) — strong spam signal
7. Flag: domains registered < 7 days ago (new-domain lookups via WHOIS or passive DNS) [ASSUMED: threshold from common anti-spam practice]

**Go library for URL extraction:** No canonical library exists. Implement with `golang.org/x/net/html` (parse HTML tree) + `net/url` (parse and normalize URLs). Both are in the standard library / already available via `golang.org/x/net` [VERIFIED: go.mod line 98].

### 2.3 Attachment Analysis — ClamAV Integration

ClamAV is the standard open-source antivirus. Integration options:

**Option A: clamd TCP socket (recommended)**
- Connect to `clamd` daemon via Unix socket or TCP
- Send `STREAM` command, pipe attachment bytes, receive `OK` or `FOUND: {virus_name}`
- Library: `github.com/dutchcoders/go-clamd` (well-maintained, ~200 stars, [ASSUMED: not verified in this session])
- Or implement raw protocol — it is extremely simple (4-byte size prefix + data)

**Option B: `clamdscan` subprocess**
- Slower, process-per-scan overhead
- Acceptable only for low-volume or batch scanning

**clamd protocol (raw, no library needed):**
```
→ zINSTREAM\0
→ [uint32 big-endian chunk_size][chunk_data]
→ [uint32 = 0]  (end of stream)
← "stream: OK\n"  or  "stream: {virus_name} FOUND\n"
```

### 2.4 YARA Rules Integration

YARA is used by commercial systems (Proofpoint, Mimecast) for pattern-based detection of email threats — particularly malicious macro documents and phishing kits.

**Go library:** `github.com/hillu/go-yara/v4` — CGo bindings to libyara. Requires libyara installed on the system. [ASSUMED: library status current as of knowledge cutoff]

**Alternative (no CGo):** Parse and match a subset of YARA rules in pure Go — feasible for simple string/regex rules. Full YARA (PE parsing, external modules) requires the C library.

**For MTA integration:** Focus YARA on:
- Attachment content type mismatches (e.g., `.docx` with PE header)
- Known phishing HTML templates (logo + credential-harvest form patterns)
- Suspicious PowerShell/VBA patterns in Office documents

### 2.5 Header Anomaly Detection

High-signal header checks that are cheap (regex/string comparison):

| Check | Signal | RFC |
|-------|--------|-----|
| `Received` chain hop count > 10 | Obfuscation | RFC 5321 §4.4 |
| `Received` timestamps going backwards | Forgery/relay reuse | RFC 5321 |
| `Message-ID` missing or malformed | Bot generation | RFC 5322 §3.6.4 |
| `Message-ID` domain ≠ MAIL FROM domain | Forgery | Heuristic |
| `Date` header > 24h in past or future | Bot generation | RFC 5322 §3.6.1 |
| Missing `From:` header | RFC violation | RFC 5322 §3.6.2 |
| `From:` display name contains email address | Phishing | Heuristic |
| `X-Mailer` value matching known spam tools | Weak signal | Heuristic |
| `Content-Type: multipart/mixed` with only one MIME part | Evasion | Heuristic |
| `Content-Transfer-Encoding: base64` on text/plain | Obfuscation | Heuristic |
| Non-ASCII in `Subject:` without RFC 2047 encoding | Bot generation | RFC 2047 |

---

## 3. Behavioral and Flow Analysis

### 3.1 Connection-Rate Limiting

The project already has `golang.org/x/time` in go.mod (token bucket) [VERIFIED: go.mod line 11].

**Architecture:** Per-IP token bucket with configurable rate and burst. The bucket state must survive across TCP connections from the same IP.

```go
// Per-IP rate limiter using token bucket
type ConnectionRateLimiter struct {
    mu      sync.Mutex
    buckets map[string]*rate.Limiter  // key: IP string
    // golang.org/x/time/rate is already available
}

// Rates (calibrate against traffic):
// - Unknown IP: 10 connections/minute, burst 5
// - Greylisted (passed): 30 connections/minute, burst 20
// - Authenticated sender: unlimited or high limit
```

### 3.2 Per-Sender Sending Patterns

Track per authenticated-sender sliding window statistics:

| Metric | Threshold (suggested) | Action |
|--------|----------------------|--------|
| Messages/hour | > 500 for new sender | Defer |
| Unique recipients/hour | > 200 | Flag + notify |
| New recipient domains/hour | > 50 distinct domains | Snowshoe signal |
| Bounce rate (NDR/message) | > 5% over 1 hour | Rate-limit |
| Complaint rate | > 0.1% | Suspend |

[ASSUMED] — thresholds are heuristic; calibrate against your traffic baseline.

### 3.3 Recipient Histogram Analysis

A legitimate bulk sender sends to one domain at a time (or a small set). A spammer sends to hundreds of domains simultaneously.

```go
type RecipientHistogram struct {
    domains     map[string]int   // domain → recipient count
    totalRcpts  int
    windowStart time.Time
}

// Entropy of domain distribution:
// Low entropy (all same domain) = likely legitimate bulk mail
// High entropy (many domains, 1 each) = snowshoe signal
func (h *RecipientHistogram) DomainEntropy() float64 {
    // Shannon entropy: -sum(p_i * log2(p_i))
    // p_i = count[domain_i] / totalRcpts
}
```

### 3.4 Snowshoe Spam Detection

Snowshoe spam distributes volume across many IPs and domains to stay below per-source reputation thresholds. Key signals:

1. **ASN concentration**: Many source IPs sharing the same ASN [ASSUMED: requires BGP data]
2. **Domain registration burst**: Multiple domains registered in same 24h window
3. **Message similarity across sources**: Same or near-identical content arriving from diverse IPs — this is the primary detection vector
4. **Low per-IP volume + high aggregate volume**: Each individual IP sends < 10 messages but 100 IPs send the same content
5. **PTR record patterns**: PTR records matching patterns like `mail[0-9]+.example.com` — sequential allocation

The existing spread prevention engine tracks per-content burst. To detect snowshoe, it needs fuzzy hashing (see Section 4) so near-identical variants are clustered.

### 3.5 Botnet C2 Callback Patterns in Email

Botnet-generated email has characteristic SMTP behavior:
- Connects to MX, sends DATA, does not read full SMTP response before closing (TCP FIN after DATA sent)
- High connection rate from residential IPs (no PTR record or ISP PTR format)
- EHLO hostname is random string, IP literal, or mismatched domain
- Sends to role addresses (info@, sales@, webmaster@) in alphabetical order — dictionary harvesting
- No QUIT command — just drops the connection
- Message-ID generated from random hex, not from a mail client pattern

---

## 4. Fuzzy Hashing for Outbreak Detection

### 4.1 Algorithm Comparison

| Algorithm | Hash Size | Similarity Score | Speed | Best For |
|-----------|-----------|-----------------|-------|---------|
| SSDEEP (context-triggered piecewise) | 64–128 chars | 0–100 edit distance | Slow (O(n²) compare) | Reference implementation |
| TLSH (Trend Micro Locality Sensitive Hash) | 70 bytes hex | 0–∞ distance (lower = more similar) | Fast | Email/malware clustering |
| SimHash | 64 bits | Hamming distance | Very fast | Large document clustering |
| MinHash/LSH | Configurable bands | Jaccard similarity | Fast lookup via LSH | Near-duplicate queries |
| nilsimsa | 32 bytes hex | 0–128 score | Fast | Email-specific (SpamAssassin uses it) |

**Recommendation for this project: TLSH** as primary, **SimHash** as secondary for quick pre-filter.

**Why TLSH over SSDEEP:**
- SSDEEP requires both hashes to be computed before comparison; TLSH comparison is more consistent
- TLSH is designed for files 50+ bytes and works well on email bodies
- TLSH distance threshold for "similar": typically 0–30 (very similar), 30–100 (related)
- SSDEEP requires minimum ~4KB input for reliable matching; email bodies are often smaller
- TLSH was developed by Trend Micro specifically for malware/email clustering [ASSUMED: from academic papers]

### 4.2 TLSH — Go Implementation Without External Dependencies

TLSH is a pure algorithm implementable in Go without CGo. The reference implementation is at https://github.com/trendmicro/tlsh [ASSUMED: repo exists, verify].

**Go port:** `github.com/glaslos/tlsh` — pure Go TLSH implementation. [ASSUMED: check current maintenance status before using]

**Core TLSH algorithm (implementable from scratch ~300 lines of Go):**

```
1. Byte quartiles: compute Q1, Q2, Q3 of byte value histogram
2. Checksum: 1–3 byte Pearson hash checksum of input
3. Length encoding: encode input length in log scale (5 bits)
4. Q ratio: encode (Q1/Q2) and (Q2/Q3) ratios (4 bits each)
5. Body hash: 128 Pearson hashes over 5-byte sliding windows → packed into 32 bytes
   Each hash feeds into a bucket; final 32-byte body = (count[i]/4) & 0x3 for each of 128 buckets

Similarity: XOR + popcount on body bytes + weighted distance on header fields
Threshold: distance < 30 = near-duplicate cluster
```

[ASSUMED] — TLSH internals based on Trend Micro paper "TLSH – A Locality Sensitive Hash" (2013, Jonathan Oliver et al.); the algorithm is stable and well-documented.

### 4.3 SimHash — Pure Go (No Dependencies)

SimHash is even simpler to implement from scratch:

```go
func SimHash(tokens []string) uint64 {
    v := [64]int{}
    for _, token := range tokens {
        h := fnv64(token)  // or any 64-bit hash
        for i := 0; i < 64; i++ {
            if (h>>i)&1 == 1 {
                v[i]++
            } else {
                v[i]--
            }
        }
    }
    var fingerprint uint64
    for i := 0; i < 64; i++ {
        if v[i] > 0 {
            fingerprint |= 1 << i
        }
    }
    return fingerprint
}

// Hamming distance ≤ 3 bits → near-duplicate (standard threshold)
func HammingDistance(a, b uint64) int {
    return bits.OnesCount64(a ^ b)  // math/bits, stdlib
}
```

SimHash threshold: Hamming distance ≤ 3 for near-duplicate email; ≤ 5 for "same campaign". [ASSUMED: common threshold from literature]

### 4.4 MinHash/LSH for Scalable Clustering

For high-volume environments (>100k messages/day), MinHash + LSH enables sub-linear similarity search:

**MinHash:** Represent each email as a set of k-shingles (overlapping n-grams of tokens), compute minimum hash values across h hash functions. Two emails' Jaccard similarity ≈ proportion of MinHash values that agree.

**LSH (Locality Sensitive Hashing):** Band the MinHash signature into b bands of r rows. Two emails are candidate pairs if they agree in at least one band. This allows O(1) candidate lookup instead of O(n) pairwise comparison.

```go
// k-shingle size: 3–5 words for email
// h = 200 hash functions (balance accuracy vs memory)
// b = 20 bands, r = 10 rows per band
// → detects similarity ≥ ~0.5 Jaccard with high probability
```

[ASSUMED] — LSH parameter values from "Mining of Massive Datasets" (Leskovec, Rajaraman, Ullman) which is the standard reference.

**Pure Go MinHash:** `github.com/dgryski/go-minhash` — well-maintained, pure Go. [ASSUMED: verify current status]

### 4.5 Nilsimsa — Email-Specific Alternative

Nilsimsa was designed specifically for spam email clustering and is used by SpamAssassin's Fuzzy Hashing plugin.

```
1. Build a "transition" histogram of 256 possible byte-pair transitions
2. Hash result into 32-byte (256-bit) digest
3. Similarity: count bit positions where hashes agree → 0–128 "score"
4. Threshold: score ≥ 100 (out of 128) = similar messages
```

Pure Go implementation is ~100 lines. [ASSUMED: no canonical Go library; implement from spec]

### 4.6 Upgrading the Existing SpreadPrevention

The current implementation uses `sha256.Sum256(body)` as cluster ID. Replace with:

```go
// Phase 1: Pre-normalize the body before hashing
// - Remove per-message unique identifiers (tracking pixels, unsubscribe tokens)
// - Strip email addresses from body (recipient personalization)
// - Normalize whitespace

// Phase 2: Compute both TLSH and SimHash
// - TLSH for primary clustering (handles content reordering)
// - SimHash as secondary fast pre-filter

// Phase 3: Bucket lookup
// - SimHash: check Hamming distance < 3 against all active cluster centroids
// - TLSH: verify true similarity on candidates (TLSH is more accurate but slower)
```

---

## 5. SMTP-Layer Signals

### 5.1 PTR/EHLO Mismatch Detection

```
Signal: EHLO claims "mail.example.com" but PTR for connecting IP is "ppp-1-2-3-4.isp.net"
Action: Score addition (+2.0), not hard rejection (legitimate forwarding scenarios exist)

Signal: EHLO is an IP literal "[1.2.3.4]" (RFC 5321 §4.1.1.1 allows but spammers abuse)
Action: Score addition (+3.0)

Signal: EHLO hostname does not resolve (NXDOMAIN)
Action: Score addition (+4.0) — very strong signal

Signal: EHLO hostname resolves but A record ≠ connecting IP
Action: Score addition (+2.5)

Signal: PTR record exists but forward-confirmed reverse DNS (FCrDNS) fails
(PTR → A lookup does not match connecting IP)
Action: Score addition (+2.0)
```

### 5.2 NULL Sender Abuse (MAIL FROM:<>)

NULL sender (bounce/DSN messages, RFC 5321 §4.5.5) is heavily abused for backscatter and bounce spam. Controls:

1. Rate-limit NULL sender to max 10/minute per source IP [ASSUMED: threshold]
2. Require single recipient for NULL sender (RFC 5321 §4.5.5 intent)
3. If message body is not an NDR/DSN format, score +5.0

### 5.3 RFC 5321 Violation Detection

| Violation | Detection | Score |
|-----------|-----------|-------|
| PIPELINING without EHLO PIPELINING capability | Check command sequence | Hard reject |
| Command injection in MAIL FROM (semicolons, newlines) | Regex check | Hard reject |
| RCPT before MAIL FROM | Command sequence | 421 temp fail |
| DATA before any RCPT | Command sequence | 503 error |
| EHLO/HELO repeated > 2 times in session | Session state | +3.0 score |
| No QUIT — TCP FIN only | Session tracking | +1.5 score |
| Response to DATA ack not waited | Pipelining abuse | +4.0 score |

### 5.4 Backscatter Detection

Backscatter = NDR/bounce messages sent from third parties as a result of forged MAIL FROM.

**Detection signals:**
- High volume of inbound NULL-sender messages to non-existent local users
- Auto-reply (vacation/OOO) messages to NULL sender with no corresponding outbound message
- NDR messages with Message-ID not in outbound queue history

**Control:** BATV (Bounce Address Tag Validation) — sign the MAIL FROM address at send time so NDRs to forged addresses can be detected. SRS (Sender Rewriting Scheme) — rewrite MAIL FROM when forwarding so NDRs route correctly.

```
BATV signed address format:
prvs=KKKKK=user@domain.com
where KKKKK = time-keyed HMAC-derived tag
```

[CITED: RFC draft-levine-batv-02; BATV is widely described but never formally standardized]

### 5.5 Banner Grabbing Anomalies

The SMTP banner (220 response) reveals MTA software. Legitimate MTAs identify themselves. Anomalies:
- Banner contains no MTA identity string (just "220 OK")
- Banner contains version information that doesn't match SPF/DKIM signing patterns (rare)
- Banner response time < 100ms from a residential IP (suggests pre-loaded botnet stub)

---

## 6. Envelope-Level Flow Control

### 6.1 Tarpitting

Tarpitting = introducing artificial delays in SMTP responses to waste spammer time/resources.

**Implementation:** After scoring suggests likely spam (but not confident enough for hard reject), insert a `time.Sleep()` before sending each SMTP response. Standard tarpit delay: 10–30 seconds.

```go
func (s *Session) tarpit(ctx context.Context, delay time.Duration) {
    select {
    case <-time.After(delay):
    case <-ctx.Done():
    }
}
```

**RFC compliance:** RFC 5321 §4.5.3.2.7 states the server can take up to 10 minutes between responses. Tarpitting to 5 minutes is RFC-compliant. [CITED: RFC 5321 §4.5.3.2.7]

**Risk:** Tarpitting holds open file descriptors and goroutines. Cap concurrent tarpitted connections to avoid resource exhaustion.

### 6.2 Per-IP/Per-Domain Recipient Limits

```go
// Limits per SMTP session
const (
    MaxRecipientsPerSession    = 100    // RFC 5321 requires supporting ≥ 100
    MaxRecipientsUnknownSender = 10     // Tighter for unauthenticated
    MaxSessionsPerIP           = 5      // Concurrent connections
    MaxMessagesPerIPPerHour    = 100    // Sliding window
)
```

### 6.3 BATV Implementation

BATV prevents backscatter absorption by signing outbound MAIL FROM:

```go
func BATVSign(localpart, domain, key string) string {
    // Current date in days since epoch (mod 1000 for 3-digit tag)
    tag := time.Now().Unix() / 86400 % 1000
    // HMAC-SHA1 of (tag + localpart + domain), truncated to 4 hex chars
    mac := hmac.New(sha1.New, []byte(key))
    mac.Write([]byte(fmt.Sprintf("%03d%s%s", tag, localpart, domain)))
    sig := hex.EncodeToString(mac.Sum(nil))[:4]
    return fmt.Sprintf("prvs=%03d%s=%s@%s", tag, sig, localpart, domain)
}

func BATVVerify(address, key string) bool {
    // Parse prvs=KKK+tag=localpart@domain
    // Recompute expected tag, check within ±7 day window
}
```

[ASSUMED] — BATV format based on draft-levine-batv; implementation details should be verified against actual draft before deployment.

### 6.4 Challenge-Response (SMTP Callback Verification)

Verify that the MAIL FROM domain actually exists as a mail sender by making an SMTP callback:

```
1. Open TCP connection to MX of MAIL FROM domain
2. Send: EHLO our.domain
3. Send: MAIL FROM:<>
4. Send: RCPT TO:<{mail_from_address}>
5. If 550 → address does not exist → reject original message
6. If 250 → address exists → accept original message
7. Send: RSET, QUIT
```

[ASSUMED] — approach is well-known but controversial: it creates backscatter risk and some domains block callback probes. Should be rate-limited and optional.

---

## 7. Threat Intelligence Integration

### 7.1 Open-Source Feeds

| Feed | URL | Format | Update Freq | Content |
|------|-----|--------|------------|---------|
| abuse.ch URLhaus | https://urlhaus-api.abuse.ch/v1/ | JSON REST API | Real-time | Malware URLs |
| abuse.ch ThreatFox | https://threatfox-api.abuse.ch/api/v1/ | JSON REST API | Real-time | IOCs (IPs, domains, hashes) |
| abuse.ch SSLBL | https://sslbl.abuse.ch/blacklist/ | CSV/JSON | Daily | Malicious SSL cert fingerprints |
| MalwareBazaar | https://bazaar.abuse.ch/api/ | JSON REST API | Real-time | Malware file hashes |
| Spamhaus DROP | https://www.spamhaus.org/drop/ | Plain text list | Daily | Hijacked/stolen IP blocks |
| PhishTank | https://www.phishtank.com/developer_info.php | JSON | Hourly | Phishing URLs |
| OpenPhish | https://openphish.com/feed.txt | Plain text | ~6hr | Phishing URLs |

[ASSUMED] — API endpoints and formats; verify against current documentation as these services update their APIs.

**abuse.ch ThreatFox API example (Go):**
```go
resp, err := http.PostForm("https://threatfox-api.abuse.ch/api/v1/",
    url.Values{"query": {"search_ioc"}, "search_term": {ip}})
```

### 7.2 Commercial Feed Integration Patterns

Commercial threat intel feeds (Proofpoint ET Intelligence, Recorded Future, CrowdStrike Falcon) provide:
- TAXII 2.1 / STIX 2.1 formatted feeds [ASSUMED: current standard]
- REST APIs with API key authentication
- CSV/JSON bulk download + real-time push (webhook or Kafka)

**TAXII 2.1 is the standard for structured threat intelligence** — consider building a TAXII client for future commercial feed integration. [ASSUMED: TAXII 2.1 is current as of 2024-2025]

### 7.3 Local Threat Cache Architecture

```go
// TTL-based cache backed by SQLite (already in go.mod)
// Table schema:
// CREATE TABLE threat_cache (
//   indicator TEXT PRIMARY KEY,     -- IP, domain, URL, hash
//   indicator_type TEXT NOT NULL,   -- 'ip' | 'domain' | 'url' | 'sha256'
//   threat_type TEXT NOT NULL,      -- 'spam' | 'phish' | 'malware' | 'botnet'
//   score REAL NOT NULL,            -- 0.0–10.0
//   source TEXT NOT NULL,           -- feed name
//   expires_at INTEGER NOT NULL,    -- Unix timestamp
//   created_at INTEGER NOT NULL,
//   raw_data TEXT                   -- JSON blob of feed-specific data
// );
// CREATE INDEX idx_expires_at ON threat_cache(expires_at);

type ThreatCacheTTL struct {
    IPReputation   time.Duration  // 4 hours — IPs change ownership
    DomainRep      time.Duration  // 24 hours — domains are more stable
    URLIntel       time.Duration  // 1 hour — URLs get taken down quickly
    FileHash        time.Duration  // 7 days — file hashes are stable
}
```

### 7.4 Feed Refresh Strategy

```go
// Background goroutine pattern
type FeedRefresher struct {
    interval time.Duration   // per-feed refresh interval
    lastETag string          // HTTP ETag for conditional GET
    lastMod  time.Time       // If-Modified-Since
}

// Use conditional HTTP GET to avoid re-downloading unchanged feeds:
req.Header.Set("If-None-Match", lastETag)
req.Header.Set("If-Modified-Since", lastMod.Format(http.TimeFormat))
// 304 Not Modified → skip processing
```

---

## 8. Clustering and Distributed Coordination

### 8.1 Shared State Requirements for Spam Control

| State Type | Consistency Required | Staleness Tolerance | Volume |
|------------|---------------------|---------------------|--------|
| Outbreak cluster hashes | Eventually consistent | 30 seconds | Low (1 key per outbreak) |
| Per-IP rate counters | Eventually consistent | 5 seconds | High (1 key per active IP) |
| Reputation cache | Eventually consistent | 5 minutes | Medium |
| Greylist triplets | Strongly consistent | 0 (split-brain = dual greylist) | Medium |
| Bayesian word counts | Batch sync OK | Hours | Low |
| Threat intel cache | Read-heavy | 1 hour | Low write, high read |

### 8.2 Redis Pub/Sub for Outbreak Propagation

When a node detects a new outbreak (burst threshold crossed), publish to a Redis channel so other nodes immediately start filtering:

```go
// Publisher (node that detected outbreak)
redis.Publish(ctx, "outbreaks", json.Marshal(OutbreakEvent{
    ClusterID:  tlshHash,
    SimHash:    simhash,
    FirstSeen:  time.Now(),
    Count:      threshold,
    TTL:        30 * time.Minute,
}))

// Subscriber (all nodes including publisher)
pubsub := redis.Subscribe(ctx, "outbreaks")
for msg := range pubsub.Channel() {
    var event OutbreakEvent
    json.Unmarshal([]byte(msg.Payload), &event)
    localSpreadPrev.AddCluster(event)
}
```

[ASSUMED] — the project's Redis store is currently a stub (verified: `internal/cluster/state/redis.go` has TODO implementations). Completing the Redis store is a prerequisite.

**Redis library for Go:** The project does not currently use a Redis client library [VERIFIED: go.mod has no redis dependency]. Add `github.com/redis/go-redis/v9` (the official client, formerly `go-redis/redis`). [ASSUMED: go-redis/v9 is the current recommended version]

### 8.3 Alternative: Gossip Protocol (Memberlist)

For environments without Redis, a gossip protocol provides eventual consistency without a central broker:

**`github.com/hashicorp/memberlist`** — production-grade gossip protocol used by Consul, Serf. [ASSUMED: widely used, well-maintained]

```go
// Broadcast outbreak to all cluster members
ml.LocalNode().Meta = marshalOutbreakState(currentOutbreaks)
// Push via delegate: memberlist.Delegate.NotifyMsg / .GetBroadcasts
```

Gossip convergence time: O(log n) rounds × gossip interval. With 10 nodes and 200ms interval, convergence < 2 seconds. [ASSUMED: from memberlist documentation]

**Tradeoff:** Redis pub/sub has lower latency (~1ms) but requires Redis infrastructure. Gossip has no single point of failure but higher convergence time.

### 8.4 Distributed Rate Counters

Use Redis INCR + EXPIRE for distributed rate limiting (sliding window approximation):

```go
// Fixed window (simpler, slight boundary artifact):
key := fmt.Sprintf("rate:%s:%d", ip, time.Now().Unix()/60)  // per-minute bucket
count, _ := redis.Incr(ctx, key)
redis.Expire(ctx, key, 2*time.Minute)  // keep for 2 windows

// Sliding window log (exact but higher memory):
// Use Redis ZADD with score=timestamp, ZCOUNT for window, ZREMRANGEBYSCORE for cleanup
```

[ASSUMED] — Redis ZADD/ZCOUNT sliding window pattern is well-documented in Redis documentation.

### 8.5 CRDTs for Eventually Consistent Counters

For per-IP message counters that don't require strong consistency, Conflict-free Replicated Data Types (specifically G-Counter or PN-Counter) can be replicated via gossip without coordination:

```
G-Counter: each node maintains its own count; total = sum of all node counts
PN-Counter: pair of G-Counters (increments, decrements) for net count
```

This is appropriate for "messages from IP X in last hour" — approximate count is sufficient, and no coordination is needed. [ASSUMED: CRDT theory is well-established; implementation complexity is moderate]

---

## 9. Standard Go Library Options

| Problem | Recommended Library | Already in go.mod | Notes |
|---------|--------------------|--------------------|-------|
| DNS lookups (DNSBL) | `github.com/miekg/dns` | YES (line 6) | Already used for SPF/DKIM |
| LRU cache (reputation) | `github.com/hashicorp/golang-lru/v2` | YES (line 27) | — |
| SQLite (threat cache) | `modernc.org/sqlite` | YES (line 36) | Pure Go, no CGo |
| Rate limiting | `golang.org/x/time/rate` | YES (line 11) | Token bucket |
| HTML parsing (URL extract) | `golang.org/x/net/html` | YES (via golang.org/x/net line 98) | — |
| Metrics | `github.com/prometheus/client_golang` | YES (line 32) | Use for spam scoring metrics |
| UUID (message IDs) | `github.com/google/uuid` | YES (line 9) | — |
| Redis client | `github.com/redis/go-redis/v9` | **NO — needs adding** | Cluster state sharing |
| ClamAV | `github.com/dutchcoders/go-clamd` | **NO — needs adding** | [ASSUMED: verify] |
| YARA (optional) | `github.com/hillu/go-yara/v4` | **NO — CGo** | Optional, complex setup |
| TLSH (fuzzy hash) | `github.com/glaslos/tlsh` | **NO — needs adding** | Or implement ~300 lines |
| MinHash | `github.com/dgryski/go-minhash` | **NO — needs adding** | Or implement from spec |

---

## 10. Architecture Patterns

### 10.1 Scoring Pipeline — The Central Pattern

All anti-spam signals funnel into a single `SpamScore` that is evaluated at each SMTP gate:

```go
// internal/spam/scorer.go

type Score struct {
    Total    float64
    Signals  []Signal
    mu       sync.Mutex
}

type Signal struct {
    Name   string
    Value  float64
    Source string  // "dnsbl", "header", "content", "behavioral"
}

type Action int
const (
    ActionAccept     Action = iota  // score < 4.0
    ActionDefer                     // score 4.0–6.9  (421 temp fail)
    ActionQuarantine                // score 7.0–8.9  (accept to spam folder)
    ActionReject                    // score ≥ 9.0   (550 hard reject)
    ActionTarpit                    // score 5.0–6.9 from new IP (slow response)
)

func (s *Score) Add(name string, value float64, source string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.Total += value
    s.Signals = append(s.Signals, Signal{name, value, source})
}

func (s *Score) Decide(thresholds ScoreThresholds) Action {
    if s.Total >= thresholds.Reject     { return ActionReject }
    if s.Total >= thresholds.Quarantine { return ActionQuarantine }
    if s.Total >= thresholds.Defer      { return ActionDefer }
    return ActionAccept
}
```

### 10.2 SMTP Session Lifecycle with Scoring

```
TCP Connect
    → Check IP DNSBL (parallel, cached)            [+0 to +8.0]
    → Check connection rate limit                   [+0 or ActionReject]
    → Add PTR/rDNS signal                           [+0 to +2.0]

EHLO received
    → Check EHLO/PTR mismatch                       [+0 to +4.0]
    → Check EHLO resolves                           [+0 to +4.0]
    → Evaluate total score → possible tarpit

MAIL FROM received
    → Check domain DNSBL (DBL)                      [+0 to +6.0]
    → Check SPF                                     [-1.0 to +3.0]
    → Check NULL sender rate                        [+0 to +5.0]
    → BATV verify (for NDR/bounces)                [+0 or +4.0]

RCPT TO received (each)
    → Check recipient histogram                     [+0 to +3.0]
    → Check recipient domain entropy                [+0 to +2.0]

DATA received
    → Parse headers, check anomalies                [+0 to +6.0]
    → Extract URLs, check URIBL/SURBL               [+0 to +8.0]
    → Check DKIM                                    [-2.0 to +2.0]
    → Bayesian classification                       [0 to +8.0]
    → TLSH fuzzy hash — check outbreak cluster      [+0 or +10.0 (outbreak)]
    → Attachment ClamAV scan                        [+0 or hard reject]
    → Final score → Accept/Quarantine/Reject
```

### 10.3 Recommended Project Structure

```
internal/spam/
├── scorer.go          # SpamScore struct, Action enum, thresholds
├── dnsbl.go           # IP + domain DNSBL lookup engine, parallel, cached
├── uribl.go           # URL extraction + URIBL/SURBL/DBL lookup
├── header.go          # Header anomaly detection
├── behavioral.go      # Rate limiters, recipient histogram, session tracking
├── fuzzy/
│   ├── tlsh.go        # TLSH implementation (pure Go)
│   ├── simhash.go     # SimHash implementation (pure Go)
│   └── cluster.go     # Near-duplicate cluster management
├── bayes/
│   ├── classifier.go  # Bayesian filter, Chi-squared combination
│   ├── tokenizer.go   # Token extraction, normalization
│   └── store.go       # SQLite word frequency storage
├── threats/
│   ├── cache.go       # SQLite-backed TTL threat cache
│   ├── feeds.go       # Feed refresh (URLhaus, ThreatFox, DROP)
│   └── clamav.go      # ClamAV clamd protocol client
└── distributed/
    ├── redis.go        # Redis pub/sub for outbreak propagation
    └── gossip.go       # Memberlist alternative for no-Redis setups
```

---

## 11. Common Pitfalls

### Pitfall 1: Sequential DNSBL Lookups
**What goes wrong:** Checking 5 DNSBLs sequentially adds 500–1000ms to every connection.
**Root cause:** Naive loop over zones.
**Prevention:** Always use goroutines + WaitGroup for parallel DNSBL queries. Use cached results (LRU, 30min TTL).
**Detection:** Measure time.Since(start) at connection accept; > 200ms means sequential lookups.

### Pitfall 2: Exact Hash for Near-Duplicate Detection
**What goes wrong:** Spammers change one word, add invisible characters, or reorder sentences. SHA256 diverges completely.
**Root cause:** The existing SpreadPrevention uses `sha256.Sum256(body)` [VERIFIED: spread_prevention.go line 45].
**Prevention:** Pre-normalize (strip tracking tokens, whitespace-normalize), then compute TLSH/SimHash.

### Pitfall 3: Greylisting Whitelisting Large ESPs
**What goes wrong:** Gmail, Outlook, SendGrid, Mailchimp all use rotating IPs. They will greylist forever unless the whitelist is IP-range based (CIDR) or domain-based, not just per-IP triplets.
**Prevention:** Maintain an ESP IP range whitelist. Consult published sending IP lists (Google publishes `_spf.google.com` SPF records, Mailchimp publishes `spf.mandrillapp.com`).

### Pitfall 4: DNSBL Over-Blocking Legitimate Forwarding
**What goes wrong:** University mail servers forward to Gmail; the intermediate hop appears to be the source. The forwarder's IP may not be listed, but the original sender's IP can leak into `Received` headers.
**Prevention:** Check only the most recent untrusted hop (the IP that connected directly to your server), not IPs in `Received` headers.

### Pitfall 5: Bayesian Filter Poisoning
**What goes wrong:** Spammers send clean-looking messages specifically designed to train your Bayesian filter toward spam-like tokens appearing in legitimate email ("Bayesian poisoning").
**Prevention:** Limit training inputs to verified spam (from honeypots/spamtraps) and verified ham (user-confirmed). Rate-limit retraining. Monitor spam capture rate for sudden drops.

### Pitfall 6: DNS TTL Violation — Ignoring DNSBL TTLs
**What goes wrong:** Caching DNSBL negative results (not-listed) too long means newly listed IPs aren't caught. Caching positive results too long means delisted IPs stay blocked.
**Prevention:** Respect DNS TTL returned in the query response. Spamhaus ZEN typically returns TTL=300 (5 minutes) for positive results, higher for negatives. [ASSUMED: TTL values; verify against actual DNS responses]

### Pitfall 7: Resource Exhaustion from Tarpitting
**What goes wrong:** 1000 tarpitted connections each holding a goroutine and file descriptor. With 30-second tarpit delay, that's 1000 * 30 = resource starvation.
**Prevention:** Cap concurrent tarpitted connections (e.g., max 100). When cap is hit, switch to hard reject instead of tarpit.

### Pitfall 8: Outbreak Engine Memory Growth
**What goes wrong:** The existing `clusterHashes` map grows unbounded if the cleanup goroutine falls behind or panics.
**Root cause:** Current cleanup in `cleanupTask()` [VERIFIED: spread_prevention.go line 81] runs every `burstWindow` interval but uses the same lock as `Evaluate()`.
**Prevention:** Use a bounded map with LRU eviction (hashicorp/golang-lru already available) instead of an unbounded map with periodic cleanup.

---

## 12. State of the Art — 2024–2025

| Old Approach | Current Approach | Changed | Impact |
|--------------|-----------------|---------|--------|
| Single DNSBL check (SBL only) | Multi-list weighted scoring | 2015+ | Reduces false positives |
| SHA256 exact match for outbreaks | TLSH/SimHash fuzzy clustering | 2015+ | Catches campaign variants |
| Greylisting full IP triplet | Greylisting with ESP CIDR whitelist | 2018+ | Avoids blocking Google/M365 |
| Manual threat feed updates | Automated API polling with ETags | 2020+ | Real-time protection |
| SpamAssassin rules (regex-heavy) | ML-augmented scoring (LLM + Bayesian) | 2023+ | Lower false negative rate |
| ClamAV signatures only | ClamAV + YARA custom rules | 2018+ | Catches targeted attacks |
| Per-server rate limits | Distributed rate limits via Redis | 2018+ | Works across MTA cluster |
| Blocking on SHA256 file hash | Blocking on TLSH + SHA256 multi-hash | 2015+ | Catches recompiled malware |

**Emerging (2024–2025):**
- **LLM-based content classification** — major ESPs (Google, Microsoft) now use transformer-based models as a final scoring layer [ASSUMED: from published blog posts/research by Google and Microsoft security teams]. Not recommended to implement from scratch; can be integrated via an external API if acceptable latency (200-500ms).
- **ARC (Authenticated Received Chain)** — already in the project. Increasingly important for indirect mail flows (forwarding). Complements but does not replace SPF.
- **BIMI (Brand Indicators for Message Identification)** — ties verified domain logos to authenticated email. Growing adoption but primarily cosmetic for recipients; useful for legitimate sender verification. [ASSUMED: status as of 2025]

---

## 13. Implementation Priority by Impact/Effort Ratio

| Rank | Feature | Impact | Effort | Notes |
|------|---------|--------|--------|-------|
| 1 | Multi-DNSBL scoring (Spamhaus ZEN + 2 others) | Very High | Low | Use existing `miekg/dns`; add parallel lookup + LRU cache |
| 2 | EHLO/PTR anomaly detection | High | Low | Pure string comparison; add to session init |
| 3 | Header anomaly scoring | High | Low | Regex/string checks on parsed headers |
| 4 | Replace SHA256 with TLSH in SpreadPrevention | High | Medium | Implement TLSH ~300 lines pure Go |
| 5 | URIBL/SURBL domain lookups on body URLs | High | Medium | URL extraction from HTML; use existing DNS lib |
| 6 | URLhaus/ThreatFox feed integration | High | Medium | HTTP polling + SQLite cache |
| 7 | Connection rate limiting per IP | Medium | Low | `golang.org/x/time/rate` already in go.mod |
| 8 | Recipient histogram / snowshoe detection | Medium | Medium | Session-level tracking |
| 9 | ClamAV integration (clamd protocol) | Medium | Low | Simple TCP protocol; optional dependency |
| 10 | Composite scoring pipeline (SpamScorer) | High | High | Ties all signals together; needed before production |
| 11 | Bayesian filter with SQLite storage | Medium | High | Requires training corpus; slower to show value |
| 12 | Redis outbreak sharing (cross-node) | Medium | High | Requires completing Redis store stub |
| 13 | BATV for bounce validation | Low-Medium | Medium | Useful for high-outbound-volume deployments |
| 14 | YARA attachment scanning | Low | High | CGo complexity; significant operational overhead |
| 15 | LLM content classification | Medium | Very High | External API dependency; latency concern |

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Spamhaus ZEN return codes 127.0.0.2–11 | §1.1 | Incorrect scoring; verify at docs.spamhaus.com |
| A2 | Barracuda BRBL requires registration | §1.1 | Queries silently ignored without registration |
| A3 | SenderScore encodes in third octet of return IP | §1.1 | Score parsing broken; check returnpath.com docs |
| A4 | SURBL multi bitmask values (2,4,8,16,64,128) | §1.2 | Incorrect category classification |
| A5 | TLSH distance < 30 = near-duplicate threshold | §4.2 | Too many or too few false positives |
| A6 | SimHash Hamming ≤ 3 = near-duplicate | §4.3 | Same as A5 |
| A7 | `github.com/glaslos/tlsh` is maintained | §4.2 | May need to implement TLSH from scratch |
| A8 | `github.com/dutchcoders/go-clamd` is maintained | §2.3 | Implement raw clamd protocol instead |
| A9 | `github.com/redis/go-redis/v9` is current version | §8.2 | Use v9 specifically (v10+ API may differ) |
| A10 | TLSH paper by Oliver et al. (Trend Micro, 2013) | §4.2 | Algorithm description accurate; paper is public |
| A11 | RFC 5321 §4.5.3.2.7 permits 10min between responses | §6.1 | Tarpitting compliance; verify against RFC text |
| A12 | abuse.ch API endpoints | §7.1 | Endpoints change; verify before implementation |
| A13 | BATV format `prvs=KKKKKK=` | §6.3 | Bounce parsing broken; check draft-levine-batv |
| A14 | Google, Microsoft use transformer models for spam | §12 | Informational only; no impact on implementation |

---

## Sources

### Primary (HIGH confidence — verified in-session)
- `go.mod` in this project — all verified library versions [VERIFIED]
- `internal/security/spread_prevention.go` — SHA256 exact-match confirmed [VERIFIED]
- `internal/greylisting/greylisting.go` — triplet hashing confirmed [VERIFIED]
- `internal/cluster/state/redis.go` — Redis implementation is stub (TODO) [VERIFIED]
- RFC 5321 (SMTP) — SMTP protocol behavior, tarpit compliance, NULL sender rules [CITED: IETF RFC 5321]
- RFC 5322 (Internet Message Format) — header requirements [CITED: IETF RFC 5322]
- RFC 7208 (SPF), RFC 7489 (DMARC) — referenced in codebase comments [CITED]

### Secondary (MEDIUM confidence — training knowledge + well-established references)
- TLSH: "TLSH – A Locality Sensitive Hash" — Jonathan Oliver, Chun Cheng, Yanggui Chen (Trend Micro, 2013) [CITED: academic paper]
- SimHash: Charikar (2002), "Similarity Estimation Techniques from Rounding Algorithms" [CITED: academic paper]
- MinHash/LSH: Leskovec, Rajaraman, Ullman — "Mining of Massive Datasets" (Cambridge, 2nd ed.) [CITED: textbook]
- SpamAssassin source and documentation — Bayesian/nilsimsa patterns [CITED: Apache SpamAssassin project]
- Spamhaus ZEN zone documentation [CITED: spamhaus.org — verify current at docs.spamhaus.com]
- SURBL documentation [CITED: surbl.org/surbl-analysis]

### Tertiary (ASSUMED — training knowledge, not verified this session)
- See Assumptions Log above for individual items requiring verification
- Commercial system thresholds (Cisco Talos, Proofpoint scoring weights) — proprietary, not publicly documented

---

## Open Questions

1. **ClamAV deployment model** — Is `clamd` available as a sidecar container or will this run inline? Raw clamd protocol integration vs. `go-clamd` library choice depends on deployment context.

2. **Redis availability** — The cluster/state/redis.go is a stub. Is Redis planned for the immediate roadmap? If not, design the spam module to work standalone with in-process LRU caches and add Redis integration later.

3. **Bayesian training corpus** — What spam/ham corpus is available? Without training data, the Bayesian filter cannot be deployed. Consider starting with SpamAssassin's public corpus (SA-Corpus) for bootstrap. [ASSUMED: SA-Corpus is available at spamassassin.apache.org/publiccorpus/]

4. **Throughput target** — At what volume (messages/second) will the spam module operate? This determines whether LSH is needed for fuzzy matching or whether linear TLSH comparison is sufficient. At < 100 msg/sec, linear comparison of all active clusters is fine.

5. **LLM integration feasibility** — Is external API latency acceptable? Google's Perspective API or a self-hosted LLM could be used as a final scoring layer, but adds 200–800ms to message delivery.

---

## Environment Availability

Step 2.6: The spam module is a new internal Go package with no external runtime dependencies except optional ClamAV and optional Redis. No environment audit is required for the code implementation itself. Deployment-time dependencies (ClamAV daemon, Redis) should be documented in deployment configuration.

| Dependency | Required By | Status |
|------------|------------|--------|
| `miekg/dns` | DNSBL lookups | In go.mod — available [VERIFIED] |
| `golang-lru/v2` | Reputation cache | In go.mod — available [VERIFIED] |
| `modernc.org/sqlite` | Threat cache + Bayes store | In go.mod — available [VERIFIED] |
| `golang.org/x/time/rate` | Rate limiting | In go.mod — available [VERIFIED] |
| `golang.org/x/net/html` | URL extraction | In go.mod (via x/net) — available [VERIFIED] |
| `github.com/redis/go-redis/v9` | Cross-node outbreak sharing | **NOT in go.mod — needs `go get`** |
| ClamAV `clamd` | Attachment scanning | Runtime daemon — optional feature |

---

## Validation Architecture

Test framework is not specified for this project (no test config detected in research). Recommended:

| Test Type | What to Test | Go Command |
|-----------|-------------|------------|
| Unit | TLSH/SimHash distance functions, scoring pipeline, BATV sign/verify | `go test ./internal/spam/...` |
| Integration | DNSBL lookup (against live or mock DNS) | `go test -tags integration ./internal/spam/dnsbl/...` |
| Benchmark | Fuzzy hash throughput, parallel DNSBL latency | `go test -bench=. ./internal/spam/...` |
| Manual | Full message classification with test corpus | Manual via `mailctl` or SMTP test script |

**Critical benchmarks to verify before deployment:**
- TLSH comparison: must be < 1ms per comparison for inline use
- DNSBL parallel lookup: must be < 100ms P95 with caching
- Bayesian classification: must be < 10ms per message
