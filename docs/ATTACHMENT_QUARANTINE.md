# Attachment Quarantine & Security Service

## Overview

Automated email attachment security service that detects, quarantines, and provides controlled access to potentially malicious attachments.

## Workflow

### 1. Inbound Email Processing

When an email arrives with attachments:

```
┌─────────────────────┐
│  Email with         │
│  Attachments        │
└──────────┬──────────┘
           │
           v
┌─────────────────────┐
│  Attachment Scanner │
│  - Virus/Malware    │
│  - File Type Check  │
│  - Content Analysis │
└──────────┬──────────┘
           │
           v
      ┌────┴─────┐
      │          │
  CLEAN         SUSPICIOUS
      │          │
      v          v
  Deliver   Quarantine
```

### 2. Quarantine Process

**Original Email**:
```
From: sender@example.com
To: user@msgs.global
Subject: Important Document
Attachments: document.pdf (245KB)

Hello, please see the attached document...
```

**Modified Email Delivered to User**:
```
From: sender@example.com
To: user@msgs.global
Subject: Important Document
[SECURITY NOTICE] Attachments quarantined

Hello, please see the attached document...

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🛡️ ATTACHMENT SECURITY NOTICE

Your attachments have been quarantined for security review.

Your attachments can be found here:
https://quarantine.msgs.global/view/abc123xyz

1 attachment(s) quarantined:
• document.pdf (245KB) - Pending review

Click the link above to authenticate and access your attachments.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### 3. User Access Flow

```
User clicks link
     │
     v
┌────────────────────────────┐
│ OAuth2 Authentication       │
│ (msgs.global SSO)           │
└────────────┬───────────────┘
             │
             v
┌────────────────────────────┐
│ Quarantine Web Interface    │
│                             │
│ Email: sender@example.com   │
│ Date: 2026-03-09 10:23 AM   │
│ Subject: Important Document │
│                             │
│ ┌─────────────────────────┐ │
│ │ 📎 document.pdf         │ │
│ │ 245KB                   │ │
│ │ Status: SAFE            │ │
│ │                         │ │
│ │ [Download] [Delete]     │ │
│ └─────────────────────────┘ │
│                             │
│ [Release Entire Email]      │
└─────────────────────────────┘
```

### 4. Admin Interface

Mailbox administrators have additional controls:

```
┌──────────────────────────────────┐
│ Quarantine Admin Console          │
│                                   │
│ Mailbox: user@msgs.global         │
│ Quarantined Items: 3              │
│                                   │
│ ┌───────────────────────────────┐ │
│ │ 1. document.pdf               │ │
│ │    From: sender@example.com   │ │
│ │    Status: SAFE               │ │
│ │    [Release] [Delete]         │ │
│ ├───────────────────────────────┤ │
│ │ 2. invoice.xlsx               │ │
│ │    From: accounting@corp.com  │ │
│ │    Status: SUSPICIOUS         │ │
│ │    [Review] [Delete]          │ │
│ ├───────────────────────────────┤ │
│ │ 3. photo.jpg                  │ │
│ │    From: friend@gmail.com     │ │
│ │    Status: SAFE               │ │
│ │    [Release] [Delete]         │ │
│ └───────────────────────────────┘ │
│                                   │
│ Release Options:                  │
│ • Attachment-free only            │
│ • With download instructions      │
│ • Full release with attachments   │
└───────────────────────────────────┘
```

## Release Options

### Option 1: Attachment-Free Release

Email is delivered without attachments, with instructions:

```
[Original email body]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️ ATTACHMENTS REMOVED

The attachments from this email were removed for security reasons.

If you need access to these attachments, please:
1. Visit: https://quarantine.msgs.global/view/abc123xyz
2. Authenticate with your msgs.global account
3. Download the attachments after review

Contact your administrator if you need assistance.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### Option 2: Download Instructions

Email delivered with secure download link:

```
[Original email body]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📥 SECURE ATTACHMENT DOWNLOAD

Your attachments are available for download:
https://quarantine.msgs.global/download/abc123xyz

This link expires in 7 days.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### Option 3: Full Release

Original email delivered with all attachments restored (after admin approval).

## Scheduled Digest

Users can opt for daily/weekly digest of quarantined items:

```
Subject: [Quarantine Digest] 3 emails quarantined this week

🛡️ Quarantine Digest - Week of March 3, 2026

You have 3 quarantined emails:

1. From: sender@example.com
   Subject: Important Document
   Date: March 9, 2026 10:23 AM
   Attachments: 1 (245KB)
   Status: SAFE
   [View] [Release]

2. From: accounting@corp.com
   Subject: Monthly Invoice
   Date: March 8, 2026 2:15 PM
   Attachments: 1 (89KB)
   Status: SUSPICIOUS
   [View] [Delete]

3. From: friend@gmail.com
   Subject: Photos from trip
   Date: March 7, 2026 5:30 PM
   Attachments: 5 (2.3MB)
   Status: SAFE
   [View] [Release All]

View all quarantined items:
https://quarantine.msgs.global/

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Digest Settings: Weekly | [Change to Daily] | [Unsubscribe]
```

## Technical Implementation

### Components Required

1. **Attachment Scanner Service**
   - ClamAV integration for virus scanning
   - File type validation
   - Content analysis (macros, scripts, etc.)
   - YARA rules for malware detection

2. **Quarantine Storage**
   - Secure object storage (S3/MinIO)
   - Metadata database (attachment info, scan results)
   - TTL/expiration (auto-delete after N days)

3. **Quarantine Web Service**
   - OAuth2 authentication (msgs.global SSO)
   - User interface for viewing/downloading
   - Admin interface for management
   - RESTful API for automation

4. **Email Rewriting Service**
   - SMTP milter or content filter
   - Attachment extraction
   - Body modification (add security notice)
   - Original message preservation

5. **Release/Delivery Service**
   - Re-inject emails with attachments
   - Generate download links
   - Send notifications

### Configuration

```yaml
attachment_quarantine:
  enabled: true

  # Storage
  storage:
    type: "s3"  # or "local", "minio"
    bucket: "mail-quarantine"
    endpoint: "s3.amazonaws.com"
    retention_days: 30

  # Scanning
  scanner:
    engine: "clamav"
    clamd_socket: "/var/run/clamav/clamd.sock"
    yara_rules: "/etc/mail/yara-rules/"
    max_scan_size: 52428800  # 50MB

    # File type restrictions
    blocked_extensions:
      - ".exe"
      - ".bat"
      - ".cmd"
      - ".scr"
      - ".vbs"

    # Always quarantine these
    high_risk_extensions:
      - ".zip"
      - ".rar"
      - ".7z"
      - ".docm"  # Macro-enabled docs
      - ".xlsm"

  # Web Interface
  web:
    addr: ":8443"
    base_url: "https://quarantine.msgs.global"
    tls:
      cert: "/etc/letsencrypt/live/quarantine.msgs.global/fullchain.pem"
      key: "/etc/letsencrypt/live/quarantine.msgs.global/privkey.pem"

  # OAuth2 SSO
  oauth2:
    provider: "afterdarksystems"
    client_id: "${ADS_CLIENT_ID}"
    client_secret: "${ADS_CLIENT_SECRET}"
    auth_url: "https://sso.afterdarksystems.com/oauth2/authorize"
    token_url: "https://sso.afterdarksystems.com/oauth2/token"
    userinfo_url: "https://sso.afterdarksystems.com/oauth2/userinfo"
    redirect_url: "https://quarantine.msgs.global/oauth/callback"

  # Digests
  digests:
    enabled: true
    default_schedule: "weekly"  # daily, weekly, monthly
    send_time: "09:00"  # UTC

  # Release options
  release:
    require_admin_approval: false
    auto_release_after_days: 7  # Auto-release SAFE items after N days
    notify_on_release: true

  # Notifications
  notifications:
    email_template: "/etc/mail/templates/quarantine-notice.html"
    from_address: "quarantine@msgs.global"
    from_name: "Mail Security Service"
```

### Database Schema

```sql
CREATE TABLE quarantined_attachments (
    id UUID PRIMARY KEY,
    message_id VARCHAR(255) NOT NULL,
    recipient VARCHAR(255) NOT NULL,
    sender VARCHAR(255) NOT NULL,
    subject TEXT,
    filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(100),
    size_bytes BIGINT,
    storage_path VARCHAR(500),
    scan_result VARCHAR(50),  -- CLEAN, SUSPICIOUS, INFECTED, ERROR
    scan_details JSONB,
    quarantined_at TIMESTAMP DEFAULT NOW(),
    accessed_at TIMESTAMP,
    released_at TIMESTAMP,
    expires_at TIMESTAMP,
    status VARCHAR(50),  -- QUARANTINED, RELEASED, DELETED, EXPIRED
    released_by VARCHAR(255),
    INDEX idx_recipient (recipient),
    INDEX idx_message_id (message_id),
    INDEX idx_status (status),
    INDEX idx_expires_at (expires_at)
);

CREATE TABLE quarantine_access_log (
    id SERIAL PRIMARY KEY,
    attachment_id UUID REFERENCES quarantined_attachments(id),
    user_email VARCHAR(255),
    action VARCHAR(50),  -- VIEW, DOWNLOAD, RELEASE, DELETE
    ip_address INET,
    user_agent TEXT,
    timestamp TIMESTAMP DEFAULT NOW(),
    INDEX idx_attachment_id (attachment_id),
    INDEX idx_user_email (user_email)
);

CREATE TABLE digest_preferences (
    user_email VARCHAR(255) PRIMARY KEY,
    schedule VARCHAR(20),  -- daily, weekly, monthly, disabled
    send_time TIME,
    last_sent TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);
```

### API Endpoints

```
GET  /api/quarantine/list              # List quarantined items for user
GET  /api/quarantine/:id               # Get attachment details
GET  /api/quarantine/:id/download      # Download attachment
POST /api/quarantine/:id/release       # Release attachment
POST /api/quarantine/:id/delete        # Delete attachment
POST /api/quarantine/message/:id/release  # Release entire message

# Admin endpoints
GET  /api/admin/quarantine/list        # List all quarantined items
POST /api/admin/quarantine/:id/approve # Approve and release
POST /api/admin/quarantine/:id/reject  # Reject and delete
GET  /api/admin/stats                  # Quarantine statistics

# Digest endpoints
GET  /api/digest/preferences           # Get digest settings
PUT  /api/digest/preferences           # Update digest settings
POST /api/digest/send-now              # Send digest immediately
```

## Security Considerations

1. **Access Control**
   - Only recipient or admins can access quarantined items
   - OAuth2 authentication required
   - Session management with timeouts
   - Audit logging for all access

2. **Storage Security**
   - Encrypted at rest (S3 encryption or LUKS)
   - Isolated from mail system
   - Regular backups
   - Automatic expiration

3. **Download Security**
   - Time-limited download URLs
   - Rate limiting
   - Virus re-scan before download
   - Content-Disposition headers to prevent auto-execution

4. **Privacy**
   - GDPR compliance (user data deletion)
   - Configurable retention periods
   - Anonymized logging options
   - User consent for digest emails

## Integration Points

### With go-emailservice-ads

```go
// internal/quarantine/service.go
type QuarantineService struct {
    storage Storage
    scanner Scanner
    db      *sql.DB
    logger  *zap.Logger
}

// Called during email processing
func (s *QuarantineService) ProcessAttachments(msg *Message) (*ModifiedMessage, error) {
    var quarantined []Attachment

    for _, att := range msg.Attachments {
        result, err := s.scanner.Scan(att)
        if err != nil {
            return nil, err
        }

        if result.IsSuspicious() || result.IsInfected() {
            // Store attachment
            qID, err := s.storage.Store(att)
            if err != nil {
                return nil, err
            }

            // Save metadata
            err = s.db.SaveQuarantinedAttachment(qID, msg, att, result)
            if err != nil {
                return nil, err
            }

            quarantined = append(quarantined, att)
        }
    }

    if len(quarantined) > 0 {
        // Modify message body
        modified := s.rewriteMessage(msg, quarantined)
        return modified, nil
    }

    return msg, nil
}
```

## Future Enhancements

1. **Machine Learning**
   - Behavioral analysis of attachments
   - Sender reputation scoring
   - Adaptive threat detection

2. **Threat Intelligence**
   - Integration with threat feeds
   - IOC (Indicator of Compromise) matching
   - Sandbox detonation (Cuckoo, ANY.RUN)

3. **User Training**
   - Security awareness emails
   - Phishing simulation integration
   - Reportable suspicious emails

4. **Advanced Features**
   - CDR (Content Disarm and Reconstruction)
   - Attachment sanitization
   - Safe file conversion (PDF→HTML)
   - Watermarking for tracking

## See Also

- [Security Guide](../SECURITY.md)
- [SSO Setup](../SSO_SETUP.md)
- [API Authentication](../API_AUTHENTICATION.md)
- [Content Filtering](./CONTENT_FILTERING.md)
