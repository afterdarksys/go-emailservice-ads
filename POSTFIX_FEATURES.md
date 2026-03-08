# Postfix Features to Implement

## Priority 1: Critical Queue Management Features

### 1. Multiple Queue Types (Beyond Our Current Tiers)
**Current:** We have emergency/msa/int/out/bulk tiers
**Postfix has:**
- **active** - Messages being delivered right now
- **deferred** - Messages that failed temporarily (our retry system)
- **hold** - Messages administratively held (manual review)
- **corrupt** - Damaged queue files
- **incoming** - Messages being received
- **maildrop** - Messages submitted via sendmail interface

**Implementation Priority: HIGH**
```
Recommendation:
- Add "hold" queue for manual review/policy holds
- Add "incoming" staging area (currently we go straight to tier queues)
- Add "corrupt" for damaged messages (recovery)
```

### 2. Queue File Structure
**Postfix uses structured queue files with:**
- Message envelope (sender, recipients, timestamps)
- Message content (headers + body)
- Delivery status per recipient
- Queue file naming convention for sorting/aging

**Implementation Priority: HIGH**
```
We currently store:
- Full message in journal
- Should separate envelope from content
- Should track per-recipient delivery status (not per-message)
```

### 3. Queue Aging and Cleanup
**Postfix features:**
- `maximal_queue_lifetime` (default: 5 days)
- `bounce_queue_lifetime` (default: 5 days)
- `maximal_backoff_time` (default: 4000s)
- Automatic queue cleanup
- Queue file aging based on modification time

**Implementation Priority: HIGH**
```go
type QueueAging struct {
    MaxQueueLifetime   time.Duration // 5 days
    BounceAfter        time.Duration // 5 days
    CleanupInterval    time.Duration // 1 hour
}
```

## Priority 2: Delivery & Routing

### 4. Transport Maps
**Postfix routing:**
```
# transport maps decide HOW to deliver
example.com      smtp:[mail.example.com]
internal.local   lmtp:unix:/var/spool/cyrus/socket/lmtp
bulk.list.com    smtp:[bulk-relay.com]:587
```

**Implementation Priority: HIGH**
```go
type TransportMap struct {
    Domain    string
    Transport string // smtp, lmtp, pipe, local
    Nexthop   string // destination host/socket
    Options   map[string]string
}
```

### 5. Recipient Rewriting & Virtual Domains
**Postfix virtual aliasing:**
```
# virtual_alias_maps
user@olddomain.com    user@newdomain.com
sales@company.com     user1@company.com,user2@company.com

# canonical maps (address rewriting)
user               user@company.com
```

**Implementation Priority: MEDIUM**
```go
type VirtualAlias struct {
    Source      string
    Destination []string // can expand to multiple
}
```

### 6. Delivery Agents
**Postfix has multiple delivery agents:**
- **smtp** - External SMTP delivery
- **lmtp** - Local Mail Transfer Protocol (to IMAP server)
- **local** - Local mailbox delivery
- **virtual** - Virtual mailbox delivery
- **pipe** - Pipe to external program
- **maildir** - Maildir format delivery

**Implementation Priority: HIGH**
```
Currently: We simulate delivery
Should implement: Real delivery agents, starting with smtp/lmtp
```

## Priority 3: Security & Access Control

### 7. SMTP Restrictions Framework
**Postfix layered restrictions:**
```
# Per-stage restrictions
smtpd_client_restrictions =
    permit_mynetworks,
    reject_rbl_client zen.spamhaus.org,
    permit

smtpd_helo_restrictions =
    reject_invalid_helo_hostname,
    reject_non_fqdn_helo_hostname

smtpd_sender_restrictions =
    reject_non_fqdn_sender,
    reject_unknown_sender_domain

smtpd_recipient_restrictions =
    permit_mynetworks,
    permit_sasl_authenticated,
    reject_unauth_destination,
    reject_invalid_recipient_data

smtpd_data_restrictions =
    reject_unauth_pipelining
```

**Implementation Priority: HIGH**
```go
type SMTPRestrictions struct {
    ClientRestrictions    []RestrictionRule
    HeloRestrictions      []RestrictionRule
    SenderRestrictions    []RestrictionRule
    RecipientRestrictions []RestrictionRule
    DataRestrictions      []RestrictionRule
}

type RestrictionRule struct {
    Type   string // permit, reject, warn, defer
    Check  string // function name
    Action string // what to do
}
```

### 8. Access Control Maps
**Postfix access tables:**
```
# smtpd_client_access
192.168.1.0/24     OK
spammer.com        REJECT
friend.com         PERMIT

# smtpd_sender_access
user@spam.com      REJECT
sales@             OK
```

**Implementation Priority: MEDIUM**

### 9. Rate Limiting Per Client/Sender
**Postfix anvil daemon tracks:**
- Connection rate per client IP
- Message rate per client IP
- Recipient rate per client IP
- Authentication failure tracking

**Implementation Priority: HIGH**
```
We have: Global tier-based rate limiting
Need: Per-client/per-sender rate limiting

smtpd_client_connection_rate_limit = 10
smtpd_client_message_rate_limit = 20
smtpd_client_recipient_rate_limit = 50
```

## Priority 4: Bounce Handling & DSN

### 10. Proper Bounce Generation
**Postfix bounce features:**
- DSN (Delivery Status Notification) RFC 3464
- Bounce templates
- Delay notifications (4h, 24h warnings)
- Final bounce after max lifetime
- Non-delivery reports with original headers

**Implementation Priority: HIGH**
```go
type BounceHandler struct {
    DelayWarnings []time.Duration // [4h, 24h]
    BounceTemplate string
    IncludeHeaders int // how many original headers
    NotifyClasses  []string // delay, failure, success
}
```

### 11. Double Bounce Protection
**Postfix:** Bounces to postmaster, never bounces a bounce

**Implementation Priority: MEDIUM**

## Priority 5: Connection Management

### 12. Connection Caching
**Postfix smtp connection cache:**
- Reuse SMTP connections for multiple deliveries
- Connection TTL
- Max cached connections per destination

**Implementation Priority: MEDIUM**
```
Benefit: Huge performance improvement for bulk mail to same domain
Current: New connection per message
Should: Connection pooling per destination
```

### 13. Concurrency Controls
**Postfix delivery concurrency:**
```
# Per-destination concurrency
smtp_destination_concurrency_limit = 20
local_destination_concurrency_limit = 2

# Per transport
smtp_transport_rate_delay = 0s
default_destination_rate_delay = 0s
```

**Implementation Priority: HIGH**
```
We have: Worker pool per tier
Need: Per-destination concurrency limits
```

## Priority 6: Monitoring & Diagnostics

### 14. Queue Inspection Tools
**Postfix commands:**
```bash
mailq                    # List queue
postqueue -p             # Queue listing
postqueue -f             # Flush queue
postsuper -d <id>        # Delete message
postsuper -h <id>        # Hold message
postsuper -H <id>        # Release held message
postsuper -r <id>        # Requeue message
postcat -q <id>          # View queue file
```

**Implementation Priority: HIGH**
```
We have: mailctl queue list/stats
Need: Hold/release, per-message manipulation, queue flushing
```

### 15. Detailed Logging with Queue IDs
**Postfix logging:**
```
Jan 1 12:00:00 mail postfix/qmgr[1234]: 4F2A31234: from=<sender@example.com>, size=1234, nrcpt=1
Jan 1 12:00:01 mail postfix/smtp[1235]: 4F2A31234: to=<recipient@test.com>, relay=mail.test.com[1.2.3.4]:25, delay=0.5, status=sent
```

**Implementation Priority: HIGH**
```
We have: Basic logging
Need: Queue ID tracking across all log messages, structured logging
```

### 16. Statistics and Reporting
**Postfix provides:**
- pflogsumm (log analysis)
- Connection/delivery statistics
- Per-domain statistics

**Implementation Priority: MEDIUM**

## Priority 7: Content Filtering

### 17. Milter Protocol Support
**Postfix milters (mail filters):**
- SpamAssassin
- ClamAV
- OpenDKIM
- Rspamd

**Implementation Priority: MEDIUM**
```go
type MilterClient struct {
    Socket   string
    Timeout  time.Duration
    Protocol string // inet/unix
}
```

### 18. Header/Body Checks
**Postfix content inspection:**
```
# header_checks
/^Subject:.*viagra/i  REJECT
/^X-Spam-Flag: YES/   DISCARD

# body_checks
/\bclick here\b/i     WARN
```

**Implementation Priority: LOW** (We have MailScript for this)

## Priority 8: Reliability Features

### 19. Multiple MX Fallback
**Postfix MX handling:**
- Try all MX hosts in priority order
- Fall back to A record if no MX
- Randomize same-priority MX

**Implementation Priority: HIGH**

### 20. Soft Bounce vs Hard Bounce Detection
**Postfix SMTP reply classification:**
```
4xx = Soft bounce (retry)
5xx = Hard bounce (permanent failure)

Specific handling:
450-459 = Greylist (retry soon)
550 = Bad address (permanent)
552 = Mailbox full (maybe retry)
```

**Implementation Priority: HIGH**
```
We have: Basic permanent error detection
Need: Smarter 4xx/5xx classification, greylist handling
```

### 21. Sender-Dependent Authentication
**Postfix sender-dependent SASL:**
```
# sender_dependent_relayhost_maps
user1@company.com   [smtp.provider1.com]:587
user2@company.com   [smtp.provider2.com]:587

# smtp_sasl_password_maps
[smtp.provider1.com]:587  user1:password1
[smtp.provider2.com]:587  user2:password2
```

**Implementation Priority: MEDIUM**

## Priority 9: Address Handling

### 22. Address Extensions (Plus Addressing)
**Postfix recipient_delimiter:**
```
user+tag@domain.com → user@domain.com
user+spam@domain.com → user@domain.com

Useful for:
- Mail filtering
- Tracking where address leaked
- Per-service addresses
```

**Implementation Priority: LOW**

### 23. Generic Address Mapping
**Postfix smtp_generic_maps:**
```
# Rewrite outgoing From: addresses
root@internal.local     noreply@company.com
@internal.local         @company.com
```

**Implementation Priority: LOW**

## Priority 10: Configuration & Operations

### 24. Hot Reload Configuration
**Postfix:** `postfix reload` without dropping connections

**Implementation Priority: HIGH**
```
We have: Require restart
Need: SIGHUP handler for config reload
```

### 25. Lookup Table Abstraction
**Postfix supports:**
- hash: (Berkeley DB)
- btree:
- mysql:
- pgsql:
- ldap:
- memcache:
- tcp: (query external server)
- regexp:
- pcre:

**Implementation Priority: MEDIUM**
```go
type LookupTable interface {
    Get(key string) (value string, found bool, err error)
}

// Implementations:
// - MemoryTable (for testing)
// - FileTable (hash/btree)
// - SQLTable (mysql/postgres)
// - LDAPTable
// - RedisTable
```

### 26. Master Process Architecture
**Postfix master daemon:**
- Spawns/manages all services
- Connection hand-off to workers
- Automatic respawn on crash
- Resource limits per service

**Implementation Priority: MEDIUM**

## Implementation Roadmap

### Phase 1: Critical Queue Management (2-4 weeks)
1. ✅ Multi-tier queues (DONE)
2. ⬜ Hold queue for policy review
3. ⬜ Queue aging and cleanup
4. ⬜ Per-recipient delivery tracking
5. ⬜ Queue file structure improvements
6. ⬜ Queue manipulation commands (hold/release/delete)

### Phase 2: Delivery & Routing (3-4 weeks)
1. ⬜ Transport maps
2. ⬜ SMTP delivery agent (actual external delivery)
3. ⬜ LMTP delivery agent
4. ⬜ Virtual domain/alias support
5. ⬜ MX lookup and fallback
6. ⬜ Connection caching

### Phase 3: Security & Access Control (2-3 weeks)
1. ⬜ SMTP restrictions framework
2. ⬜ Access control maps
3. ⬜ Per-client rate limiting
4. ⬜ RBL/DNSBL support
5. ⬜ Enhanced SASL integration

### Phase 4: Bounce & DSN (1-2 weeks)
1. ⬜ DSN generation (RFC 3464)
2. ⬜ Delay notifications
3. ⬜ Bounce templates
4. ⬜ Double bounce protection
5. ⬜ Soft vs hard bounce detection

### Phase 5: Monitoring & Management (1-2 weeks)
1. ⬜ Enhanced mailctl commands
2. ⬜ Queue ID tracking in logs
3. ⬜ Statistics and reporting
4. ⬜ Hot config reload

### Phase 6: Advanced Features (3-4 weeks)
1. ⬜ Milter protocol support
2. ⬜ Lookup table abstraction
3. ⬜ Master process architecture
4. ⬜ Sender-dependent routing

## Configuration Examples

### Proposed config.yaml with Postfix-inspired features:

```yaml
server:
  addr: ":25"
  domain: "mail.example.com"

queue:
  # Queue aging
  maximal_queue_lifetime: "5d"
  bounce_queue_lifetime: "5d"
  maximal_backoff_time: "4000s"

  # Cleanup
  cleanup_interval: "1h"

  # Concurrency
  default_destination_concurrency_limit: 20
  local_destination_concurrency_limit: 2

smtp:
  # Restrictions
  client_restrictions:
    - permit_mynetworks
    - reject_rbl_client: zen.spamhaus.org
    - permit

  helo_restrictions:
    - reject_invalid_helo_hostname
    - reject_non_fqdn_helo_hostname

  recipient_restrictions:
    - permit_mynetworks
    - permit_sasl_authenticated
    - reject_unauth_destination

  # Rate limiting per client
  client_connection_rate_limit: 10
  client_message_rate_limit: 20
  client_recipient_rate_limit: 50

delivery:
  # Transport maps
  transport_maps:
    - file: /etc/mail/transport

  # Virtual aliases
  virtual_alias_maps:
    - file: /etc/mail/virtual

  # Connection caching
  smtp_connection_cache_on_demand: yes
  smtp_connection_cache_time_limit: 2s
  smtp_connection_reuse_time_limit: 60s

bounce:
  # DSN configuration
  delay_warning_time: "4h,24h"
  bounce_template: /etc/mail/bounce.template
  notify_classes: "delay,failure"

logging:
  level: "info"
  # Postfix-style logging with queue IDs
  format: "postfix"
  queue_id_length: 11  # Like Postfix: 4F2A31234AB
```

## Quick Wins (Implement First)

1. **Hold Queue** - Easy to add, very useful for spam review
2. **Queue Aging/Cleanup** - Prevent disk fill, essential
3. **Per-Client Rate Limiting** - Security critical
4. **Transport Maps** - Needed for real delivery
5. **SMTP Delivery Agent** - Currently we just simulate
6. **Queue ID Tracking** - Debugging essential
7. **Hot Config Reload** - Operational necessity
8. **Soft/Hard Bounce Detection** - Retry optimization

## Files to Create

```
internal/
├── delivery/
│   ├── smtp.go          # SMTP delivery agent
│   ├── lmtp.go          # LMTP delivery agent
│   ├── transport.go     # Transport map lookup
│   └── virtual.go       # Virtual alias expansion
├── restrictions/
│   ├── framework.go     # Restriction rule engine
│   ├── client.go        # Client restrictions
│   ├── helo.go          # HELO restrictions
│   ├── sender.go        # Sender restrictions
│   └── recipient.go     # Recipient restrictions
├── bounce/
│   ├── handler.go       # Bounce generation
│   ├── dsn.go           # DSN formatting
│   └── templates.go     # Bounce templates
├── lookup/
│   ├── table.go         # Lookup table interface
│   ├── file.go          # File-based tables
│   ├── sql.go           # SQL tables
│   └── ldap.go          # LDAP tables
└── qmgr/
    ├── aging.go         # Queue aging
    ├── cleanup.go       # Queue cleanup
    └── hold.go          # Hold queue
```
