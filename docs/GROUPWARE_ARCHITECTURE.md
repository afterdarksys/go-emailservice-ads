# Groupware Architecture - Encrypted IMAP + CalDAV System

## Overview

Full-featured encrypted groupware platform with:
- **IMAP** - Email storage and access
- **CalDAV** - Calendar/scheduling
- **Encryption** - At-rest and in-transit
- **Live Updates** - Real-time synchronization
- **Attachment Quarantine** - Integrated security service

## Architecture Diagram

```
┌───────────────────────────────────────────────────────────────┐
│                         Client Layer                          │
├───────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Thunderbird  │  │  Apple Mail  │  │   Webmail    │      │
│  │    (IMAP)    │  │    (IMAP)    │  │   Browser    │      │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘      │
│         │                  │                  │               │
│  ┌──────┴───────┐  ┌──────┴───────┐  ┌──────┴───────┐      │
│  │  Calendar    │  │   Calendar   │  │   Web Cal    │      │
│  │   (CalDAV)   │  │   (CalDAV)   │  │   Interface  │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│                                                               │
└───────────────────────────────────────────────────────────────┘
                            │
                            │ TLS 1.3
                            │
┌───────────────────────────────────────────────────────────────┐
│                      Application Layer                        │
├───────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              IMAP Server (Port 993/143)              │   │
│  │  • IDLE push notifications                           │   │
│  │  • CONDSTORE (conditional store)                     │   │
│  │  • QRESYNC (quick resync)                            │   │
│  │  • SPECIAL-USE mailboxes                             │   │
│  │  • ACL (access control lists)                        │   │
│  └────────────────────────┬─────────────────────────────┘   │
│                            │                                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │             CalDAV Server (Port 443)                 │   │
│  │  • Calendar storage                                  │   │
│  │  • Event scheduling                                  │   │
│  │  • Timezone handling                                 │   │
│  │  • Recurrence rules                                  │   │
│  │  • Invitations/RSVP                                  │   │
│  └────────────────────────┬─────────────────────────────┘   │
│                            │                                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │          Quarantine Web Service (Port 8443)          │   │
│  │  • OAuth2 authentication                             │   │
│  │  • Attachment viewer                                 │   │
│  │  • Admin console                                     │   │
│  │  • Digest management                                 │   │
│  └────────────────────────┬─────────────────────────────┘   │
│                            │                                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │           SMTP Server (Port 25/465/587)              │   │
│  │  • Inbound mail processing                           │   │
│  │  • Attachment scanning                               │   │
│  │  • Quarantine integration                            │   │
│  │  • Delivery to IMAP mailboxes                        │   │
│  └────────────────────────┬─────────────────────────────┘   │
│                            │                                  │
└────────────────────────────┼──────────────────────────────────┘
                            │
┌───────────────────────────────────────────────────────────────┐
│                        Storage Layer                          │
├───────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │         Encrypted Mail Storage (Maildir/mdbox)       │   │
│  │  • Per-user encryption keys                          │   │
│  │  • AES-256-GCM encryption                            │   │
│  │  • Searchable encryption (optional)                  │   │
│  │  • Compression (zstd)                                │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Calendar Storage (PostgreSQL)           │   │
│  │  • vCalendar/iCal format                             │   │
│  │  • Event versioning                                  │   │
│  │  • Encrypted calendar data                           │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │         Quarantine Storage (S3/MinIO)                │   │
│  │  • Isolated attachment storage                       │   │
│  │  • Encrypted at rest                                 │   │
│  │  • Automatic expiration                              │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │           Full-Text Search (Elasticsearch)           │   │
│  │  • Mail content indexing                             │   │
│  │  • Calendar event search                             │   │
│  │  • Encrypted index                                   │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

## Live Update System

### IMAP IDLE for Real-Time Mail Notifications

```
┌──────────────┐                                ┌──────────────┐
│    Client    │                                │ IMAP Server  │
└──────┬───────┘                                └──────┬───────┘
       │                                               │
       │  C: a001 SELECT INBOX                        │
       │ ──────────────────────────────────────────>  │
       │                                               │
       │  S: * FLAGS (\Answered \Flagged \Draft...)   │
       │  <──────────────────────────────────────────  │
       │  S: * 23 EXISTS                               │
       │  <──────────────────────────────────────────  │
       │  S: a001 OK [READ-WRITE] SELECT completed     │
       │  <──────────────────────────────────────────  │
       │                                               │
       │  C: a002 IDLE                                 │
       │ ──────────────────────────────────────────>  │
       │                                               │
       │  S: + idling                                  │
       │  <──────────────────────────────────────────  │
       │                                               │
       │         [Client waits for updates...]         │
       │                                               │
       │            [New mail arrives]                 │
       │                                               │
       │  S: * 24 EXISTS                               │
       │  <══════════════════════════════════════════  │  Push!
       │  S: * 24 FETCH (FLAGS (\Recent))              │
       │  <══════════════════════════════════════════  │  Push!
       │                                               │
       │  C: DONE                                      │
       │ ──────────────────────────────────────────>  │
       │                                               │
       │  S: a002 OK IDLE terminated                   │
       │  <──────────────────────────────────────────  │
       │                                               │
```

### CalDAV Calendar Sync

```
┌──────────────┐                                ┌──────────────┐
│    Client    │                                │CalDAV Server │
└──────┬───────┘                                └──────┬───────┘
       │                                               │
       │  Initial sync request                         │
       │  PROPFIND /calendars/user@msgs.global/        │
       │ ──────────────────────────────────────────>  │
       │                                               │
       │  S: Calendar list with sync-tokens            │
       │  <──────────────────────────────────────────  │
       │                                               │
       │  [Poll for changes every 30s]                 │
       │  REPORT calendar-query                        │
       │  <sync-token>FT=-@RU=5f2f...                  │
       │ ──────────────────────────────────────────>  │
       │                                               │
       │  S: Changed/new events only                   │
       │  <──────────────────────────────────────────  │
       │  S: New sync-token: FT=-@RU=6a3d...           │
       │  <──────────────────────────────────────────  │
       │                                               │
```

### WebSocket for Web Interface

```
┌──────────────┐                                ┌──────────────┐
│  Browser     │                                │  WebSocket   │
│  (Webmail)   │                                │   Server     │
└──────┬───────┘                                └──────┬───────┘
       │                                               │
       │  WSS handshake                                │
       │ ══════════════════════════════════════════>  │
       │                                               │
       │  S: Connected                                 │
       │  <══════════════════════════════════════════  │
       │                                               │
       │  [Maintain persistent connection]             │
       │                                               │
       │  [New mail arrives on backend]                │
       │                                               │
       │  S: {"type":"new_mail","count":1,...}         │
       │  <══════════════════════════════════════════  │  Push!
       │                                               │
       │  [Calendar event updated]                     │
       │                                               │
       │  S: {"type":"calendar_update","event_id":...} │
       │  <══════════════════════════════════════════  │  Push!
       │                                               │
```

## Encryption Architecture

### Mail Encryption (Per-User Keys)

```
┌─────────────────────────────────────────────────────────┐
│                 Inbound Mail Processing                 │
└────────────────────────┬────────────────────────────────┘
                         │
                         v
              ┌──────────────────────┐
              │  Get User Master Key │
              │  (from key store)    │
              └──────────┬───────────┘
                         │
                         v
              ┌──────────────────────┐
              │  Generate Message    │
              │  Encryption Key      │
              │  (AES-256-GCM)       │
              └──────────┬───────────┘
                         │
                         v
              ┌──────────────────────┐
              │  Encrypt Message     │
              │  • Headers (selected)│
              │  • Body              │
              │  • Attachments       │
              └──────────┬───────────┘
                         │
                         v
              ┌──────────────────────┐
              │  Encrypt Message Key │
              │  with User Master Key│
              └──────────┬───────────┘
                         │
                         v
              ┌──────────────────────┐
              │  Store to Maildir    │
              │  • Encrypted payload │
              │  • Encrypted key     │
              │  • Metadata          │
              └──────────────────────┘
```

### Message Format on Disk

```
/var/mail/user@msgs.global/cur/1234567890.M123P456.hostname:2,S

┌─────────────────────────────────────────────────────────┐
│                   Encrypted Message                     │
├─────────────────────────────────────────────────────────┤
│ X-Encrypted-Version: 1                                  │
│ X-Encryption-Method: AES-256-GCM                        │
│ X-Encrypted-Key: [base64 encrypted message key]        │
│ X-Key-ID: user-key-abc123                               │
│ X-Nonce: [base64 nonce]                                 │
│                                                          │
│ ──────── Searchable Headers (Optional) ────────         │
│ From: sender@example.com                                │
│ To: user@msgs.global                                    │
│ Date: Mon, 09 Mar 2026 10:23:45 -0700                   │
│ Message-ID: <abc123@example.com>                        │
│                                                          │
│ ──────── Encrypted Payload ────────                     │
│ [Base64 encrypted data containing:]                     │
│ • Full headers (including Subject, etc.)                │
│ • Message body                                          │
│ • Inline attachments                                    │
│ • MIME structure                                        │
└─────────────────────────────────────────────────────────┘
```

### Calendar Encryption

```sql
CREATE TABLE calendars (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    name VARCHAR(255),
    color VARCHAR(7),
    encrypted_data BYTEA,  -- Encrypted calendar properties
    encryption_key_id VARCHAR(255),
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE calendar_events (
    id UUID PRIMARY KEY,
    calendar_id UUID REFERENCES calendars(id),
    uid VARCHAR(255) UNIQUE,  -- iCalendar UID

    -- Searchable metadata (encrypted with searchable encryption)
    start_time TIMESTAMP,
    end_time TIMESTAMP,
    all_day BOOLEAN,

    -- Encrypted event data
    encrypted_summary BYTEA,  -- Event title
    encrypted_description BYTEA,
    encrypted_location BYTEA,
    encrypted_ical BYTEA,  -- Full iCalendar data

    encryption_key_id VARCHAR(255),
    etag VARCHAR(64),  -- For sync
    sequence INT DEFAULT 0,

    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),

    INDEX idx_calendar_id (calendar_id),
    INDEX idx_time_range (start_time, end_time)
);
```

## Quarantine Integration with IMAP

### Quarantine Mailbox (IMAP SPECIAL-USE)

Each user automatically gets a `Quarantine` IMAP folder:

```
INBOX
├── Sent
├── Drafts
├── Trash
├── Junk
└── Quarantine  ← Special-use \Quarantine
    ├── 2026-03-09 - document.pdf quarantined
    ├── 2026-03-08 - invoice.xlsx quarantined
    └── 2026-03-07 - 5 photos quarantined
```

### Quarantine Message Format

```
From: Mail Security Service <quarantine@msgs.global>
To: user@msgs.global
Subject: [QUARANTINED] Important Document from sender@example.com
Date: Mon, 09 Mar 2026 10:23:45 -0700
X-Quarantine-ID: abc123xyz
X-Original-Sender: sender@example.com
X-Original-Subject: Important Document
X-Quarantine-Reason: Suspicious attachment

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🛡️ MAIL QUARANTINED

An email from sender@example.com was quarantined
due to potentially harmful attachments.

Original Email Details:
From: sender@example.com
Subject: Important Document
Date: March 9, 2026 10:23 AM
Attachments: 1 file (245KB)

Quarantine Reason:
• Suspicious file type detected
• File: document.pdf
• Scanner: ClamAV

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ACTIONS:

1. View Attachment Details:
   https://quarantine.msgs.global/view/abc123xyz

2. Download After Review:
   https://quarantine.msgs.global/download/abc123xyz

3. Release Full Email:
   Reply to this email with "RELEASE" to restore
   the original email with attachments.

4. Delete Permanently:
   Reply to this email with "DELETE" to remove
   the quarantined items.

This quarantine expires in 30 days.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### Email-Based Quarantine Management

Users can manage quarantine via email:

```
┌────────────────────────────────────────────────────┐
│  User Action: Reply to quarantine notice          │
│  Body: "RELEASE"                                   │
└────────────────┬───────────────────────────────────┘
                 │
                 v
      ┌──────────────────────┐
      │  Parse Command        │
      │  - RELEASE            │
      │  - DELETE             │
      │  - INFO               │
      └──────────┬────────────┘
                 │
                 v
      ┌──────────────────────┐
      │  Verify Sender        │
      │  (must be recipient) │
      └──────────┬────────────┘
                 │
                 v
      ┌──────────────────────┐
      │  Execute Action       │
      └──────────┬────────────┘
                 │
                 v
      ┌──────────────────────┐
      │  Send Confirmation    │
      │  + Deliver Email      │
      └────────────────────────┘
```

## Full Configuration

```yaml
server:
  addr: ":2525"
  domain: "apps.afterdarksys.com"
  max_message_bytes: 52428800  # 50MB
  max_recipients: 50
  require_auth: true
  require_tls: true
  mode: "production"

  local_domains:
    - "apps.afterdarksys.com"
    - "afterdarksys.com"
    - "msgs.global"

  tls:
    cert: "/etc/letsencrypt/live/apps.afterdarksys.com/fullchain.pem"
    key: "/etc/letsencrypt/live/apps.afterdarksys.com/privkey.pem"

# IMAP Server Configuration
imap:
  addr: ":993"  # IMAPS (implicit TLS)
  addr_starttls: ":143"  # IMAP with STARTTLS
  require_tls: true

  tls:
    cert: "/etc/letsencrypt/live/apps.afterdarksys.com/fullchain.pem"
    key: "/etc/letsencrypt/live/apps.afterdarksys.com/privkey.pem"

  # Storage backend
  storage:
    type: "maildir"  # or "mdbox", "sdbox"
    path: "/var/mail"

  # Encryption
  encryption:
    enabled: true
    method: "per-user"  # Per-user master keys
    algorithm: "AES-256-GCM"
    key_derivation: "scrypt"

    # Key storage
    key_store:
      type: "vault"  # or "local", "kms"
      vault_addr: "https://vault.afterdarksys.com:8200"
      vault_path: "secret/mail/keys"

    # Searchable encryption (optional)
    searchable:
      enabled: true
      fields: ["from", "to", "date", "message-id"]

  # Features
  features:
    idle: true  # Push notifications
    condstore: true  # Conditional store
    qresync: true  # Quick resync
    special_use: true  # Special-use mailboxes
    acl: true  # Access control lists
    quota: true  # Mailbox quotas

  # Special-use mailboxes
  special_use_mailboxes:
    - name: "INBOX"
      special_use: null
    - name: "Sent"
      special_use: "\\Sent"
    - name: "Drafts"
      special_use: "\\Drafts"
    - name: "Trash"
      special_use: "\\Trash"
    - name: "Junk"
      special_use: "\\Junk"
    - name: "Quarantine"
      special_use: "\\Quarantine"

# CalDAV Server Configuration
caldav:
  enabled: true
  addr: ":443"
  base_url: "https://caldav.msgs.global"

  tls:
    cert: "/etc/letsencrypt/live/caldav.msgs.global/fullchain.pem"
    key: "/etc/letsencrypt/live/caldav.msgs.global/privkey.pem"

  # Storage
  storage:
    type: "postgresql"
    dsn: "postgres://caluser:password@localhost/calendars?sslmode=require"

  # Encryption
  encryption:
    enabled: true
    fields: ["summary", "description", "location", "icalendar"]

  # Features
  features:
    scheduling: true  # Calendar invitations
    freebusy: true  # Free/busy information
    notifications: true  # Email notifications

# Attachment Quarantine
attachment_quarantine:
  enabled: true

  storage:
    type: "s3"
    bucket: "mail-quarantine-prod"
    endpoint: "s3.amazonaws.com"
    region: "us-east-1"
    encryption: true  # S3 server-side encryption
    retention_days: 30

  scanner:
    engine: "clamav"
    clamd_socket: "/var/run/clamav/clamd.sock"
    max_scan_size: 52428800  # 50MB

    blocked_extensions:
      - ".exe"
      - ".bat"
      - ".cmd"
      - ".scr"
      - ".vbs"
      - ".js"
      - ".jar"

    high_risk_extensions:
      - ".zip"
      - ".rar"
      - ".7z"
      - ".docm"
      - ".xlsm"
      - ".pptm"

  web:
    addr: ":8443"
    base_url: "https://quarantine.msgs.global"
    tls:
      cert: "/etc/letsencrypt/live/quarantine.msgs.global/fullchain.pem"
      key: "/etc/letsencrypt/live/quarantine.msgs.global/privkey.pem"

  oauth2:
    provider: "afterdarksystems"
    client_id: "${ADS_CLIENT_ID}"
    client_secret: "${ADS_CLIENT_SECRET}"
    auth_url: "https://sso.afterdarksystems.com/oauth2/authorize"
    token_url: "https://sso.afterdarksystems.com/oauth2/token"
    userinfo_url: "https://sso.afterdarksystems.com/oauth2/userinfo"
    redirect_url: "https://quarantine.msgs.global/oauth/callback"

  # IMAP integration
  imap_integration:
    enabled: true
    create_quarantine_folder: true
    send_notifications: true
    email_commands: true  # Allow RELEASE/DELETE via email

  digests:
    enabled: true
    default_schedule: "weekly"
    send_time: "09:00"

  release:
    require_admin_approval: false
    auto_release_safe_after_days: 7
    notify_on_release: true

# Full-text search
elasticsearch:
  enabled: true
  endpoints:
    - "https://es1.msgs.global:9200"
    - "https://es2.msgs.global:9200"

  username: "mail_indexer"
  password: "${ES_PASSWORD}"

  # Encrypted search
  encryption:
    enabled: true
    method: "deterministic"  # For exact match

  index_prefix: "mail"
  retention_days: 365

# WebSocket for web interface
websocket:
  enabled: true
  addr: ":8444"
  tls:
    cert: "/etc/letsencrypt/live/webmail.msgs.global/fullchain.pem"
    key: "/etc/letsencrypt/live/webmail.msgs.global/privkey.pem"

  # Real-time notifications
  notifications:
    new_mail: true
    calendar_updates: true
    quarantine_alerts: true

# Authentication
auth:
  # SSO (primary)
  sso:
    enabled: true
    provider: "afterdarksystems"
    directory_url: "https://directory.msgs.global"

  # LDAP (fallback)
  ldap:
    enabled: true
    server: "ldap.internal.msgs.global"
    port: 636
    use_tls: true
    base_dn: "dc=msgs,dc=global"
    bind_dn: "cn=mailservice,dc=msgs,dc=global"
    bind_password: "${LDAP_PASSWORD}"
    user_filter: "(mail=%s)"

# API
api:
  rest_addr: ":8080"
  grpc_addr: ":50051"

  api_keys:
    - name: "Web Platform"
      key: "${API_KEY}"
      description: "API key for web platform integration"
      permissions: ["read", "write"]

  allowed_ips:
    - "127.0.0.1"
    - "::1"
    - "66.228.40.180"  # mx.nerdycupid.com
    - "108.165.123.229"  # apps.afterdarksys.com
  require_ip_auth: true

logging:
  level: "info"
  format: "json"
  output: "/var/log/mail/service.log"
```

## Client Configuration Examples

### Thunderbird

```
Account Settings:
  Server Name: apps.afterdarksys.com
  Port: 993
  Connection Security: SSL/TLS
  Authentication: OAuth2 or Normal Password

Outgoing Server (SMTP):
  Server Name: apps.afterdarksys.com
  Port: 465
  Connection Security: SSL/TLS
  Authentication: OAuth2 or Normal Password

Calendar:
  Location: https://caldav.msgs.global/calendars/user@msgs.global/
  Format: CalDAV
  Authentication: OAuth2
```

### Apple Mail/Calendar (macOS/iOS)

```
Add Account → Other Account
  Account Type: IMAP

Mail Server:
  Hostname: apps.afterdarksys.com
  Port: 993
  Use SSL: Yes
  Username: user@msgs.global
  Password: [OAuth2 or password]

SMTP Server:
  Hostname: apps.afterdarksys.com
  Port: 465
  Use SSL: Yes

Calendar:
  Server: https://caldav.msgs.global
  Username: user@msgs.global
  Password: [OAuth2 or password]
```

## Performance Considerations

1. **Encryption Overhead**
   - ~5-10% CPU overhead for encryption/decryption
   - Use hardware AES acceleration (AES-NI)
   - Cache decrypted message keys in memory

2. **IMAP IDLE Scalability**
   - Use connection pooling
   - Implement idle timeout (29 minutes)
   - Support multiple IDLE connections per user

3. **CalDAV Sync**
   - Use sync-token for incremental updates
   - Cache calendar data
   - Batch event updates

4. **Search Performance**
   - Elasticsearch for full-text search
   - Indexed searchable encryption for metadata
   - Limit search scope (date ranges, folders)

5. **Storage Optimization**
   - Compression (zstd) before encryption
   - Deduplication for attachments
   - Tiered storage (hot/warm/cold)

## Security Model

1. **Zero-Knowledge Architecture**
   - Server cannot decrypt user data
   - Master keys derived from user password
   - Key escrow optional (for password recovery)

2. **End-to-End Encryption Option**
   - PGP/S/MIME support
   - Automatic key exchange
   - Web of trust or keyserver integration

3. **Audit Logging**
   - All access logged (encrypted)
   - Admin actions tracked
   - Compliance reporting (GDPR, HIPAA)

## See Also

- [Attachment Quarantine](./ATTACHMENT_QUARANTINE.md)
- [Security Guide](../SECURITY.md)
- [Encryption Details](./ENCRYPTION.md)
- [IMAP Implementation](./IMAP.md)
- [CalDAV Implementation](./CALDAV.md)
