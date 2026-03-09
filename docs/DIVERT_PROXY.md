# Divert Proxy System

The Divert Proxy System intercepts mail to specific recipients and silently redirects it to different recipients. The original recipient never receives the message, and no bounce is sent to the sender.

## Overview

**Critical Behavior:**
- Original recipient NEVER receives the message
- NO bounce message sent to sender (silent diversion)
- Message is rewritten with diversion notice
- Original message attached as RFC822
- All diversions logged for audit trail

## Use Cases

1. **Legal Hold**: Divert all mail for users under investigation
2. **Employee Monitoring**: Redirect employee communications to compliance
3. **Content Filtering**: Intercept messages containing sensitive keywords
4. **After-Hours Routing**: Redirect support messages to on-call staff
5. **Compliance Archiving**: Capture regulated communications

## Configuration File: `divert.yaml`

### Basic Structure

```yaml
divert_rules:
  - name: "Rule Name"
    enabled: true|false
    match:
      type: recipient|sender|group|domain|content
      value: "match value"
      # Optional fields based on type
    action:
      divert_to: "new-recipient@example.com"
      reason: "Explanation for diversion"
      notify_sender: false
      encrypt: true
      attach_original: true

settings:
  audit_log: /var/log/mail/divert-audit.log
  attachment_format: message/rfc822
  include_headers: true
  hash_algorithm: sha256
  max_message_size: 52428800
```

## Match Types

### 1. Recipient Match

Divert mail to a specific email address:

```yaml
- name: "Bob Legal Hold"
  enabled: true
  match:
    type: recipient
    value: bob@company.com
  action:
    divert_to: compliance@company.com
    reason: "Legal hold - Case #12345"
```

### 2. Sender Match

Divert mail from a specific sender:

```yaml
- name: "Monitor Contractor"
  enabled: true
  match:
    type: sender
    value: contractor@external.com
  action:
    divert_to: security@company.com
    reason: "External contractor monitoring"
```

### 3. Group Match

Divert mail to any member of a group:

```yaml
- name: "Executive Monitoring"
  enabled: true
  match:
    type: group
    value: executives
  action:
    divert_to: board-compliance@company.com
    reason: "Executive communication monitoring"
```

### 4. Domain Match

Divert mail to any address in a domain:

```yaml
- name: "Legal Department"
  enabled: true
  match:
    type: domain
    value: legal.company.com
  action:
    divert_to: legal-archive@company.com
    reason: "Legal department archiving"
```

### 5. Content Match

Divert mail containing specific keywords:

```yaml
- name: "Sensitive Keywords"
  enabled: true
  match:
    type: content
    pattern: "(confidential|secret|proprietary)"
    case_insensitive: true
  action:
    divert_to: security@company.com
    reason: "Sensitive content detected"
```

## Time-Based Diversion

Divert only during specific times:

```yaml
- name: "After Hours Support"
  enabled: true
  match:
    type: recipient
    value: support@company.com
    schedule:
      days: [saturday, sunday]
      hours: all
  action:
    divert_to: oncall@company.com
    reason: "After-hours support routing"
```

### Schedule Options

**Days:**
- `[monday, tuesday, wednesday, thursday, friday, saturday, sunday]`
- Or use: `weekdays`, `weekends`, `all`

**Hours:**
- `all`: 24/7
- `business`: 9am-5pm (configurable)
- `HH:MM-HH:MM`: Specific time range

## Diverted Message Format

When a message is diverted, it's rewritten to include:

### Headers

```
From: Mail System <postmaster@divert-system>
To: compliance@company.com
Subject: [DIVERTED] Original Subject
X-Divert-Original-Recipient: bob@company.com
X-Divert-Original-Sender: alice@example.com
X-Divert-Reason: Legal hold - Case #12345
X-Divert-Timestamp: 2026-03-08T18:00:00Z
X-Divert-Message-Hash: sha256-hash-of-original
```

### Body

```
MAIL DIVERSION NOTICE
====================

This message was diverted from its original recipient per system policy.

Original Recipient: bob@company.com
Original Sender: alice@example.com
Diverted To: compliance@company.com
Diversion Reason: Legal hold - Case #12345
Diverted At: 2026-03-08 18:00:00 (UTC)

The original message is attached below as an RFC822 message.

Message Hash (SHA-256): abc123...

This is an automated system message. Do not reply.

[Original message attached as message/rfc822]
```

## Action Options

### divert_to

**Required.** Email address to receive diverted message.

```yaml
action:
  divert_to: compliance@company.com
```

### reason

**Required.** Explanation for why message was diverted.

```yaml
action:
  reason: "Legal hold - Case #12345"
```

### notify_sender

**Default: false.** Whether to send notification to original sender.

```yaml
action:
  notify_sender: false  # Silent diversion (recommended)
```

**WARNING:** Setting `notify_sender: true` defeats the purpose of silent diversion and may alert subjects of investigation.

### encrypt

**Default: false.** Whether to encrypt the diverted message.

```yaml
action:
  encrypt: true  # Recommended for compliance
```

### attach_original

**Default: true.** Whether to attach original message.

```yaml
action:
  attach_original: true
```

## Audit Logging

All diversions are logged to the audit log in JSON format:

```json
{
  "timestamp": "2026-03-08T18:00:00Z",
  "from_address": "alice@example.com",
  "to_address": "bob@company.com",
  "diverted_to": "compliance@company.com",
  "rule_name": "Bob Legal Hold",
  "reason": "Legal hold - Case #12345",
  "message_hash": "sha256-hash",
  "success": true
}
```

### Audit Log Location

Default: `/var/log/mail/divert-audit.log`

Configure in `settings`:

```yaml
settings:
  audit_log: /path/to/divert-audit.log
```

### Audit Log Retention

The audit log is append-only and should be:
- Backed up regularly
- Retained per compliance requirements (typically 7 years)
- Protected with appropriate permissions (640 or 600)
- Monitored for tampering

## Security Considerations

### 1. No Bounce Guarantee

The system NEVER sends bounce messages for diverted mail. This is critical for:
- Legal investigations
- Insider threat monitoring
- Compliance oversight

### 2. Encryption

Always encrypt diverted messages containing:
- Personally Identifiable Information (PII)
- Health information (HIPAA)
- Financial data (PCI-DSS)
- Legal communications (attorney-client privilege)

```yaml
action:
  encrypt: true
```

### 3. Access Control

Restrict who can:
- View divert rules: Read access to `divert.yaml`
- Modify divert rules: Write access to `divert.yaml`
- Access diverted messages: Mailbox access for divert recipients
- View audit logs: Read access to audit log files

### 4. Compliance

Ensure diversion rules comply with:
- **GDPR**: Data minimization, purpose limitation
- **ECPA**: Electronic Communications Privacy Act
- **SCA**: Stored Communications Act
- **Company policy**: Employee notification requirements

## Multiple Rules

Messages can match multiple rules. Behavior:

1. **First match wins** for the same recipient
2. **All rules evaluated** for different recipients
3. **Divert takes precedence** over normal delivery

Example:

```yaml
divert_rules:
  - name: "Specific User"
    match:
      type: recipient
      value: bob@company.com
    action:
      divert_to: compliance@company.com

  - name: "Entire Group"
    match:
      type: group
      value: executives
    action:
      divert_to: board@company.com
```

If Bob is in the executives group:
- Mail to bob@company.com → Goes to compliance@company.com (first match)
- Mail to other executives → Goes to board@company.com

## Testing Divert Rules

### 1. Dry Run Mode

Test rules without actually diverting:

```bash
go run cmd/test-divert/main.go \
  --config divert.yaml \
  --from alice@example.com \
  --to bob@company.com \
  --dry-run
```

### 2. Validation

Validate configuration:

```bash
go run cmd/validate-divert-config/main.go divert.yaml
```

### 3. Test Cases

Before enabling in production:

```yaml
# Test case 1: Specific recipient
# Expected: Diverted to compliance@company.com

# Test case 2: Group member
# Expected: Diverted to group watcher

# Test case 3: Non-matching message
# Expected: Normal delivery
```

## Performance Impact

Divert checks add minimal overhead:

- **< 1ms** per message for recipient/sender/domain matches
- **< 5ms** per message for group lookups (cached)
- **< 10ms** per message for content matching

For high-volume systems:
- Use recipient/sender matches when possible
- Avoid complex regex in content matches
- Cache group memberships (automatic)

## Troubleshooting

### Message not diverted

1. Check rule is enabled:
   ```yaml
   enabled: true
   ```

2. Verify match criteria:
   ```bash
   grep "Divert rule matched" /var/log/mail/service.log
   ```

3. Check audit log:
   ```bash
   tail -f /var/log/mail/divert-audit.log
   ```

### Message diverted incorrectly

1. Review rule order (first match wins)
2. Check group membership
3. Verify pattern matching (case sensitivity)

### Audit log not writing

1. Check directory permissions:
   ```bash
   ls -la /var/log/mail/
   ```

2. Verify path in config:
   ```yaml
   settings:
     audit_log: /var/log/mail/divert-audit.log
   ```

## Integration with Mail Flow

```
Incoming Email
    ↓
SMTP Server
    ↓
Divert Check ← Check divert rules
    ↓
    ├─ Match Found
    │   ↓
    │   Create Divert Message
    │   ↓
    │   Enqueue to Divert Recipient
    │   ↓
    │   Log Audit Entry
    │   ↓
    │   STOP (original recipient never gets it)
    │
    └─ No Match
        ↓
        Continue Normal Delivery
```

## Best Practices

1. **Minimize rules**: Only divert when absolutely necessary
2. **Document reasons**: Clear justification in `reason` field
3. **Regular review**: Audit divert rules quarterly
4. **Test thoroughly**: Use dry-run mode before production
5. **Monitor audit logs**: Watch for anomalies
6. **Encrypt sensitive**: Always encrypt compliance-related diversions
7. **Retention policy**: Follow legal requirements for audit logs
8. **Access control**: Restrict who can modify divert rules
9. **Change management**: Review changes before applying
10. **Incident response**: Have a plan for handling diversion failures

## Legal Considerations

**CONSULT YOUR LEGAL TEAM** before implementing mail diversion for:

- Employee monitoring
- Legal holds
- Regulatory compliance
- Insider threat investigations

Requirements vary by:
- Jurisdiction
- Industry (healthcare, finance, etc.)
- Employment law
- Privacy regulations

## See Also

- [Screen Proxy System](SCREEN_PROXY.md) - Transparent monitoring
- [Mail Groups](MAIL_GROUPS.md) - Group management
- [Master Control](MASTER_CONTROL.md) - Service configuration
