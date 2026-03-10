# Dovecot Feature Comparison & Implementation Roadmap

## Overview

Dovecot is the industry-leading open-source IMAP/POP3 server. This document compares Dovecot's features with go-emailservice-ads and provides an implementation roadmap.

## Core IMAP Features

### Basic IMAP4rev1 (RFC 3501)

| Feature | Dovecot | go-emailservice-ads | Priority | Notes |
|---------|---------|---------------------|----------|-------|
| IMAP4rev1 protocol | ✅ | ✅ | - | Core implementation complete |
| LOGIN authentication | ✅ | ✅ | - | Implemented |
| AUTHENTICATE (PLAIN) | ✅ | ✅ | - | Implemented |
| SELECT/EXAMINE | ✅ | ✅ | - | Implemented |
| FETCH | ✅ | ✅ | - | Implemented |
| STORE (flags) | ✅ | ✅ | - | Implemented |
| SEARCH | ✅ | ✅ | - | Implemented |
| COPY | ✅ | ✅ | - | Implemented |
| EXPUNGE | ✅ | ✅ | - | Implemented |
| CREATE/DELETE mailbox | ✅ | ✅ | - | Implemented |
| RENAME mailbox | ✅ | ⚠️ | HIGH | Needs testing |
| SUBSCRIBE/UNSUBSCRIBE | ✅ | ⚠️ | MEDIUM | Implement subscription management |
| LIST/LSUB | ✅ | ✅ | - | Implemented |
| STATUS | ✅ | ✅ | - | Implemented |
| APPEND | ✅ | ✅ | - | Implemented |

### Extended IMAP Features

| Feature | RFC | Dovecot | go-emailservice-ads | Priority | Description |
|---------|-----|---------|---------------------|----------|-------------|
| **IDLE** | RFC 2177 | ✅ | ❌ | **CRITICAL** | Push notifications - users expect this |
| **CONDSTORE** | RFC 4551 | ✅ | ❌ | **CRITICAL** | Conditional STORE - efficient sync |
| **QRESYNC** | RFC 5162 | ✅ | ❌ | **CRITICAL** | Quick resync - fast reconnection |
| **ENABLE** | RFC 5161 | ✅ | ❌ | HIGH | Enable extensions |
| **SPECIAL-USE** | RFC 6154 | ✅ | ⚠️ | HIGH | Special mailbox attributes |
| **LIST-EXTENDED** | RFC 5258 | ✅ | ⚠️ | HIGH | Extended LIST command |
| **NOTIFY** | RFC 5465 | ✅ | ❌ | MEDIUM | Event notifications |
| **METADATA** | RFC 5464 | ✅ | ❌ | MEDIUM | Server/mailbox metadata |
| **ACL** | RFC 4314 | ✅ | ❌ | MEDIUM | Access Control Lists |
| **QUOTA** | RFC 2087 | ✅ | ❌ | HIGH | Mailbox quotas |
| **SORT** | RFC 5256 | ✅ | ⚠️ | HIGH | Server-side sorting |
| **THREAD** | RFC 5256 | ✅ | ❌ | MEDIUM | Conversation threading |
| **ESEARCH** | RFC 4731 | ✅ | ❌ | MEDIUM | Extended SEARCH |
| **SEARCHRES** | RFC 5182 | ✅ | ❌ | LOW | Search result variables |
| **WITHIN** | RFC 5032 | ✅ | ❌ | LOW | Date-relative search |
| **FILTERS** | RFC 5466 | ✅ | ❌ | LOW | Named search filters |
| **COMPRESS** | RFC 4978 | ✅ | ❌ | MEDIUM | DEFLATE compression |
| **UTF8=ACCEPT** | RFC 6855 | ✅ | ❌ | MEDIUM | UTF-8 support |
| **MOVE** | RFC 6851 | ✅ | ❌ | HIGH | Atomic move operation |
| **ID** | RFC 2971 | ✅ | ⚠️ | LOW | Client/server identification |
| **CHILDREN** | RFC 3348 | ✅ | ❌ | LOW | Mailbox hierarchy |
| **NAMESPACE** | RFC 2342 | ✅ | ⚠️ | MEDIUM | Namespace support |
| **UIDPLUS** | RFC 4315 | ✅ | ✅ | - | UID operations |
| **UNSELECT** | RFC 3691 | ✅ | ✅ | - | Close without expunge |
| **LITERAL+** | RFC 2088 | ✅ | ✅ | - | Non-sync literals |
| **LOGIN-REFERRALS** | RFC 2221 | ✅ | ❌ | LOW | Server referrals |
| **MULTIAPPEND** | RFC 3502 | ✅ | ❌ | MEDIUM | Multiple APPEND |
| **BINARY** | RFC 3516 | ✅ | ❌ | LOW | Binary content |
| **CATENATE** | RFC 4469 | ✅ | ❌ | LOW | APPEND concatenation |

## Critical Missing Features

### 1. IDLE (RFC 2177) - CRITICAL

**Why Critical**: Modern email clients rely on IDLE for real-time notifications. Without it, clients must poll the server constantly, wasting resources and delaying message delivery.

**Implementation Requirements**:

```go
// internal/imap/idle.go
package imap

import (
    "context"
    "sync"
    "time"
)

type IdleManager struct {
    mu         sync.RWMutex
    sessions   map[string]*IdleSession
    maxIdle    time.Duration
    heartbeat  time.Duration
}

type IdleSession struct {
    userID     string
    mailbox    string
    conn       net.Conn
    updates    chan *MailboxUpdate
    done       chan struct{}
    lastUpdate time.Time
}

type MailboxUpdate struct {
    Type     string  // "EXISTS", "EXPUNGE", "FETCH"
    SeqNum   uint32
    Flags    []string
}

func (m *IdleManager) StartIdle(ctx context.Context, session *Session) error {
    idleSession := &IdleSession{
        userID:     session.UserID,
        mailbox:    session.SelectedMailbox,
        conn:       session.Conn,
        updates:    make(chan *MailboxUpdate, 100),
        done:       make(chan struct{}),
        lastUpdate: time.Now(),
    }

    m.mu.Lock()
    m.sessions[session.ID] = idleSession
    m.mu.Unlock()

    // Send "+ idling" response
    session.WriteUntaggedLine("+ idling")

    // Start monitoring
    go m.monitorMailbox(idleSession)

    // Wait for DONE or timeout
    select {
    case <-idleSession.done:
        return nil
    case <-time.After(m.maxIdle):
        session.WriteUntaggedLine("* BYE Idle timeout")
        return ErrIdleTimeout
    case <-ctx.Done():
        return ctx.Err()
    }
}

func (m *IdleManager) monitorMailbox(session *IdleSession) {
    ticker := time.NewTicker(m.heartbeat)
    defer ticker.Stop()

    for {
        select {
        case <-session.done:
            return
        case <-ticker.C:
            // Check for mailbox changes
            updates := m.checkMailboxUpdates(session.userID, session.mailbox)
            for _, update := range updates {
                session.conn.Write(formatUpdate(update))
            }
        }
    }
}
```

### 2. CONDSTORE (RFC 4551) - CRITICAL

**Why Critical**: Enables efficient synchronization by tracking modification sequences. Clients can quickly determine what changed since last sync.

**Implementation Requirements**:

```go
// Add to mailbox metadata
type Mailbox struct {
    // ... existing fields ...
    HighestModSeq uint64  // Modification sequence counter
}

// Add to message metadata
type Message struct {
    // ... existing fields ...
    ModSeq uint64  // Per-message modification sequence
}

// CONDSTORE extension
func (s *Session) handleCondstore(cmd *Command) error {
    // FETCH with CHANGEDSINCE
    // FETCH 1:* (FLAGS) (CHANGEDSINCE 12345)
    //
    // Returns only messages with ModSeq > 12345

    modseq, err := cmd.GetArgument("CHANGEDSINCE")
    if err != nil {
        return err
    }

    messages := s.mailbox.GetMessagesChangedSince(modseq)
    for _, msg := range messages {
        s.sendFetchResponse(msg)
    }

    return nil
}
```

### 3. QRESYNC (RFC 5162) - CRITICAL

**Why Critical**: Fast reconnection after disconnect. Client can quickly resync without downloading all flags again.

**Implementation Requirements**:

```go
// SELECT with QRESYNC
// C: A01 SELECT INBOX (QRESYNC (67890007 20050715194045000 41,43:211,214:541))
//
// Server returns:
// * OK [CLOSED]
// * 99 EXISTS
// * 2 RECENT
// * OK [UIDVALIDITY 67890007]
// * OK [UIDNEXT 550]
// * OK [HIGHESTMODSEQ 67890012]
// * VANISHED (EARLIER) 41,43:116,118:210,214:540
// * 1 FETCH (UID 117 FLAGS (\Seen \Answered) MODSEQ (67890011))
// ...
// A01 OK [READ-WRITE] SELECT completed

func (s *Session) handleQresync(uidvalidity, highestModseq uint64, knownUIDs string) error {
    // 1. Verify UIDVALIDITY matches
    if uidvalidity != s.mailbox.UIDValidity {
        return s.sendFullResync()
    }

    // 2. Find vanished messages (UIDs that were deleted)
    vanished := s.mailbox.GetVanishedUIDs(knownUIDs, highestModseq)
    if len(vanished) > 0 {
        s.sendVanished(vanished)
    }

    // 3. Find changed messages (FLAGS/etc changed)
    changed := s.mailbox.GetChangedMessages(highestModseq)
    for _, msg := range changed {
        s.sendFetchResponse(msg)
    }

    // 4. Send new HIGHESTMODSEQ
    s.sendUntagged(fmt.Sprintf("OK [HIGHESTMODSEQ %d]", s.mailbox.HighestModSeq))

    return nil
}
```

### 4. QUOTA (RFC 2087) - HIGH

**Why Important**: Users need to know mailbox usage and limits.

**Implementation Requirements**:

```go
// GETQUOTA command
// C: A01 GETQUOTA "INBOX"
// S: * QUOTA "INBOX" (STORAGE 1024 10240)
// S: A01 OK GETQUOTA completed

type Quota struct {
    Root        string
    StorageUsed uint64  // KB used
    StorageLimit uint64  // KB limit
    MessageUsed uint32  // Message count
    MessageLimit uint32  // Message limit
}

func (s *Session) handleGetQuota(root string) error {
    quota, err := s.storage.GetQuota(s.UserID, root)
    if err != nil {
        return err
    }

    s.sendQuotaResponse(quota)
    return nil
}
```

## Dovecot-Specific Features

### Storage Formats

| Format | Dovecot | go-emailservice-ads | Priority | Notes |
|--------|---------|---------------------|----------|-------|
| **Maildir** | ✅ | ✅ | - | Implemented |
| **mdbox** (multi-dbox) | ✅ | ⚠️ | HIGH | Better performance than Maildir |
| **sdbox** (single-dbox) | ✅ | ❌ | MEDIUM | One file per message |
| **mbox** | ✅ | ❌ | LOW | Legacy format |
| **Maildir++** (quota) | ✅ | ⚠️ | HIGH | Maildir with quota support |
| **dbox** with SIS | ✅ | ❌ | MEDIUM | Single-Instance Storage (dedup) |

### Performance Features

| Feature | Dovecot | go-emailservice-ads | Priority | Description |
|---------|---------|---------------------|----------|-------------|
| **Index files** | ✅ | ❌ | **CRITICAL** | Cache message metadata |
| **FTS (Full-Text Search)** | ✅ | ⚠️ | HIGH | Elasticsearch integration exists |
| **Lazy expunge** | ✅ | ❌ | MEDIUM | Move to trash, delay deletion |
| **Virtual mailboxes** | ✅ | ❌ | LOW | Search folders |
| **Shared mailboxes** | ✅ | ❌ | MEDIUM | Multiple user access |
| **Master user** | ✅ | ❌ | LOW | Admin access to all accounts |
| **Preauth** | ✅ | ❌ | LOW | Pre-authenticated connections |

## Index Files (CRITICAL)

Dovecot's performance advantage comes from aggressive caching:

```
/var/mail/user@msgs.global/
├── cur/
│   ├── 1234567890.M123P456.hostname:2,S
│   └── ...
├── new/
├── tmp/
└── dovecot.index.cache  ← Cache file
    dovecot.index.log
    dovecot.index
```

**What's Cached**:
- Message UIDs
- Flags (Read, Flagged, etc.)
- Envelope data (From, To, Date, Subject)
- MIME structure
- Body structure
- Frequently accessed headers

**Performance Impact**:
- LIST: 100x faster (don't scan all files)
- FETCH: 50x faster for headers (cached)
- SEARCH: 10-100x faster (indexed)

**Implementation**:

```go
// internal/storage/index/cache.go
package index

import (
    "encoding/binary"
    "os"
)

type IndexCache struct {
    path     string
    messages map[uint32]*CachedMessage
    dirty    bool
}

type CachedMessage struct {
    UID           uint32
    Flags         []string
    InternalDate  time.Time
    Size          uint32

    // Cached headers
    From          string
    To            []string
    Subject       string
    MessageID     string
    Date          time.Time

    // MIME structure
    BodyStructure string

    // Search optimization
    HeadersIndex  []byte  // Searchable header index
}

func (cache *IndexCache) Load() error {
    // Load from dovecot.index.cache file
    file, err := os.Open(cache.path)
    if err != nil {
        return err
    }
    defer file.Close()

    // Parse cache format
    // ... implementation ...

    return nil
}

func (cache *IndexCache) GetMessage(uid uint32) (*CachedMessage, bool) {
    msg, exists := cache.messages[uid]
    return msg, exists
}

func (cache *IndexCache) UpdateMessage(uid uint32, msg *CachedMessage) {
    cache.messages[uid] = msg
    cache.dirty = true
}

func (cache *IndexCache) Flush() error {
    if !cache.dirty {
        return nil
    }

    // Write cache to disk
    // ... implementation ...

    cache.dirty = false
    return nil
}
```

## Authentication Features

| Feature | Dovecot | go-emailservice-ads | Priority | Description |
|---------|---------|---------------------|----------|-------------|
| **PLAIN** | ✅ | ✅ | - | Implemented |
| **LOGIN** | ✅ | ✅ | - | Implemented |
| **CRAM-MD5** | ✅ | ❌ | LOW | Challenge-response |
| **DIGEST-MD5** | ✅ | ❌ | LOW | Challenge-response |
| **SCRAM-SHA-1** | ✅ | ❌ | MEDIUM | Modern auth |
| **SCRAM-SHA-256** | ✅ | ❌ | MEDIUM | Modern auth |
| **OAUTH2/XOAUTH2** | ✅ | ⚠️ | HIGH | OAuth2 integration |
| **OAUTHBEARER** | ✅ | ❌ | MEDIUM | OAuth2 bearer token |
| **GSSAPI (Kerberos)** | ✅ | ❌ | LOW | Enterprise auth |
| **Certificate auth** | ✅ | ❌ | LOW | Client certificates |
| **Proxy auth** | ✅ | ❌ | LOW | Proxied authentication |

## Plugin Architecture

| Feature | Dovecot | go-emailservice-ads | Priority | Description |
|---------|---------|---------------------|----------|-------------|
| Plugin system | ✅ | ❌ | HIGH | Extend functionality |
| Virtual plugin | ✅ | ❌ | LOW | Virtual mailboxes |
| Quota plugin | ✅ | ❌ | HIGH | Quota enforcement |
| ACL plugin | ✅ | ❌ | MEDIUM | Access control |
| FTS plugin | ✅ | ⚠️ | HIGH | Full-text search |
| Antispam plugin | ✅ | ❌ | LOW | Train spam filters |
| Mail-log plugin | ✅ | ⚠️ | MEDIUM | Detailed logging |
| Lazy-expunge plugin | ✅ | ❌ | MEDIUM | Delayed deletion |
| Notify plugin | ✅ | ❌ | LOW | Event notifications |

## Implementation Priority

### Phase 1: Critical Features (Q2 2026)

1. **IDLE (RFC 2177)**
   - Real-time push notifications
   - Essential for modern clients
   - Estimated: 2 weeks

2. **CONDSTORE (RFC 4551)**
   - Efficient synchronization
   - Prerequisite for QRESYNC
   - Estimated: 1 week

3. **QRESYNC (RFC 5162)**
   - Fast reconnection
   - Depends on CONDSTORE
   - Estimated: 1 week

4. **Index Files**
   - Performance critical
   - Cache message metadata
   - Estimated: 3 weeks

### Phase 2: High-Priority Features (Q3 2026)

1. **QUOTA (RFC 2087)**
   - Mailbox usage limits
   - User-facing feature
   - Estimated: 1 week

2. **SORT (RFC 5256)**
   - Server-side sorting
   - Reduces client workload
   - Estimated: 1 week

3. **MOVE (RFC 6851)**
   - Atomic move operation
   - More efficient than COPY+EXPUNGE
   - Estimated: 3 days

4. **SPECIAL-USE (RFC 6154)**
   - Complete implementation
   - Special mailbox attributes
   - Estimated: 3 days

5. **mdbox Storage Format**
   - Better performance than Maildir
   - Less I/O overhead
   - Estimated: 2 weeks

### Phase 3: Medium-Priority Features (Q4 2026)

1. **ACL (RFC 4314)**
   - Shared mailbox access
   - Enterprise feature
   - Estimated: 2 weeks

2. **THREAD (RFC 5256)**
   - Conversation threading
   - Better UX
   - Estimated: 1 week

3. **COMPRESS (RFC 4978)**
   - Bandwidth savings
   - Mobile clients
   - Estimated: 3 days

4. **METADATA (RFC 5464)**
   - Store per-mailbox data
   - Client preferences
   - Estimated: 1 week

5. **Plugin System**
   - Extensibility
   - Third-party integrations
   - Estimated: 2 weeks

### Phase 4: Low-Priority Features (2027)

1. **THREAD, ESEARCH, SEARCHRES**
2. **Virtual mailboxes**
3. **Master user access**
4. **Additional auth methods**
5. **Binary content support**

## Configuration Enhancements

### Dovecot-Inspired Config

```yaml
imap:
  # Protocol features
  capabilities:
    - IMAP4rev1
    - IDLE
    - CONDSTORE
    - QRESYNC
    - QUOTA
    - SORT
    - THREAD=REFERENCES
    - MOVE
    - SPECIAL-USE
    - COMPRESS=DEFLATE
    - UTF8=ACCEPT

  # Performance
  performance:
    cache:
      enabled: true
      cache_dir: "/var/mail-cache"
      cache_size: "1G"
      cache_ttl: "7d"

    indexes:
      enabled: true
      index_dir: "/var/mail-indexes"
      memory_limit: "512M"

    compression:
      enabled: true
      algorithm: "zstd"
      level: 3

  # Quotas
  quota:
    enabled: true
    rules:
      - name: "default"
        storage: "10G"
        messages: 100000
      - name: "premium"
        storage: "50G"
        messages: 500000

  # Mailbox management
  mailboxes:
    auto_create:
      - name: "INBOX"
        special_use: null
      - name: "Sent"
        special_use: "\\Sent"
      - name: "Drafts"
        special_use: "\\Drafts"
      - name: "Trash"
        special_use: "\\Trash"
        auto_expunge: "30d"
      - name: "Junk"
        special_use: "\\Junk"
        auto_expunge: "7d"
      - name: "Quarantine"
        special_use: "\\Quarantine"
        auto_expunge: "30d"

  # IDLE settings
  idle:
    enabled: true
    max_idle_time: "29m"
    heartbeat: "2m"
    max_concurrent: 1000

  # Storage
  storage:
    format: "mdbox"  # or "maildir", "sdbox"
    mdbox:
      rotate_size: "10M"
      rotate_interval: "1d"
    single_instance_storage: true  # Deduplication
```

## Migration Path

### For Existing Dovecot Users

```bash
# 1. Export Dovecot mailboxes
doveadm sync -u user@msgs.global maildir:/backup/user

# 2. Import to go-emailservice-ads
./bin/import-dovecot \
  --source /var/mail/vhosts/msgs.global/user \
  --dest /var/mail/user@msgs.global \
  --format maildir \
  --preserve-flags \
  --preserve-uids

# 3. Migrate index files (if compatible)
./bin/convert-dovecot-index \
  --source /var/mail/vhosts/msgs.global/user/dovecot.index.cache \
  --dest /var/mail-indexes/user@msgs.global/
```

## Testing & Compatibility

### IMAP Test Suite

```bash
# Install imaptest (Dovecot's test tool)
git clone https://github.com/dovecot/imaptest
cd imaptest
./configure && make && sudo make install

# Run compatibility tests
imaptest \
  host=apps.afterdarksys.com \
  port=993 \
  ssl=yes \
  user=testuser@msgs.global \
  pass=testpass \
  test=all \
  clients=10 \
  duration=300

# Expected output:
# - IMAP4rev1: PASS
# - IDLE: PASS
# - CONDSTORE: PASS
# - QRESYNC: PASS
# - QUOTA: PASS
# ...
```

## Performance Benchmarks

### Dovecot vs go-emailservice-ads

| Operation | Dovecot | go-emailservice-ads (current) | go-emailservice-ads (with indexes) | Target |
|-----------|---------|-------------------------------|-------------------------------------|--------|
| LIST 1000 folders | 50ms | 2000ms | 100ms | <100ms |
| FETCH 100 headers | 100ms | 1500ms | 150ms | <200ms |
| SEARCH 10000 messages | 200ms | 5000ms | 300ms | <500ms |
| IDLE (1000 concurrent) | 50MB RAM | N/A | 100MB RAM | <150MB RAM |
| SELECT with QRESYNC | 30ms | N/A | 50ms | <100ms |

## See Also

- [IMAP Implementation](./IMAP.md)
- [Groupware Architecture](./GROUPWARE_ARCHITECTURE.md)
- [Storage Formats](./STORAGE.md)
- [Performance Tuning](./PERFORMANCE.md)

## References

- [Dovecot Documentation](https://doc.dovecot.org/)
- [Dovecot IMAP Extensions](https://doc.dovecot.org/configuration_manual/imap_server/)
- [IMAP RFCs](https://imapwiki.org/Specs)
- [imaptest Tool](https://github.com/dovecot/imaptest)
