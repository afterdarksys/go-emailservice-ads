# SMTP Banner and Status Code Configuration

## Overview

Complete guide to customizing SMTP banners, status codes, and HELO/EHLO-based access control.

## Banner Configuration

### Current Behavior

The SMTP banner is constructed from `config.yaml`:
```yaml
server:
  domain: "apps.afterdarksys.com"
```

This generates the banner:
```
220 apps.afterdarksys.com ESMTP Service Ready
```

### Enhanced Banner Configuration

Add custom banner support to `config.yaml`:

```yaml
server:
  addr: ":2525"
  domain: "apps.afterdarksys.com"

  # Banner configuration
  banner:
    greeting: "ESMTP Service Ready"  # Default: "ESMTP Service Ready"
    software: "go-emailservice-ads"  # Software name (optional)
    version: "v1.0.0"                # Version (optional)
    show_software: false             # Show software name/version in banner
    custom: null                     # Full custom banner (overrides all)
```

### Banner Output Examples

**Basic (default)**:
```
220 apps.afterdarksys.com ESMTP Service Ready
```

**With Software Info**:
```yaml
server:
  banner:
    show_software: true
    software: "go-emailservice-ads"
    version: "v1.0.0"
```
Output:
```
220 apps.afterdarksys.com ESMTP go-emailservice-ads v1.0.0 Service Ready
```

**Custom Banner**:
```yaml
server:
  banner:
    custom: "220 apps.afterdarksys.com AfterSMTP Next-Gen Mail Server Ready"
```
Output:
```
220 apps.afterdarksys.com AfterSMTP Next-Gen Mail Server Ready
```

**Security-Focused (Hide Details)**:
```yaml
server:
  banner:
    greeting: "ESMTP"
    show_software: false
```
Output:
```
220 apps.afterdarksys.com ESMTP
```

## SMTP Status Codes (RFC 5321)

### Standard Reply Codes

All SMTP responses must use proper status codes:

| Code | Class | Meaning | Example |
|------|-------|---------|---------|
| **2xx** | Success | Command completed successfully | 250 OK |
| **3xx** | Intermediate | Command accepted, more info needed | 354 Start mail input |
| **4xx** | Temporary Failure | Try again later | 450 Mailbox unavailable |
| **5xx** | Permanent Failure | Do not retry | 550 User not found |

### Enhanced Status Codes (RFC 3463)

Format: `X.Y.Z`
- **X** = Class (2=success, 4=temp fail, 5=perm fail)
- **Y** = Subject (0=other, 1=addressing, 2=mailbox, 3=system, 4=network, 5=protocol, 7=security)
- **Z** = Detail (specific error)

### Complete Status Code Reference

```yaml
status_codes:
  # Connection Establishment
  service_ready:
    code: 220
    enhanced: null
    message: "{domain} ESMTP Service Ready"

  service_closing:
    code: 221
    enhanced: "2.0.0"
    message: "Service closing transmission channel"

  # Success Responses
  ok:
    code: 250
    enhanced: "2.0.0"
    message: "OK"

  ok_queued:
    code: 250
    enhanced: "2.0.0"
    message: "OK: queued as {message_id}"

  ok_forwarded:
    code: 251
    enhanced: "2.1.5"
    message: "User not local; will forward to {forward_path}"

  cannot_verify:
    code: 252
    enhanced: "2.5.0"
    message: "Cannot VRFY user, but will accept message and attempt delivery"

  # Intermediate Responses
  start_mail_input:
    code: 354
    enhanced: null
    message: "Start mail input; end with <CRLF>.<CRLF>"

  # Temporary Failures (4xx)
  service_not_available:
    code: 421
    enhanced: "4.3.0"
    message: "Service not available, closing transmission channel"

  mailbox_busy:
    code: 450
    enhanced: "4.2.1"
    message: "Mailbox unavailable (busy)"

  temp_error:
    code: 451
    enhanced: "4.3.0"
    message: "Requested action aborted: local error in processing"

  insufficient_storage:
    code: 452
    enhanced: "4.3.1"
    message: "Insufficient system storage"

  temp_auth_failed:
    code: 454
    enhanced: "4.7.0"
    message: "Temporary authentication failure"

  rate_limit:
    code: 450
    enhanced: "4.7.1"
    message: "Rate limit exceeded, try again later"

  greylisted:
    code: 451
    enhanced: "4.7.1"
    message: "Greylisted, please try again later"

  # Permanent Failures (5xx)
  syntax_error:
    code: 500
    enhanced: "5.5.2"
    message: "Syntax error, command unrecognized"

  syntax_error_params:
    code: 501
    enhanced: "5.5.4"
    message: "Syntax error in parameters or arguments"

  command_not_implemented:
    code: 502
    enhanced: "5.5.1"
    message: "Command not implemented"

  bad_sequence:
    code: 503
    enhanced: "5.5.1"
    message: "Bad sequence of commands"

  param_not_implemented:
    code: 504
    enhanced: "5.5.4"
    message: "Command parameter not implemented"

  # Authentication (RFC 4954)
  auth_success:
    code: 235
    enhanced: "2.7.0"
    message: "Authentication successful"

  auth_continue:
    code: 334
    enhanced: null
    message: "{challenge}"  # Base64 challenge

  auth_required:
    code: 530
    enhanced: "5.7.0"
    message: "Authentication required"

  auth_failed:
    code: 535
    enhanced: "5.7.8"
    message: "Authentication credentials invalid"

  auth_weak:
    code: 534
    enhanced: "5.7.9"
    message: "Authentication mechanism is too weak"

  # Mailbox Errors
  mailbox_unavailable:
    code: 550
    enhanced: "5.2.1"
    message: "Mailbox unavailable"

  user_not_local:
    code: 551
    enhanced: "5.1.6"
    message: "User not local; please try {forward_path}"

  exceeded_storage:
    code: 552
    enhanced: "5.2.2"
    message: "Exceeded storage allocation"

  mailbox_name_invalid:
    code: 553
    enhanced: "5.1.3"
    message: "Mailbox name not allowed"

  transaction_failed:
    code: 554
    enhanced: "5.3.0"
    message: "Transaction failed"

  # Relay/Routing Errors
  relay_denied:
    code: 550
    enhanced: "5.7.1"
    message: "Relaying denied"

  no_valid_recipients:
    code: 554
    enhanced: "5.5.0"
    message: "No valid recipients"

  # Policy Errors
  policy_violation:
    code: 550
    enhanced: "5.7.1"
    message: "Policy violation: {reason}"

  spam_detected:
    code: 550
    enhanced: "5.7.1"
    message: "Message rejected as spam"

  virus_detected:
    code: 554
    enhanced: "5.7.0"
    message: "Message contains virus: {virus_name}"

  content_rejected:
    code: 550
    enhanced: "5.7.1"
    message: "Content rejected: {reason}"

  # TLS Errors (RFC 3207)
  tls_not_available:
    code: 454
    enhanced: "4.3.0"
    message: "TLS not available due to temporary reason"

  tls_required:
    code: 530
    enhanced: "5.7.0"
    message: "Must issue a STARTTLS command first"

  tls_already_active:
    code: 554
    enhanced: "5.5.1"
    message: "TLS already active"

  # Message Size Errors
  message_too_large:
    code: 552
    enhanced: "5.3.4"
    message: "Message size exceeds fixed maximum message size"

  line_too_long:
    code: 500
    enhanced: "5.5.2"
    message: "Line too long"

  # Recipient Errors
  too_many_recipients:
    code: 452
    enhanced: "4.5.3"
    message: "Too many recipients"

  recipient_rejected:
    code: 550
    enhanced: "5.1.1"
    message: "Recipient address rejected: {reason}"

  sender_rejected:
    code: 550
    enhanced: "5.1.8"
    message: "Sender address rejected: {reason}"
```

## HELO/EHLO-Based Access Control

### Configuration

Add HELO/EHLO hostname-based access lists:

```yaml
server:
  domain: "apps.afterdarksys.com"

  # HELO/EHLO Access Control
  helo_access:
    enabled: true

    # Action when hostname doesn't match rules
    default_action: "accept"  # accept, reject, tempfail, log

    # Require valid hostname format
    require_valid_hostname: true

    # Reject if HELO matches our local domain (spoofing attempt)
    reject_local_domain: true

    # Whitelist - Always allow these
    whitelist:
      - "trusted-relay.example.com"
      - "mail.partner.com"
      - "*.googlemail.com"  # Wildcard support
      - "*.outlook.com"

    # Blacklist - Always reject these
    blacklist:
      - "localhost"  # Reject bare "localhost"
      - "localhost.localdomain"
      - "[127.0.0.1]"  # Reject loopback addresses
      - "*.spammer.bad"

    # Per-hostname policy rules
    rules:
      # Trusted senders - Skip greylisting, SPF, DKIM checks
      - hostname_pattern: "mail.trustedcorp.com"
        action: "accept"
        apply_policies:
          greylisting: false
          spf_check: false
          dkim_verify: false
          rate_limit_exempt: true

      # Known bulk senders - Apply strict limits
      - hostname_pattern: "*.bulk-sender.com"
        action: "accept"
        apply_policies:
          rate_limit: 10  # Max 10 messages per hour
          require_dkim: true
          require_spf_pass: true

      # Suspicious patterns - Temp fail for investigation
      - hostname_pattern: "dynamic-*.isp.com"
        action: "tempfail"
        message: "Dynamic IPs must use authenticated submission port 587"

      # Known spam sources
      - hostname_pattern: "*.spam-source.bad"
        action: "reject"
        code: 550
        enhanced_code: "5.7.1"
        message: "Host rejected due to policy"
```

### Access List Actions

| Action | SMTP Code | Behavior |
|--------|-----------|----------|
| **accept** | - | Allow connection, apply policies |
| **reject** | 550 5.7.1 | Permanent rejection |
| **tempfail** | 450 4.7.1 | Temporary failure (try later) |
| **log** | - | Log and continue |

### Example HELO/EHLO Access Scenarios

#### Scenario 1: Trusted Partner

```yaml
rules:
  - hostname_pattern: "mail.partner.com"
    action: "accept"
    apply_policies:
      greylisting: false
      rate_limit_exempt: true
```

Connection:
```
C: EHLO mail.partner.com
S: 250-apps.afterdarksys.com
S: 250-8BITMIME
S: 250 STARTTLS
```

#### Scenario 2: Known Spammer

```yaml
blacklist:
  - "spammer.bad"
```

Connection:
```
C: EHLO spammer.bad
S: 550 5.7.1 Host rejected due to policy
```

#### Scenario 3: Dynamic IP

```yaml
rules:
  - hostname_pattern: "dhcp-*.residential.isp"
    action: "reject"
    message: "Residential IPs blocked, use port 587 with authentication"
```

Connection:
```
C: EHLO dhcp-123-456.residential.isp
S: 550 5.7.1 Residential IPs blocked, use port 587 with authentication
```

#### Scenario 4: Require DKIM for Bulk Senders

```yaml
rules:
  - hostname_pattern: "*.mailchimp.com"
    action: "accept"
    apply_policies:
      require_dkim: true
      require_spf_pass: true
```

## Implementation in Code

### Config Structure

```go
// internal/config/config.go

type ServerConfig struct {
    Addr            string        `yaml:"addr"`
    Domain          string        `yaml:"domain"`
    Banner          BannerConfig  `yaml:"banner"`
    HeloAccess      HeloAccessConfig `yaml:"helo_access"`
    // ... other fields ...
}

type BannerConfig struct {
    Greeting     string `yaml:"greeting"`      // "ESMTP Service Ready"
    Software     string `yaml:"software"`      // "go-emailservice-ads"
    Version      string `yaml:"version"`       // "v1.0.0"
    ShowSoftware bool   `yaml:"show_software"` // false
    Custom       string `yaml:"custom"`        // Full override
}

type HeloAccessConfig struct {
    Enabled              bool                `yaml:"enabled"`
    DefaultAction        string              `yaml:"default_action"` // accept, reject, tempfail, log
    RequireValidHostname bool                `yaml:"require_valid_hostname"`
    RejectLocalDomain    bool                `yaml:"reject_local_domain"`
    Whitelist            []string            `yaml:"whitelist"`
    Blacklist            []string            `yaml:"blacklist"`
    Rules                []HeloAccessRule    `yaml:"rules"`
}

type HeloAccessRule struct {
    HostnamePattern string            `yaml:"hostname_pattern"`
    Action          string            `yaml:"action"` // accept, reject, tempfail, log
    Code            int               `yaml:"code"`
    EnhancedCode    string            `yaml:"enhanced_code"`
    Message         string            `yaml:"message"`
    ApplyPolicies   PolicyOverrides   `yaml:"apply_policies"`
}

type PolicyOverrides struct {
    Greylisting      *bool `yaml:"greylisting"`       // nil = default, true/false = override
    SPFCheck         *bool `yaml:"spf_check"`
    DKIMVerify       *bool `yaml:"dkim_verify"`
    RateLimit        *int  `yaml:"rate_limit"`        // nil = default, value = override
    RateLimitExempt  bool  `yaml:"rate_limit_exempt"`
    RequireDKIM      bool  `yaml:"require_dkim"`
    RequireSPFPass   bool  `yaml:"require_spf_pass"`
}
```

### Banner Generation

```go
// internal/smtpd/banner.go

package smtpd

import (
    "fmt"
    "github.com/afterdarksys/go-emailservice-ads/internal/config"
)

func GenerateBanner(cfg *config.ServerConfig) string {
    // Custom banner overrides everything
    if cfg.Banner.Custom != "" {
        return cfg.Banner.Custom
    }

    // Build banner parts
    parts := []string{"220", cfg.Domain}

    if cfg.Banner.ShowSoftware && cfg.Banner.Software != "" {
        parts = append(parts, "ESMTP", cfg.Banner.Software)
        if cfg.Banner.Version != "" {
            parts = append(parts, cfg.Banner.Version)
        }
    } else {
        parts = append(parts, "ESMTP")
    }

    greeting := cfg.Banner.Greeting
    if greeting == "" {
        greeting = "Service Ready"
    }
    parts = append(parts, greeting)

    return strings.Join(parts, " ")
}
```

### HELO Access Control

```go
// internal/smtpd/helo_access.go

package smtpd

import (
    "path/filepath"
    "strings"
    "github.com/afterdarksys/go-emailservice-ads/internal/config"
)

type HeloAccessChecker struct {
    config *config.HeloAccessConfig
    logger *zap.Logger
}

func NewHeloAccessChecker(cfg *config.HeloAccessConfig, logger *zap.Logger) *HeloAccessChecker {
    return &HeloAccessChecker{
        config: cfg,
        logger: logger,
    }
}

type HeloAccessResult struct {
    Action          string  // accept, reject, tempfail
    Code            int     // SMTP code
    EnhancedCode    string  // Enhanced status code
    Message         string  // Response message
    PolicyOverrides *config.PolicyOverrides
}

func (h *HeloAccessChecker) CheckHostname(hostname string, localDomain string) *HeloAccessResult {
    if !h.config.Enabled {
        return &HeloAccessResult{Action: "accept"}
    }

    // Check if hostname is local domain (spoofing attempt)
    if h.config.RejectLocalDomain && hostname == localDomain {
        return &HeloAccessResult{
            Action:       "reject",
            Code:         550,
            EnhancedCode: "5.7.1",
            Message:      "Cannot HELO/EHLO as local domain",
        }
    }

    // Check whitelist
    if h.matchesList(hostname, h.config.Whitelist) {
        return &HeloAccessResult{Action: "accept"}
    }

    // Check blacklist
    if h.matchesList(hostname, h.config.Blacklist) {
        return &HeloAccessResult{
            Action:       "reject",
            Code:         550,
            EnhancedCode: "5.7.1",
            Message:      "Host rejected due to policy",
        }
    }

    // Check rules
    for _, rule := range h.config.Rules {
        if h.matchesPattern(hostname, rule.HostnamePattern) {
            result := &HeloAccessResult{
                Action:          rule.Action,
                PolicyOverrides: &rule.ApplyPolicies,
            }

            if rule.Code > 0 {
                result.Code = rule.Code
            } else if rule.Action == "reject" {
                result.Code = 550
            } else if rule.Action == "tempfail" {
                result.Code = 450
            }

            if rule.EnhancedCode != "" {
                result.EnhancedCode = rule.EnhancedCode
            } else if rule.Action == "reject" {
                result.EnhancedCode = "5.7.1"
            } else if rule.Action == "tempfail" {
                result.EnhancedCode = "4.7.1"
            }

            if rule.Message != "" {
                result.Message = rule.Message
            }

            return result
        }
    }

    // Default action
    return &HeloAccessResult{Action: h.config.DefaultAction}
}

func (h *HeloAccessChecker) matchesList(hostname string, list []string) bool {
    for _, pattern := range list {
        if h.matchesPattern(hostname, pattern) {
            return true
        }
    }
    return false
}

func (h *HeloAccessChecker) matchesPattern(hostname, pattern string) bool {
    // Exact match
    if hostname == pattern {
        return true
    }

    // Wildcard match
    matched, _ := filepath.Match(pattern, hostname)
    return matched
}
```

## Testing

### Test Banner Output

```bash
# Basic banner
echo "QUIT" | nc apps.afterdarksys.com 25
# Expected: 220 apps.afterdarksys.com ESMTP Service Ready

# With EHLO
echo -e "EHLO test.client.com\nQUIT" | nc apps.afterdarksys.com 25
```

### Test HELO Access Control

```bash
# Test trusted host
echo -e "EHLO mail.trustedcorp.com\nQUIT" | nc apps.afterdarksys.com 25
# Expected: 250-apps.afterdarksys.com

# Test blacklisted host
echo -e "EHLO spammer.bad\nQUIT" | nc apps.afterdarksys.com 25
# Expected: 550 5.7.1 Host rejected due to policy

# Test dynamic IP
echo -e "EHLO dhcp-123.residential.isp\nQUIT" | nc apps.afterdarksys.com 25
# Expected: 550 5.7.1 Residential IPs blocked...
```

## See Also

- [Starlark Filters](./STARLARK_FILTERS.md)
- [SMTP Configuration](../CONFIGURATION.md)
- [Access Control](./ACCESS_CONTROL.md)
- [RFC 5321](https://tools.ietf.org/html/rfc5321) - SMTP
- [RFC 3463](https://tools.ietf.org/html/rfc3463) - Enhanced Status Codes
- [RFC 4954](https://tools.ietf.org/html/rfc4954) - SMTP Authentication
