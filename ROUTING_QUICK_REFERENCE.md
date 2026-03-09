# Routing System Quick Reference Card

**Version:** 1.0
**Date:** 2026-03-08

---

## File Locations

| Component | Location |
|-----------|----------|
| Master Control Config | `/master.yaml` |
| Divert Rules Config | `/divert.yaml` |
| Screen Rules Config | `/screen.yaml` |
| Groups Config | `/groups.yaml` |
| Divert Audit Log | `/var/log/mail/divert-audit.log` |
| Screen Audit Log | `/var/log/mail/screen-audit.log` |

---

## Configuration Syntax

### Master Control (master.yaml)

```yaml
services:
  service-name:
    type: smtp|imap|jmap
    enabled: true|false
    listen: "0.0.0.0:port"
    workers: number
    tls_mode: implicit|required|optional
    settings:
      require_auth: true|false
      filters: [list]
```

### Groups (groups.yaml)

```yaml
groups:
  group-name:
    type: static|ldap|dynamic
    members: [list]              # For static
    ldap_query: "query"          # For LDAP
    query: "SQL"                 # For dynamic
```

### Divert Rules (divert.yaml)

```yaml
divert_rules:
  - name: "Rule Name"
    enabled: true|false
    match:
      type: recipient|sender|group|domain|content
      value: "match-value"
    action:
      divert_to: "email@example.com"
      reason: "explanation"
      encrypt: true|false
```

### Screen Rules (screen.yaml)

```yaml
screen_rules:
  - name: "Rule Name"
    enabled: true|false
    match:
      type: user|group|sender|domain|content
      direction: both|inbound|outbound
    action:
      screen_to: [watchers]
      sample_rate: 0.0-1.0
      retention_days: number
```

---

## Common Commands

### Validate Configuration

```bash
# Validate all configs (when tools are built)
go run cmd/validate-master/main.go master.yaml
go run cmd/validate-divert/main.go divert.yaml
go run cmd/validate-screen/main.go screen.yaml
go run cmd/validate-groups/main.go groups.yaml
```

### Test Routing

```bash
# Dry-run test
go run cmd/test-routing/main.go \
  --from alice@example.com \
  --to bob@company.com \
  --dry-run
```

### View Audit Logs

```bash
# Tail divert audit
tail -f /var/log/mail/divert-audit.log | jq .

# Tail screen audit
tail -f /var/log/mail/screen-audit.log | jq .

# Search for specific address
grep "bob@company.com" /var/log/mail/divert-audit.log
```

### Reload Configuration

```bash
# Auto-reload (if hot_reload enabled in master.yaml)
# Just edit the file, it reloads automatically every 5s

# Manual reload (via API or signal)
kill -HUP $(pidof go-emailservice-ads)
```

---

## Match Types Reference

### Divert Match Types

| Type | Matches | Example |
|------|---------|---------|
| `recipient` | Specific email to | `bob@company.com` |
| `sender` | Specific email from | `alice@example.com` |
| `group` | Any member of group | `executives` |
| `domain` | Any email @domain | `company.com` |
| `content` | Message body/headers | `(confidential\|secret)` |

### Screen Match Types

| Type | Matches | Example |
|------|---------|---------|
| `user` | User (sent or received) | `ceo@company.com` |
| `group` | Group members | `sales-team` |
| `sender` | Specific sender | `contractor@external.com` |
| `domain` | Domain | `competitor.com` |
| `content` | Keywords in message | `[merger, acquisition]` |

---

## Divert vs Screen Decision Tree

```
Should recipient NEVER see the message?
  ├─ YES → Use DIVERT
  │   - Legal holds
  │   - Active investigations
  │   - After-hours routing
  │
  └─ NO → Use SCREEN
      - Compliance monitoring
      - Executive oversight
      - Quality assurance
      - Security monitoring
```

---

## Performance Guidelines

| Operation | Typical Time |
|-----------|--------------|
| Recipient/sender match | < 1ms |
| Group lookup (cached) | < 5ms |
| Content match | < 10ms |
| Message copying (screen) | < 2ms per watcher |
| Divert composition | < 5ms |
| **Total overhead** | **< 10ms average** |

---

## Security Checklist

- [ ] Divert rules use `encrypt: true` for sensitive content
- [ ] Screen rules use `encrypt: true` for compliance
- [ ] Audit logs protected (permissions 640 or 600)
- [ ] Config files protected (permissions 640)
- [ ] Watchers have proper security clearance
- [ ] Rules documented with justification
- [ ] Compliance requirements verified
- [ ] Access control configured

---

## Common Patterns

### Legal Hold (Silent Diversion)

```yaml
# divert.yaml
- name: "Legal Hold - Employee X"
  enabled: true
  match:
    type: recipient
    value: employee@company.com
  action:
    divert_to: legal@company.com
    reason: "Legal hold - Case #12345"
    notify_sender: false
    encrypt: true
```

### Executive Monitoring (Transparent)

```yaml
# screen.yaml
- name: "CEO Monitoring"
  enabled: true
  match:
    type: user
    value: ceo@company.com
    direction: both
  action:
    screen_to: [board-compliance@company.com]
    encrypt: true
    retention_days: 2555
    add_header: false
```

### Compliance Keywords (Alert)

```yaml
# screen.yaml
- name: "Insider Trading Alert"
  enabled: true
  match:
    type: content
    keywords: [MNPI, "insider information"]
    case_insensitive: true
  action:
    screen_to: [compliance@company.com]
    alert: true
    priority: high
    encrypt: true
```

### Quality Sampling (10%)

```yaml
# screen.yaml
- name: "Support QA"
  enabled: true
  match:
    type: group
    value: support-team
    direction: outbound
  action:
    screen_to: [qa-team@company.com]
    sample_rate: 0.1
    retention_days: 90
```

---

## Troubleshooting

### Rule Not Triggering

1. Check `enabled: true`
2. Verify match criteria
3. Check audit logs
4. Test with dry-run

### Performance Issues

1. Check group cache hit rate
2. Optimize content patterns
3. Reduce watchers per rule
4. Use sampling

### Audit Log Issues

1. Verify directory exists: `/var/log/mail/`
2. Check permissions
3. Check disk space
4. Rotate logs if too large

---

## Retention Periods

| Use Case | Recommended Retention |
|----------|----------------------|
| General business | 90 days |
| Financial (SEC/FINRA) | 2555 days (7 years) |
| Healthcare (HIPAA) | 2555 days (7 years) |
| Legal matters | 2555-3650 days (7-10 years) |
| Short-term monitoring | 30-90 days |

---

## Group Syntax

### Reference in Rules

```yaml
match:
  type: group
  value: group-name  # References groups.yaml
```

### Expand in Recipients

```
To: @group-name
# Expands to all members
```

### Cache Timing

- **Duration:** 5 minutes
- **Invalidation:** Automatic on reload
- **Manual:** Via API or restart

---

## Important Behaviors

### Divert System
- ✅ Original recipient NEVER receives message
- ✅ NO bounce sent to sender
- ✅ Silent redirection
- ✅ All diversions logged

### Screen System
- ✅ Original recipient DOES receive message
- ✅ Watchers receive copies
- ✅ Transparent to sender/recipient
- ✅ All screenings logged

### Both Systems
- ✅ Fail-open (delivery continues on error)
- ✅ Immutable audit trail
- ✅ Encryption support
- ✅ Hot-reload support

---

## Key Limits

| Limit | Default | Configurable |
|-------|---------|--------------|
| Max watchers per rule | 5 | Yes (settings.max_watchers) |
| Max message size | 50MB | Yes (settings.max_message_size) |
| Group cache TTL | 5 minutes | No (hardcoded) |
| Hot-reload interval | 5 seconds | Yes (hot_reload.check_interval) |
| Max connections | 10000 | Yes (resource_limits.max_connections) |

---

## Emergency Procedures

### Disable All Divert Rules

```bash
# Edit divert.yaml, set all to enabled: false
sed -i 's/enabled: true/enabled: false/g' divert.yaml

# Or disable divert entirely in code
# Set EnableDivert: false in pipeline config
```

### Disable All Screen Rules

```bash
# Edit screen.yaml, set all to enabled: false
sed -i 's/enabled: true/enabled: false/g' screen.yaml

# Or disable screen entirely
# Set EnableScreen: false in pipeline config
```

### Restore from Backup

```bash
# List backups
ls -lt *.yaml.backup.*

# Restore
cp master.yaml.backup.20260308-120000 master.yaml
# Auto-reloads if hot_reload enabled
```

---

## Documentation Links

| Topic | Document |
|-------|----------|
| Master Control | `docs/MASTER_CONTROL.md` |
| Divert System | `docs/DIVERT_PROXY.md` |
| Screen System | `docs/SCREEN_PROXY.md` |
| Mail Groups | `docs/MAIL_GROUPS.md` |
| Examples | `docs/ROUTING_EXAMPLES.md` |
| Implementation | `ROUTING_IMPLEMENTATION_COMPLETE.md` |

---

## Support Resources

**Configuration Files:**
- `/master.yaml` - Service definitions
- `/divert.yaml` - Diversion rules
- `/screen.yaml` - Screening rules
- `/groups.yaml` - Group definitions

**Source Code:**
- `/internal/master/` - Master control
- `/internal/routing/groups/` - Groups
- `/internal/routing/divert/` - Divert
- `/internal/routing/screen/` - Screen
- `/internal/routing/pipeline.go` - Integration

**Audit Logs:**
- `/var/log/mail/divert-audit.log`
- `/var/log/mail/screen-audit.log`

---

**Quick Reference Version 1.0 | 2026-03-08**
