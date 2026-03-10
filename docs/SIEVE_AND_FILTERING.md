# Sieve Mail Filtering & AfterScript Policies

## Overview

Comprehensive mail filtering system supporting:
- **Sieve Scripts** - Standard RFC 5228 mail filtering language
- **GUI Builder** - Visual drag-and-drop filter editor
- **AfterScript** - Enhanced policy language with advanced features
- **Multi-Stage Processing** - Apply rules at any point in mail flow

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Mail Flow Stages                         │
└─────────────────────────────────────────────────────────────┘

  SMTP Connection  →  Pre-DATA  →  Data  →  Post-DATA  →  Delivery
       │                │          │          │              │
       │                │          │          │              │
       ▼                ▼          ▼          ▼              ▼
  ┌──────────┐   ┌──────────┐  ┌────┐  ┌──────────┐  ┌───────────┐
  │ Connect  │   │  RCPT TO │  │Body│  │  Queue   │  │   LMTP    │
  │ Policies │   │  Filters │  │Scan│  │ Filters  │  │  Sieve    │
  └──────────┘   └──────────┘  └────┘  └──────────┘  └───────────┘
       │                │          │          │              │
       │                └──────────┴──────────┴──────────────┘
       │                              │
       ▼                              ▼
  AfterScript                    Sieve + AfterScript
  Policies                       User Filters
```

## Sieve Support (RFC 5228)

### Direct Sieve Script Management

Users can upload/edit Sieve scripts directly:

```sieve
# ~/.sieve/main.sieve
require ["fileinto", "reject", "vacation", "envelope", "body", "variables"];

# 1. Reject spam
if header :contains "X-Spam-Flag" "YES" {
    fileinto "Junk";
    stop;
}

# 2. Vacation auto-reply
if allof (
    currentdate :value "ge" "date" "2026-03-15",
    currentdate :value "le" "date" "2026-03-22"
) {
    vacation
        :days 7
        :subject "Out of Office"
        :from "user@msgs.global"
        "I am currently out of office and will return on March 23.
        For urgent matters, please contact admin@msgs.global.";
}

# 3. Mailing list filters
if header :contains "List-Id" "<dev-team.company.com>" {
    fileinto "Lists/Dev-Team";
    stop;
}

# 4. Important senders
if address :is "from" ["boss@company.com", "ceo@company.com"] {
    addflag "\\Flagged";
    fileinto "INBOX/Important";
    stop;
}

# 5. Large attachments
if size :over 5M {
    fileinto "Large-Attachments";
    stop;
}

# 6. Body content filtering
if body :text :contains ["invoice", "payment due"] {
    fileinto "Finance";
    stop;
}

# Default: keep in INBOX
keep;
```

### Sieve Extensions Supported

```yaml
sieve:
  enabled: true
  extensions:
    - fileinto          # RFC 5228 - File into folders
    - reject            # RFC 5429 - Reject messages
    - envelope          # RFC 5228 - Envelope testing
    - body              # RFC 5173 - Body tests
    - imap4flags        # RFC 5232 - IMAP flags
    - variables         # RFC 5229 - Variables
    - vacation          # RFC 5230 - Vacation auto-reply
    - subaddress        # RFC 5233 - Subaddress extension
    - relational        # RFC 5231 - Relational tests
    - comparator        # RFC 5228 - Comparator support
    - copy              # RFC 3894 - Copy extension
    - include           # RFC 6609 - Include extension
    - editheader        # RFC 5293 - Edit headers
    - date              # RFC 5260 - Date/time tests
    - regex             # Regex matching
    - extlists          # RFC 6134 - External lists
    - enotify           # RFC 5435 - Email notifications
    - imapsieve         # RFC 6785 - IMAP Sieve integration
    - vnd.aftersmtp     # Custom AfterSMTP extensions

  # ManageSieve protocol (RFC 5804)
  managesieve:
    enabled: true
    addr: ":4190"
    tls:
      cert: "/etc/letsencrypt/live/apps.afterdarksys.com/fullchain.pem"
      key: "/etc/letsencrypt/live/apps.afterdarksys.com/privkey.pem"
    require_tls: true

  # Storage
  script_dir: "/var/mail/{domain}/{user}/.sieve"
  active_script: "main.sieve"

  # Limits
  max_script_size: 102400  # 100KB
  max_actions: 100
  max_redirects: 5
```

## GUI Filter Builder

### Visual Filter Editor

```
┌──────────────────────────────────────────────────────────────┐
│ Mail Filters - user@msgs.global                         [+] │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│ ┌──────────────────────────────────────────────────────┐   │
│ │ Filter 1: Spam Detection                      [↑][↓][×] │
│ ├──────────────────────────────────────────────────────┤   │
│ │ IF:                                                    │   │
│ │   ┌────────────┬──────────┬───────────────────────┐  │   │
│ │   │ Header     │ contains │ "X-Spam-Flag: YES"    │  │   │
│ │   └────────────┴──────────┴───────────────────────┘  │   │
│ │                                          [+ Add Rule] │   │
│ │                                                        │   │
│ │ THEN:                                                  │   │
│ │   ┌──────────────────────────────────────────────┐   │   │
│ │   │ Move to folder: Junk                         │   │   │
│ │   └──────────────────────────────────────────────┘   │   │
│ │   ┌──────────────────────────────────────────────┐   │   │
│ │   │ Stop processing more rules                   │   │   │
│ │   └──────────────────────────────────────────────┘   │   │
│ │                                        [+ Add Action] │   │
│ └──────────────────────────────────────────────────────┘   │
│                                                              │
│ ┌──────────────────────────────────────────────────────┐   │
│ │ Filter 2: Vacation Auto-Reply                [↑][↓][×] │
│ ├──────────────────────────────────────────────────────┤   │
│ │ IF:                                                    │   │
│ │   ┌────────────┬──────────┬───────────────────────┐  │   │
│ │   │ Date       │ between  │ 2026-03-15 to         │  │   │
│ │   │            │          │ 2026-03-22            │  │   │
│ │   └────────────┴──────────┴───────────────────────┘  │   │
│ │                                                        │   │
│ │ THEN:                                                  │   │
│ │   ┌──────────────────────────────────────────────┐   │   │
│ │   │ Send auto-reply:                             │   │   │
│ │   │ Subject: "Out of Office"                     │   │   │
│ │   │ Message: [                                   │   │   │
│ │   │   I am currently out...                      │   │   │
│ │   │ ]                                            │   │   │
│ │   │ Send once per: 7 days                        │   │   │
│ │   └──────────────────────────────────────────────┘   │   │
│ └──────────────────────────────────────────────────────┘   │
│                                                              │
│ ┌──────────────────────────────────────────────────────┐   │
│ │ Filter 3: Important Senders                  [↑][↓][×] │
│ ├──────────────────────────────────────────────────────┤   │
│ │ IF:                                                    │   │
│ │   ┌────────────┬──────────┬───────────────────────┐  │   │
│ │   │ From       │ is one of│ boss@company.com      │  │   │
│ │   │            │          │ ceo@company.com       │  │   │
│ │   └────────────┴──────────┴───────────────────────┘  │   │
│ │                                                        │   │
│ │ THEN:                                                  │   │
│ │   ┌──────────────────────────────────────────────┐   │   │
│ │   │ Mark as: ⭐ Flagged                          │   │   │
│ │   └──────────────────────────────────────────────┘   │   │
│ │   ┌──────────────────────────────────────────────┐   │   │
│ │   │ Move to folder: INBOX/Important              │   │   │
│ │   └──────────────────────────────────────────────┘   │   │
│ └──────────────────────────────────────────────────────┘   │
│                                                              │
│                     [+ Add Filter]   [Save] [Export Sieve]  │
└──────────────────────────────────────────────────────────────┘
```

### GUI Components

Available filter conditions:
- **From/To/CC/BCC** - Email addresses
- **Subject** - Subject line matching
- **Header** - Any header field
- **Body** - Message body content
- **Size** - Message size
- **Date** - Date/time conditions
- **Has attachment** - Attachment presence
- **Attachment type** - File extension
- **Spam score** - SpamAssassin score
- **List-ID** - Mailing list identifier
- **Priority** - Message priority

Available actions:
- **Move to folder** - File into mailbox
- **Copy to folder** - Copy (keep original)
- **Delete** - Discard message
- **Mark as** - Add IMAP flags (Read, Flagged, etc.)
- **Add label** - Custom labels
- **Forward to** - Forward to address
- **Send auto-reply** - Vacation message
- **Add header** - Add custom header
- **Remove header** - Delete header
- **Run script** - Execute custom AfterScript
- **Stop processing** - Don't run more filters

### GUI to Sieve Compilation

```javascript
// GUI Filter Object
{
  "name": "Spam Detection",
  "enabled": true,
  "conditions": [
    {
      "type": "header",
      "header": "X-Spam-Flag",
      "operator": "contains",
      "value": "YES"
    }
  ],
  "actions": [
    {
      "type": "fileinto",
      "folder": "Junk"
    },
    {
      "type": "stop"
    }
  ]
}

// Generated Sieve Script
require ["fileinto"];

if header :contains "X-Spam-Flag" "YES" {
    fileinto "Junk";
    stop;
}
```

## AfterScript Policy Language

Enhanced filtering language with advanced features:

### AfterScript Example

```afterscript
# /etc/mail/afterscript/policies/main.as
# AfterScript Policy - Enhanced Sieve-like language

policy spam_defense {
    stage: pre_data
    priority: 100

    # Check sender reputation
    if sender.reputation < 0.5 {
        tempfail "Sender reputation too low"
    }

    # Rate limiting
    if count(sender.email, window=1h) > 50 {
        reject "Too many messages from sender"
    }

    # Geographic restrictions
    if sender.geoip.country in ["CN", "RU", "KP"] {
        quarantine "Suspicious origin"
    }
}

policy attachment_security {
    stage: post_data
    priority: 90

    # Scan with multiple engines
    for attachment in message.attachments {
        results = scan_attachment(attachment, [
            "clamav",
            "yara",
            "sandbox"
        ])

        if results.clamav.infected {
            reject "Virus detected: {results.clamav.signature}"
        }

        if results.yara.matched {
            quarantine "Malware pattern detected"
        }

        if attachment.extension in [".exe", ".bat", ".cmd"] {
            # Check if sender is authorized
            if sender.domain not in config.trusted_domains {
                remove_attachment(attachment)
                message.add_header("X-Attachment-Removed", attachment.filename)
            }
        }
    }
}

policy user_filters {
    stage: delivery
    priority: 50

    # Load user's personal filters
    user_sieve = load_sieve(recipient.user)
    if user_sieve {
        execute_sieve(user_sieve, message)
    }

    # Apply machine learning classification
    ml_category = classify(message.body, model="intent_classifier")
    message.add_header("X-Category", ml_category)

    if ml_category == "invoice" {
        message.folder = "Finance"
    } elsif ml_category == "support" {
        message.folder = "Support"
        message.forward_to("support-team@company.com")
    }
}

policy compliance {
    stage: post_data
    priority: 95

    # DLP - Data Loss Prevention
    for pattern in config.sensitive_patterns {
        if message.body matches pattern {
            log_security_event("DLP: Sensitive data detected", {
                pattern: pattern.name,
                sender: sender.email,
                recipient: recipient.email
            })

            if sender.domain != recipient.domain {
                reject "Cannot send sensitive data externally"
            }
        }
    }

    # Encryption enforcement
    if message.has_header("X-Require-Encryption") {
        if not message.encrypted {
            reject "Message must be encrypted"
        }
    }

    # Retention policy
    if recipient.user.retention_policy {
        message.expire_after(recipient.user.retention_policy.days)
    }
}

policy analytics {
    stage: delivery
    priority: 10
    async: true  # Don't block delivery

    # Send to analytics pipeline
    publish_event("mail.delivered", {
        timestamp: now(),
        sender: sender.email,
        recipient: recipient.email,
        size: message.size,
        subject_hash: hash(message.subject),
        has_attachments: message.attachments.length > 0,
        spam_score: message.spam_score,
        processing_time: message.processing_time_ms
    })

    # Update user statistics
    increment_counter("mail.received", {
        user: recipient.user,
        hour: now().hour
    })
}
```

### AfterScript Language Features

```yaml
afterscript:
  enabled: true
  policy_dir: "/etc/mail/afterscript/policies"

  # Built-in functions
  functions:
    # Scanning
    - scan_attachment(attachment, engines)
    - check_reputation(email)
    - check_dnsbl(ip, list)

    # Classification
    - classify(text, model)
    - extract_entities(text)
    - sentiment_analysis(text)

    # Actions
    - quarantine(reason)
    - tempfail(reason)
    - reject(reason)
    - remove_attachment(attachment)
    - encrypt_message(method)
    - sign_message(key)

    # Data
    - lookup_ldap(dn, filter)
    - query_database(sql)
    - http_request(url, method, body)
    - cache_get(key)
    - cache_set(key, value, ttl)

    # Logging
    - log_event(category, data)
    - alert(severity, message)
    - publish_event(topic, data)

  # Available stages
  stages:
    - connect       # TCP connection established
    - ehlo          # HELO/EHLO received
    - mail_from     # MAIL FROM received
    - rcpt_to       # RCPT TO received
    - pre_data      # Before DATA command
    - post_data     # After DATA received
    - queue         # Message queued
    - delivery      # Local delivery

  # Execution
  execution:
    timeout: 5000ms  # Policy execution timeout
    max_policies: 50
    allow_async: true
```

## ManageSieve Protocol (RFC 5804)

Users can manage Sieve scripts via ManageSieve protocol:

```
$ sieveshell --user=user@msgs.global --port=4190 apps.afterdarksys.com

> list
main.sieve  <- ACTIVE
vacation.sieve
spam-filters.sieve

> get main.sieve
require ["fileinto", "reject"];

if header :contains "X-Spam-Flag" "YES" {
    fileinto "Junk";
    stop;
}

> put spam-filters.sieve /path/to/local/script.sieve
ok

> activate spam-filters.sieve
ok

> delete vacation.sieve
ok

> logout
```

## Multi-Stage Processing

### Stage 1: SMTP Connection (AfterScript)

```afterscript
policy connection_filter {
    stage: connect

    # Check IP reputation
    if check_dnsbl(connection.ip, "zen.spamhaus.org") {
        reject "IP blacklisted"
    }

    # Geographic restrictions
    if connection.geoip.country in config.blocked_countries {
        reject "Country blocked"
    }

    # Rate limiting by IP
    if count(connection.ip, window=1h) > 100 {
        tempfail "Too many connections"
    }
}
```

### Stage 2: RCPT TO (AfterScript + Access Control)

```afterscript
policy recipient_validation {
    stage: rcpt_to

    # Verify recipient exists
    if not recipient.exists {
        reject "User unknown"
    }

    # Check recipient's quota
    if recipient.quota_used > recipient.quota_limit * 0.95 {
        tempfail "Mailbox nearly full"
    }

    # Vacation check
    if recipient.vacation_enabled {
        message.add_header("X-Vacation-Active", "yes")
    }
}
```

### Stage 3: POST-DATA (Attachment Scanning)

```afterscript
policy attachment_scan {
    stage: post_data

    for attachment in message.attachments {
        result = scan_attachment(attachment, ["clamav"])

        if result.infected {
            remove_attachment(attachment)
            message.quarantine_reason = "Virus: {result.signature}"
            quarantine("Infected attachment removed")
        }
    }
}
```

### Stage 4: Delivery (User Sieve Scripts)

```sieve
# User's personal filter
require ["fileinto", "variables"];

if address :is "from" "boss@company.com" {
    fileinto "Important";
}
```

## API for Filter Management

### REST API

```bash
# List user's filters
GET /api/v1/users/{email}/filters
Authorization: Bearer {token}

Response:
{
  "filters": [
    {
      "id": "filter-1",
      "name": "Spam Detection",
      "enabled": true,
      "priority": 100,
      "conditions": [...],
      "actions": [...]
    }
  ]
}

# Create filter
POST /api/v1/users/{email}/filters
{
  "name": "Important Senders",
  "conditions": [
    {
      "type": "from",
      "operator": "is_one_of",
      "values": ["boss@company.com"]
    }
  ],
  "actions": [
    {"type": "flag", "flag": "flagged"},
    {"type": "fileinto", "folder": "Important"}
  ]
}

# Upload Sieve script
PUT /api/v1/users/{email}/sieve/script.sieve
Content-Type: text/plain

require ["fileinto"];
if header :contains "Subject" "urgent" {
    fileinto "Urgent";
}

# Validate Sieve script
POST /api/v1/sieve/validate
{
  "script": "require [\"fileinto\"];\nif header..."
}

Response:
{
  "valid": true,
  "errors": [],
  "warnings": ["Extension 'regex' not available"]
}

# Test filter
POST /api/v1/users/{email}/filters/test
{
  "filter_id": "filter-1",
  "test_message": {
    "from": "spam@evil.com",
    "subject": "BUY NOW!!!",
    "headers": {...}
  }
}

Response:
{
  "matched": true,
  "actions_taken": [
    {"type": "fileinto", "folder": "Junk"},
    {"type": "stop"}
  ]
}
```

## Configuration

```yaml
# Sieve configuration
sieve:
  enabled: true
  extensions:
    - fileinto
    - reject
    - vacation
    - envelope
    - body
    - imap4flags
    - variables
    - regex
    - date
    - editheader

  managesieve:
    enabled: true
    addr: ":4190"
    require_tls: true

  storage:
    script_dir: "/var/mail/{domain}/{user}/.sieve"
    active_script: "main.sieve"
    backup_scripts: true
    max_backups: 10

  limits:
    max_script_size: 102400
    max_actions: 100
    max_redirects: 5

  gui:
    enabled: true
    auto_compile: true
    show_generated_sieve: true

# AfterScript configuration
afterscript:
  enabled: true
  policy_dir: "/etc/mail/afterscript/policies"
  include_dirs:
    - "/etc/mail/afterscript/lib"
    - "/var/lib/afterscript/modules"

  execution:
    timeout: 5000ms
    max_concurrent: 100
    enable_sandbox: true

  integrations:
    clamav:
      socket: "/var/run/clamav/clamd.sock"
    yara:
      rules_dir: "/etc/yara/rules"
    ml_models:
      intent_classifier: "/var/lib/ml/intent_v2.model"
      sentiment_analyzer: "/var/lib/ml/sentiment_v1.model"

  logging:
    level: "info"
    log_policy_execution: true
    log_performance: true
```

## See Also

- [IMAP Implementation](./IMAP.md)
- [Access Control](../internal/access/README.md)
- [Content Filtering](./CONTENT_FILTERING.md)
- [Security Guide](../SECURITY.md)
