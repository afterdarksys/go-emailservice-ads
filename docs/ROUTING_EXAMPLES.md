# Real-World Routing Examples

This document provides real-world scenarios and complete configurations for the divert and screen proxy systems.

## Table of Contents

1. [Legal Hold Scenarios](#legal-hold-scenarios)
2. [Executive Monitoring](#executive-monitoring)
3. [Compliance & Regulatory](#compliance--regulatory)
4. [Customer Service](#customer-service)
5. [Security & Insider Threat](#security--insider-threat)
6. [Department-Specific](#department-specific)
7. [Complex Multi-Rule Scenarios](#complex-multi-rule-scenarios)

---

## Legal Hold Scenarios

### Scenario 1: Employee Under Investigation

**Requirement:** Silently divert all mail to/from suspect employee to legal team.

**Configuration:**

```yaml
# groups.yaml
groups:
  legal-team:
    type: static
    members:
      - general-counsel@company.com
      - legal-admin@company.com
      - outside-counsel@lawfirm.com

# divert.yaml
divert_rules:
  - name: "Investigation - John Doe"
    enabled: true
    match:
      type: recipient
      value: john.doe@company.com
    action:
      divert_to: legal-team-archive@company.com
      reason: "Legal hold - Case #2024-0315"
      notify_sender: false
      encrypt: true
      attach_original: true

# screen.yaml (monitor outgoing too)
screen_rules:
  - name: "Investigation - John Doe Outbound"
    enabled: true
    match:
      type: sender
      value: john.doe@company.com
    action:
      screen_to:
        - legal-team-archive@company.com
      encrypt: true
      retention_days: 2555
```

**Result:**
- Inbound mail to John → Diverted to legal (John never sees it)
- Outbound mail from John → Copied to legal (John's mail still goes out)
- All logged in audit trail
- Completely transparent to John

---

### Scenario 2: Department-Wide Hold

**Requirement:** Preserve all communications for entire department.

```yaml
# groups.yaml
groups:
  accounting-dept:
    type: ldap
    ldap_query: "(&(ou=accounting)(objectClass=user)(mail=*))"
    ldap_server: "ldap://ldap.company.com"

# screen.yaml
screen_rules:
  - name: "Accounting Department Hold"
    enabled: true
    match:
      type: group
      value: accounting-dept
      direction: both
    action:
      screen_to:
        - legal-holds@company.com
        - forensics-team@company.com
      encrypt: true
      retention_days: 2555  # 7 years
      add_header: false
```

**Result:**
- All mail to/from accounting department copied to legal holds
- Transparent to accounting team
- 7-year retention for compliance

---

## Executive Monitoring

### Scenario 3: C-Level Oversight

**Requirement:** Board requires copies of all C-level communications.

```yaml
# groups.yaml
groups:
  c-level:
    type: static
    members:
      - ceo@company.com
      - cfo@company.com
      - cto@company.com
      - coo@company.com
      - ciso@company.com
    metadata:
      owner: board@company.com
      purpose: "C-level executives"
      sensitivity: "high"

# screen.yaml
screen_rules:
  - name: "C-Level Communications Monitoring"
    enabled: true
    match:
      type: group
      value: c-level
      direction: both
    action:
      screen_to:
        - board-compliance@company.com
        - corporate-secretary@company.com
      encrypt: true
      retention_days: 2555
      add_header: false
      priority: high
```

**Result:**
- All C-level email monitored
- Board receives copies
- Encrypted and retained for 7 years

---

### Scenario 4: CEO Specific Monitoring

**Requirement:** General Counsel must review all CEO communications.

```yaml
# screen.yaml
screen_rules:
  - name: "CEO Email Review"
    enabled: true
    match:
      type: user
      value: ceo@company.com
      direction: both
    action:
      screen_to:
        - general-counsel@company.com
      encrypt: true
      retention_days: 3650  # 10 years
      add_header: true
      header_name: X-Executive-Screening
```

---

## Compliance & Regulatory

### Scenario 5: Financial Services (SEC/FINRA)

**Requirement:** Monitor traders for insider trading compliance.

```yaml
# groups.yaml
groups:
  trading-desk:
    type: static
    members:
      - trader1@company.com
      - trader2@company.com
      - trader3@company.com

# screen.yaml
screen_rules:
  - name: "Trading Desk Compliance"
    enabled: true
    match:
      type: group
      value: trading-desk
      direction: both
    action:
      screen_to:
        - compliance@company.com
        - finra-surveillance@company.com
      encrypt: true
      retention_days: 2555  # SEC requirement
      priority: high

  - name: "Material Non-Public Information"
    enabled: true
    match:
      type: content
      keywords:
        - "MNPI"
        - "material non-public"
        - "inside information"
        - "earnings release"
      case_insensitive: true
    action:
      screen_to:
        - compliance-alerts@company.com
      alert: true
      priority: high
      encrypt: true
```

**Result:**
- All trader communications archived
- Keyword alerts for suspicious content
- 7-year retention per SEC requirements

---

### Scenario 6: Healthcare (HIPAA)

**Requirement:** Monitor PHI communications for HIPAA compliance.

```yaml
# groups.yaml
groups:
  healthcare-staff:
    type: dynamic
    query: "SELECT email FROM employees WHERE department='healthcare' AND active=true"
    database: "hr_db"

# screen.yaml
screen_rules:
  - name: "HIPAA Compliance Monitoring"
    enabled: true
    match:
      type: group
      value: healthcare-staff
      direction: both
    action:
      screen_to:
        - hipaa-compliance@hospital.com
      encrypt: true  # Required for PHI
      retention_days: 2555  # 6-7 years per state law

  - name: "PHI Keywords Alert"
    enabled: true
    match:
      type: content
      keywords:
        - "patient"
        - "diagnosis"
        - "SSN"
        - "medical record"
      case_insensitive: true
    action:
      screen_to:
        - privacy-officer@hospital.com
      alert: true
      encrypt: true
```

---

## Customer Service

### Scenario 7: Support Quality Assurance

**Requirement:** QA team reviews sample of customer interactions.

```yaml
# groups.yaml
groups:
  support-team:
    type: dynamic
    query: "SELECT email FROM employees WHERE role='support' AND active=true"
    database: "hr_db"

# screen.yaml
screen_rules:
  - name: "Support QA Monitoring"
    enabled: true
    match:
      type: group
      value: support-team
      direction: outbound
    action:
      screen_to:
        - qa-team@company.com
      sample_rate: 0.15  # Review 15% of messages
      retention_days: 90
      add_header: false
```

**Result:**
- 15% of support emails randomly monitored
- QA can review customer interactions
- No impact on customer experience

---

### Scenario 8: Escalation Monitoring

**Requirement:** VP must see all escalated customer issues.

```yaml
# screen.yaml
screen_rules:
  - name: "Escalation Monitoring"
    enabled: true
    match:
      type: content
      keywords:
        - "escalation"
        - "urgent"
        - "complaint"
        - "cancel account"
        - "lawsuit"
        - "attorney"
      case_insensitive: true
    action:
      screen_to:
        - vp-customer-success@company.com
        - customer-success-lead@company.com
      priority: high
      alert: true
```

---

## Security & Insider Threat

### Scenario 9: Departing Employee Monitoring

**Requirement:** Monitor employee who gave notice for data exfiltration.

```yaml
# screen.yaml
screen_rules:
  - name: "Departing Employee - Jane Smith"
    enabled: true
    match:
      type: user
      value: jane.smith@company.com
      direction: outbound
    action:
      screen_to:
        - security-operations@company.com
        - dlp-team@company.com
      encrypt: true
      priority: high
      retention_days: 365

  - name: "Data Exfiltration Keywords"
    enabled: true
    match:
      type: content
      keywords:
        - "personal email"
        - "dropbox"
        - "google drive"
        - "competitor"
        - "new job"
      case_insensitive: true
    action:
      screen_to:
        - security-alerts@company.com
      alert: true
      priority: high
```

---

### Scenario 10: Competitor Communications

**Requirement:** Monitor all communications with competitor domains.

```yaml
# screen.yaml
screen_rules:
  - name: "Competitor Contact Monitoring"
    enabled: true
    match:
      type: domain
      value: competitor1.com
    action:
      screen_to:
        - security@company.com
        - legal@company.com
      priority: high
      encrypt: true

  - name: "Competitor Contact Monitoring 2"
    enabled: true
    match:
      type: domain
      value: competitor2.com
    action:
      screen_to:
        - security@company.com
        - legal@company.com
      priority: high
      encrypt: true
```

---

## Department-Specific

### Scenario 11: Legal Department Archiving

**Requirement:** All legal department communications must be archived.

```yaml
# groups.yaml
groups:
  legal-dept:
    type: ldap
    ldap_query: "(&(ou=legal)(objectClass=user)(mail=*))"
    ldap_server: "ldap://ldap.company.com"

# screen.yaml
screen_rules:
  - name: "Legal Department Archive"
    enabled: true
    match:
      type: group
      value: legal-dept
      direction: both
    action:
      screen_to:
        - legal-archive@company.com
      encrypt: true
      retention_days: 3650  # 10 years
      add_header: false
```

---

### Scenario 12: M&A Project Monitoring

**Requirement:** Monitor all communications about confidential M&A project.

```yaml
# groups.yaml
groups:
  project-titan-team:
    type: static
    members:
      - ceo@company.com
      - cfo@company.com
      - m-and-a-lead@company.com
      - legal-m-and-a@company.com
    metadata:
      purpose: "Project Titan M&A team"
      confidentiality: "top-secret"

# screen.yaml
screen_rules:
  - name: "Project Titan Monitoring"
    enabled: true
    match:
      type: group
      value: project-titan-team
      direction: both
    action:
      screen_to:
        - board-chair@company.com
        - outside-counsel@lawfirm.com
      encrypt: true
      retention_days: 2555
      priority: high

  - name: "Project Titan Keywords"
    enabled: true
    match:
      type: content
      keywords:
        - "Project Titan"
        - "acquisition target"
        - "due diligence"
        - "merger agreement"
      case_insensitive: true
    action:
      screen_to:
        - board-chair@company.com
      alert: true
      priority: high
      encrypt: true
```

---

## Complex Multi-Rule Scenarios

### Scenario 13: Graduated Monitoring

**Requirement:** Different monitoring levels based on role.

```yaml
# groups.yaml
groups:
  executives:
    type: static
    members: [ceo@company.com, cfo@company.com]

  directors:
    type: static
    members: [director1@company.com, director2@company.com]

  managers:
    type: static
    members: [mgr1@company.com, mgr2@company.com]

# screen.yaml
screen_rules:
  # Level 1: Executives - High retention, board oversight
  - name: "Executive Level Monitoring"
    enabled: true
    match:
      type: group
      value: executives
      direction: both
    action:
      screen_to:
        - board-compliance@company.com
        - general-counsel@company.com
      encrypt: true
      retention_days: 3650
      priority: high

  # Level 2: Directors - Medium retention, VP oversight
  - name: "Director Level Monitoring"
    enabled: true
    match:
      type: group
      value: directors
      direction: both
    action:
      screen_to:
        - vp-compliance@company.com
      encrypt: true
      retention_days: 1825
      priority: normal

  # Level 3: Managers - Low retention, sampling
  - name: "Manager Level Monitoring"
    enabled: true
    match:
      type: group
      value: managers
      direction: outbound
    action:
      screen_to:
        - compliance@company.com
      sample_rate: 0.1  # 10% sampling
      retention_days: 365
```

---

### Scenario 14: Time-Based Routing

**Requirement:** Different handling for business hours vs after hours.

```yaml
# divert.yaml
divert_rules:
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
      reason: "Weekend on-call routing"

  - name: "After Hours Support Weekdays"
    enabled: true
    match:
      type: recipient
      value: support@company.com
      schedule:
        days: [monday, tuesday, wednesday, thursday, friday]
        hours: "18:00-08:00"  # After 6pm, before 8am
    action:
      divert_to: oncall@company.com
      reason: "After-hours on-call routing"
```

---

### Scenario 15: Combined Divert + Screen

**Requirement:** Divert to legal, but also notify security.

```yaml
# divert.yaml
divert_rules:
  - name: "Suspect Employee Divert"
    enabled: true
    match:
      type: recipient
      value: suspect@company.com
    action:
      divert_to: legal-investigation@company.com
      reason: "Active investigation"
      encrypt: true

# screen.yaml
screen_rules:
  - name: "Suspect Employee Screen Outbound"
    enabled: true
    match:
      type: sender
      value: suspect@company.com
    action:
      screen_to:
        - legal-investigation@company.com
        - security-operations@company.com
      encrypt: true
      priority: high
```

**Flow:**
1. Inbound to suspect → Diverted to legal (suspect never sees)
2. Outbound from suspect → Copied to legal and security (still sends)

---

## Testing Checklist

Before deploying any configuration:

```bash
# 1. Validate syntax
go run cmd/validate-config/main.go

# 2. Test specific scenarios
go run cmd/test-routing/main.go \
  --from alice@example.com \
  --to bob@company.com \
  --dry-run

# 3. Check audit logs
tail -f /var/log/mail/divert-audit.log
tail -f /var/log/mail/screen-audit.log

# 4. Monitor performance
# Ensure routing adds < 10ms overhead

# 5. Verify encryption
# Check screened/diverted messages are encrypted

# 6. Test failover
# Ensure original delivery works if routing fails
```

---

## Deployment Best Practices

1. **Start Disabled**: Deploy rules with `enabled: false`
2. **Test Thoroughly**: Use dry-run mode
3. **Gradual Rollout**: Enable one rule at a time
4. **Monitor Impact**: Watch performance and audit logs
5. **Document Changes**: Maintain change log
6. **Regular Review**: Audit rules quarterly
7. **Compliance Check**: Verify legal requirements
8. **Access Control**: Restrict who can modify rules
9. **Backup Configs**: Version control all YAML files
10. **Incident Response**: Plan for handling divert/screen failures

---

## See Also

- [Master Control System](MASTER_CONTROL.md)
- [Divert Proxy System](DIVERT_PROXY.md)
- [Screen Proxy System](SCREEN_PROXY.md)
- [Mail Groups](MAIL_GROUPS.md)
