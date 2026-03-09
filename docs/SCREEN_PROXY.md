# Screen Proxy System

The Screen Proxy System creates transparent copies of messages for watchers while still delivering the original message to the intended recipient. Sender and recipient are unaware of screening.

## Overview

**Critical Behavior:**
- Original recipient DOES receive the message (unchanged)
- Watchers receive exact copies (optionally with screening headers)
- Sender is unaware of screening
- All screening logged for audit trail
- Supports bidirectional monitoring (sent and received)

## Divert vs Screen

| Feature | Divert | Screen |
|---------|--------|--------|
| Original delivery | NO | YES |
| Recipient aware | NO | NO |
| Sender aware | Optional | NO |
| Bounce sent | NO | NO |
| Use case | Investigation | Monitoring |

## Use Cases

1. **Executive Oversight**: Monitor C-level communications
2. **Compliance Monitoring**: Regulated communications (finance, healthcare)
3. **Quality Assurance**: Customer service interactions
4. **Security Monitoring**: Detect data exfiltration
5. **Insider Threat**: Monitor suspicious employees

## Configuration File: `screen.yaml`

### Basic Structure

```yaml
screen_rules:
  - name: "Rule Name"
    enabled: true|false
    match:
      type: user|group|sender|domain|content
      value: "match value"
      direction: both|inbound|outbound
    action:
      screen_to:
        - watcher1@example.com
        - watcher2@example.com
      add_header: true|false
      encrypt: true|false
      retention_days: number
      priority: high|normal|low
      alert: true|false
      sample_rate: 0.0-1.0

settings:
  audit_log: /var/log/mail/screen-audit.log
  max_watchers: 5
  encryption_required: true
  default_retention_days: 90
  preserve_original: true
  notify_watchers: false
```

## Match Types

### 1. User Match

Screen all mail for a specific user (bidirectional):

```yaml
- name: "CEO Monitoring"
  enabled: true
  match:
    type: user
    value: ceo@company.com
    direction: both  # Monitor sent AND received
  action:
    screen_to:
      - board-compliance@company.com
      - legal@company.com
    encrypt: true
    retention_days: 2555  # 7 years
```

**Directions:**
- `both`: Monitor messages sent by AND received by user
- `inbound`: Only messages received by user
- `outbound`: Only messages sent by user

### 2. Group Match

Screen all mail for group members:

```yaml
- name: "Sales Team Monitoring"
  enabled: true
  match:
    type: group
    value: sales-team
    direction: outbound  # Monitor outgoing sales emails
  action:
    screen_to:
      - sales-manager@company.com
    add_header: false  # Completely transparent
    retention_days: 365
```

### 3. Sender Match

Screen mail from specific sender:

```yaml
- name: "Monitor External Contractor"
  enabled: true
  match:
    type: sender
    value: contractor@external.com
  action:
    screen_to:
      - security@company.com
```

### 4. Domain Match

Screen mail to/from specific domains:

```yaml
- name: "Competitor Communications"
  enabled: true
  match:
    type: domain
    value: competitor.com
  action:
    screen_to:
      - competitive-intelligence@company.com
    priority: high
```

### 5. Content Match

Screen mail containing keywords:

```yaml
- name: "Compliance Keywords"
  enabled: true
  match:
    type: content
    keywords:
      - merger
      - acquisition
      - insider
      - material non-public
    case_insensitive: true
  action:
    screen_to:
      - compliance-alerts@company.com
    priority: high
    alert: true
    encrypt: true
```

## Advanced Features

### Sampling

Screen only a percentage of messages:

```yaml
- name: "Support Quality Monitoring"
  enabled: true
  match:
    type: group
    value: support-team
  action:
    screen_to:
      - quality@company.com
    sample_rate: 0.1  # Screen 10% of messages
```

Sample rates:
- `1.0`: 100% (all messages)
- `0.5`: 50% (half of messages)
- `0.1`: 10% (one in ten)
- `0.01`: 1% (one in hundred)

### Priority Levels

Mark screened copies with priority:

```yaml
action:
  priority: high  # high|normal|low
  alert: true     # Send immediate alert to watchers
```

- `high`: Immediate attention required
- `normal`: Standard monitoring
- `low`: Archival/audit purposes

### Encryption

Encrypt screened copies:

```yaml
action:
  encrypt: true  # Recommended for sensitive communications
```

### Retention

Specify how long to keep screened copies:

```yaml
action:
  retention_days: 2555  # 7 years for compliance
```

Common retention periods:
- **90 days**: General business communications
- **365 days**: Financial records (1 year)
- **2555 days**: SEC requirement (7 years)
- **3650 days**: Some healthcare records (10 years)

## Transparent vs Header Mode

### Transparent Mode (Default)

No headers added, completely invisible:

```yaml
action:
  add_header: false  # Completely transparent
```

Screened message is identical to original.

### Header Mode

Add screening headers (useful for watcher filtering):

```yaml
action:
  add_header: true
  header_name: X-Screened-Rule
```

Headers added:
```
X-Screened-For: watcher@company.com
X-Screened-From: alice@example.com
X-Screened-To: bob@company.com
X-Screened-Timestamp: 2026-03-08T18:00:00Z
X-Screened-Rule: CEO Monitoring
```

**Caution:** Headers make screening visible if original recipient somehow receives a copy.

## Multiple Watchers

Each rule can have up to `max_watchers` (default: 5):

```yaml
action:
  screen_to:
    - compliance@company.com
    - legal@company.com
    - audit@company.com
    - ciso@company.com
    - cfo@company.com
```

If limit exceeded, first N watchers are used.

## Bidirectional Monitoring

Monitor both sent and received messages:

```yaml
- name: "Executive Oversight"
  match:
    type: user
    value: cfo@company.com
    direction: both
  action:
    screen_to:
      - board-compliance@company.com
```

**Result:**
- Mail TO cfo@company.com → Copied to board-compliance
- Mail FROM cfo@company.com → Copied to board-compliance

## Audit Logging

All screening events logged in JSON format:

```json
{
  "timestamp": "2026-03-08T18:00:00Z",
  "from_address": "alice@example.com",
  "to_address": "bob@company.com",
  "watchers": ["compliance@company.com", "legal@company.com"],
  "rule_name": "CEO Monitoring",
  "success": true
}
```

### Audit Log Location

Default: `/var/log/mail/screen-audit.log`

Configure in settings:

```yaml
settings:
  audit_log: /path/to/screen-audit.log
```

## Integration with Mail Flow

```
Incoming Email
    ↓
SMTP Server
    ↓
Screen Check ← Check screening rules
    ↓
    ├─ Match Found
    │   ↓
    │   Create Screened Copies (for each watcher)
    │   ↓
    │   Enqueue Copies to Watchers
    │   ↓
    │   Log Audit Entry
    │   ↓
    │   Continue to Normal Delivery ←── Important!
    │
    └─ No Match
        ↓
        Continue Normal Delivery
```

**Key Difference from Divert:** Original delivery ALWAYS happens.

## Performance Impact

Screening adds overhead proportional to watcher count:

- **< 1ms** per message for user/sender/domain matches
- **< 5ms** per message for group lookups (cached)
- **< 10ms** per message for content matching
- **+ N×2ms** for N watchers (message copying)

Example: Message with 3 watchers and content match ≈ 16ms overhead

For high-volume systems:
- Use sampling to reduce load
- Limit watchers per rule
- Avoid content matching when possible

## Security Considerations

### 1. Watcher Privacy

Ensure watchers have:
- Proper security clearance
- Need-to-know justification
- Signed confidentiality agreements

### 2. Encryption

Always encrypt when screening:
- Personally Identifiable Information (PII)
- Health information (HIPAA)
- Financial data
- Attorney-client communications

```yaml
action:
  encrypt: true
```

### 3. Access Control

Restrict:
- Read access to `screen.yaml`
- Write access to `screen.yaml`
- Mailbox access for watchers
- Audit log access

### 4. Data Minimization

Only screen what's necessary:
- Use sampling when appropriate
- Set reasonable retention periods
- Delete screened copies when no longer needed

## Compliance

### GDPR Considerations

When screening EU citizens' communications:

1. **Legal Basis**: Legitimate interest or legal obligation
2. **Transparency**: Inform employees of monitoring
3. **Purpose Limitation**: Only use for stated purpose
4. **Data Minimization**: Screen only what's necessary
5. **Retention**: Delete when purpose fulfilled
6. **Access Rights**: Handle subject access requests

### Industry-Specific

**Finance (SEC, FINRA):**
```yaml
retention_days: 2555  # 7 years minimum
encrypt: true
```

**Healthcare (HIPAA):**
```yaml
encrypt: true  # Required for PHI
retention_days: 2555  # 6-7 years depending on state
```

**Legal (Attorney-Client):**
```yaml
# Generally should NOT screen attorney-client communications
# unless specific legal requirement
```

## Testing Screen Rules

### 1. Dry Run

Test without actually screening:

```bash
go run cmd/test-screen/main.go \
  --config screen.yaml \
  --from alice@example.com \
  --to bob@company.com \
  --dry-run
```

### 2. Validation

```bash
go run cmd/validate-screen-config/main.go screen.yaml
```

### 3. Test Matrix

| Test Case | Expected Result |
|-----------|-----------------|
| CEO inbound | Screened to board-compliance |
| CEO outbound | Screened to board-compliance |
| Sales team outbound | Screened to sales-manager |
| Keyword match | Screened to compliance |
| No match | Normal delivery only |

## Troubleshooting

### Watchers not receiving copies

1. Check rule is enabled
2. Verify match criteria
3. Check audit log:
   ```bash
   tail -f /var/log/mail/screen-audit.log
   ```
4. Verify watcher addresses

### Original delivery not happening

This should NEVER happen with screening. If it does:

1. Check logs for errors
2. Verify it's a screen rule (not divert)
3. Report as critical bug

### Too many watchers error

```yaml
settings:
  max_watchers: 10  # Increase limit
```

Or reduce watchers per rule.

## Best Practices

1. **Document justification**: Clear business need for each rule
2. **Inform employees**: Transparency (where legally required)
3. **Minimize watchers**: Only who needs to know
4. **Use sampling**: Reduce overhead for high-volume monitoring
5. **Encrypt sensitive**: Always encrypt regulated communications
6. **Regular review**: Audit screening rules quarterly
7. **Access control**: Restrict who can modify rules
8. **Test before deploy**: Use dry-run mode
9. **Monitor performance**: Watch for system impact
10. **Retention compliance**: Follow legal requirements

## Complex Scenario Examples

### Executive Team Oversight

```yaml
- name: "C-Level Monitoring"
  match:
    type: group
    value: executives
    direction: both
  action:
    screen_to:
      - board-compliance@company.com
      - general-counsel@company.com
    encrypt: true
    retention_days: 2555
    add_header: false  # Transparent
```

### Customer Communications Quality

```yaml
- name: "Customer Service QA"
  match:
    type: group
    value: support-team
    direction: outbound
  action:
    screen_to:
      - qa-team@company.com
    sample_rate: 0.2  # 20% sampling
    retention_days: 90
    priority: normal
```

### Insider Threat Detection

```yaml
- name: "Data Exfiltration Monitoring"
  match:
    type: content
    keywords:
      - "personal email"
      - "download database"
      - "my last day"
      - "competitor offer"
    case_insensitive: true
  action:
    screen_to:
      - security-operations@company.com
    priority: high
    alert: true
    encrypt: true
```

### M&A Confidentiality

```yaml
- name: "Merger Keywords"
  match:
    type: content
    keywords:
      - Project Titan  # Codename for M&A
      - acquisition target
      - due diligence
    case_insensitive: true
  action:
    screen_to:
      - legal@company.com
      - m-and-a-team@company.com
    priority: high
    encrypt: true
    retention_days: 2555
```

## See Also

- [Divert Proxy System](DIVERT_PROXY.md) - Silent redirection
- [Mail Groups](MAIL_GROUPS.md) - Group management
- [Master Control](MASTER_CONTROL.md) - Service configuration
