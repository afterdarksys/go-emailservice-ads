# Policy Engine Design: Sieve + Starlark

## Overview
This document describes the architecture for adding two policy/scripting engines to the email service:
1. **Sieve** - RFC 5228 standard mail filtering language
2. **Starlark** - Python-like scripting for advanced email policies

## Architecture

### Component Structure
```
internal/policy/
├── engine.go              # Main policy engine interface
├── context.go             # Email context passed to policies
├── actions.go             # Policy action definitions
├── manager.go             # Policy management & loading
├── sieve/
│   ├── interpreter.go     # Sieve RFC 5228 interpreter
│   ├── extensions.go      # Sieve extensions support
│   └── tests.go           # Sieve test implementations
└── starlark/
    ├── engine.go          # Starlark execution engine
    ├── builtins.go        # Email-specific built-in functions
    ├── modules.go         # Available Starlark modules
    └── sandbox.go         # Security sandbox for scripts
```

### Policy Context
Every policy receives an `EmailContext` with:
```go
type EmailContext struct {
    // Envelope
    From         string
    To           []string
    RemoteIP     string
    EHLO         string

    // Message
    Headers      mail.Header
    Body         []byte
    Attachments  []Attachment

    // Security
    SPFResult    SPFResult
    DKIMResult   DKIMResult
    DMARCResult  DMARCResult
    ARC          ARCResult

    // Reputation
    RBLResults   []RBLResult
    IPReputation ReputationScore

    // Metadata
    MessageID    string
    Size         int64
    ReceivedAt   time.Time
}
```

### Policy Actions
Policies can return actions:
```go
type Action struct {
    Type     ActionType // ACCEPT, REJECT, DEFER, DISCARD, REDIRECT, FILEINTO, etc.
    Reason   string
    Target   string     // For REDIRECT, FILEINTO
    Headers  []Header   // Headers to add/modify
}
```

### Configuration Schema
```yaml
# policies.yaml
policies:
  # Global policies (apply to all mail)
  - name: "Global Anti-Spam"
    type: starlark
    enabled: true
    priority: 100
    script_path: /etc/mail/policies/antispam.star

  # User-specific policies
  - name: "CEO Vacation Responder"
    type: sieve
    enabled: true
    priority: 50
    scope:
      type: user
      users:
        - ceo@company.com
    script_path: /etc/mail/policies/ceo-vacation.sieve

  # Group policies
  - name: "Sales Team Filter"
    type: starlark
    enabled: true
    priority: 75
    scope:
      type: group
      groups:
        - sales-team
    script_path: /etc/mail/policies/sales-filter.star

  # Domain policies
  - name: "External Domain Quarantine"
    type: starlark
    enabled: true
    priority: 90
    scope:
      type: domain
      domains:
        - competitor.com
    script_path: /etc/mail/policies/quarantine.star

  # Special purpose (e.g., inbound vs outbound)
  - name: "Outbound DLP"
    type: starlark
    enabled: true
    priority: 80
    scope:
      type: direction
      direction: outbound
    script_path: /etc/mail/policies/dlp.star
```

## Sieve Implementation

### Features to Support
- RFC 5228: Base Sieve specification
- RFC 5229: Variables extension
- RFC 5230: Vacation extension
- RFC 5231: Relational extension
- RFC 5232: IMAP4 Flags extension
- RFC 5233: Subaddress extension
- RFC 5235: Spamtest/Virustest extensions
- RFC 5293: Editheader extension
- RFC 5429: Reject extension
- RFC 5435: Notification extension
- RFC 6134: Externally stored lists
- RFC 6558: MIME loops

### Example Sieve Script
```sieve
require ["fileinto", "reject", "envelope", "body"];

# Reject mail from specific domain
if address :domain :is "from" "spam.example.com" {
    reject "Mail from this domain is not accepted";
}

# File sales inquiries
if header :contains "subject" ["quote", "pricing", "sales"] {
    fileinto "INBOX.Sales";
}

# Vacation responder
if header :contains "subject" "urgent" {
    keep;
} else {
    vacation :days 7 :subject "Out of Office"
             "I am currently out of office until next week.";
}
```

### Go Library
Use: `github.com/emersion/go-sieve` or implement custom interpreter

## Starlark Implementation

### Exposed API
```python
# Email inspection
def has_header(name: str) -> bool
def get_header(name: str) -> str
def get_all_headers(name: str) -> list[str]
def get_body() -> str
def get_attachments() -> list[Attachment]

# Envelope
def get_from() -> str
def get_to() -> list[str]
def get_remote_ip() -> str

# Security checks
def check_spf() -> str  # "pass", "fail", "softfail", "neutral", "none"
def check_dkim() -> str
def check_dmarc() -> str
def check_rbl(server: str) -> bool
def get_ip_reputation() -> int  # 0-100

# Actions
def accept(reason: str = "")
def reject(reason: str)
def defer(reason: str, retry_after: int = 300)
def discard(reason: str = "")
def redirect(target: str)
def fileinto(folder: str)
def add_header(name: str, value: str)
def remove_header(name: str)

# Utilities
def match_pattern(text: str, pattern: str) -> bool  # Regex
def lookup_dns(domain: str, type: str) -> list[str]
def query_group(group_name: str) -> list[str]
def is_in_group(email: str, group: str) -> bool
def log(level: str, message: str)
```

### Example Starlark Script
```python
# Advanced anti-spam policy
def evaluate():
    # Check IP reputation
    if get_ip_reputation() < 30:
        reject("Low IP reputation")
        return

    # Check SPF, DKIM, DMARC
    spf = check_spf()
    dkim = check_dkim()
    dmarc = check_dmarc()

    if spf == "fail" and dkim == "fail":
        reject("SPF and DKIM verification failed")
        return

    # Check RBL
    for rbl in ["zen.spamhaus.org", "bl.spamcop.net"]:
        if check_rbl(rbl):
            defer("Listed in RBL: " + rbl, retry_after=3600)
            return

    # Content filtering
    body = get_body().lower()
    spam_keywords = ["viagra", "casino", "lottery", "nigerian prince"]

    score = 0
    for keyword in spam_keywords:
        if keyword in body:
            score += 1

    if score >= 2:
        fileinto("INBOX.Spam")
        add_header("X-Spam-Score", str(score))
        return

    # Check attachments
    attachments = get_attachments()
    for att in attachments:
        if att.extension in [".exe", ".scr", ".com", ".bat"]:
            reject("Dangerous attachment type: " + att.extension)
            return

    # Accept with modifications
    add_header("X-Scanned-By", "Starlark Policy Engine")
    accept()

# Entry point
evaluate()
```

### Go Library
Use: `go.starlark.net` (official Starlark implementation)

## Integration Points

### 1. SMTP Session Integration
Add policy check in `Session.Data()`:
```go
func (s *Session) Data(r io.Reader) error {
    // ... existing code ...

    // Run policy engines
    ctx := policy.NewEmailContext(s.msg, s.ip, s.ehlo, b)
    action, err := s.policyManager.Evaluate(ctx)
    if err != nil {
        s.logger.Error("Policy evaluation failed", zap.Error(err))
    }

    switch action.Type {
    case policy.ActionReject:
        return &smtp.SMTPError{
            Code:         550,
            EnhancedCode: smtp.EnhancedCode{5, 7, 1},
            Message:      action.Reason,
        }
    case policy.ActionDefer:
        return &smtp.SMTPError{
            Code:         451,
            EnhancedCode: smtp.EnhancedCode{4, 7, 1},
            Message:      action.Reason,
        }
    case policy.ActionRedirect:
        s.msg.To = []string{action.Target}
    case policy.ActionFileinto:
        s.msg.Folder = action.Target
    case policy.ActionAccept:
        // Continue normal processing
    }

    // ... existing queue code ...
}
```

### 2. Routing Pipeline Integration
Extend `internal/routing/pipeline.go`:
```go
func (p *Pipeline) Process(ctx context.Context, from, to string, data []byte) *RoutingDecision {
    // ... existing divert/screen checks ...

    // Run policy engines
    if p.policyManager != nil {
        policyCtx := policy.NewEmailContext(...)
        action, err := p.policyManager.Evaluate(policyCtx)
        decision.PolicyAction = action
    }

    return decision
}
```

### 3. Post-Queue Filtering
Add policy evaluation in queue processor for async filtering:
- Virus scanning
- Content analysis
- DLP checks
- Compliance archiving

## Execution Order

1. **Pre-SMTP Policies** (priority 0-25)
   - IP blocklists
   - Rate limiting
   - Connection limits

2. **SMTP Transaction Policies** (priority 26-50)
   - SPF/DKIM/DMARC
   - Greylisting
   - Sender validation

3. **Content Policies** (priority 51-75)
   - Spam filtering
   - Virus scanning
   - Content filtering

4. **User/Group Policies** (priority 76-100)
   - Vacation responders
   - Forwarding rules
   - Folder filing

5. **Post-Processing Policies** (priority 101+)
   - Archiving
   - Compliance
   - Reporting

## Security Considerations

### Sieve Security
- No file system access
- No network access
- Limited CPU/memory per script
- Timeout enforcement (5 seconds default)

### Starlark Security
- Sandboxed execution (no `open`, `import`, etc.)
- Limited built-ins only
- No Go interop beyond provided API
- Resource limits:
  - Max execution time: 10 seconds
  - Max memory: 128MB
  - Max script size: 1MB
- Deterministic execution (no randomness/time)

## Performance

### Caching
- Compiled Sieve scripts cached in memory
- Starlark bytecode cached
- Policy load time < 1ms
- Execution time budget: 100ms per message

### Metrics
Track:
- Policy execution time
- Policy hit rate
- Action distribution
- Error rate

## Admin Tools

### CLI Commands
```bash
# List policies
mailctl policy list

# Add policy
mailctl policy add --name "Spam Filter" --type starlark --scope user --user bob@company.com --script spam.star

# Test policy
mailctl policy test --policy "Spam Filter" --email test.eml

# Enable/disable
mailctl policy enable "Spam Filter"
mailctl policy disable "Spam Filter"

# Reload all policies
mailctl policy reload
```

### API Endpoints
```
GET    /api/v1/policies           # List all policies
POST   /api/v1/policies           # Create policy
GET    /api/v1/policies/:id       # Get policy details
PUT    /api/v1/policies/:id       # Update policy
DELETE /api/v1/policies/:id       # Delete policy
POST   /api/v1/policies/:id/test  # Test policy with sample email
POST   /api/v1/policies/reload    # Hot-reload all policies
```

## Migration Path

1. ✅ **Phase 1**: Design (this document)
2. **Phase 2**: Core framework
   - Policy engine interface
   - Email context
   - Action types
   - Policy manager
3. **Phase 3**: Sieve implementation
   - Interpreter integration
   - Test implementations
   - Extension support
4. **Phase 4**: Starlark implementation
   - Engine setup
   - Built-in functions
   - Sandbox configuration
5. **Phase 5**: Integration
   - SMTP hooks
   - Routing pipeline
   - Queue processing
6. **Phase 6**: Management
   - API endpoints
   - CLI commands
   - Hot reload
7. **Phase 7**: Testing & Documentation
   - Unit tests
   - Integration tests
   - Example policies
   - Admin guide

## Files to Create

```
internal/policy/
├── engine.go              # PolicyEngine interface
├── context.go             # EmailContext struct
├── actions.go             # Action types
├── manager.go             # PolicyManager
├── config.go              # Policy configuration
├── sieve/
│   ├── engine.go
│   ├── tests.go
│   └── extensions.go
├── starlark/
│   ├── engine.go
│   ├── builtins.go
│   └── sandbox.go
└── tests/
    ├── engine_test.go
    └── fixtures/

policies.yaml              # Policy configuration file
examples/
├── sieve/
│   ├── vacation.sieve
│   ├── spam-filter.sieve
│   └── forwarding.sieve
└── starlark/
    ├── antispam.star
    ├── dlp.star
    └── reputation.star
```
