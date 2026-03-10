# Starlark-Based Mail Filtering

## Overview

Starlark is a Python-like configuration language designed for safety and determinism. It's used by Bazel and now for mail filtering in go-emailservice-ads.

**Why Starlark?**
- **Safe**: No file I/O, no network access, sandboxed execution
- **Deterministic**: Same input always produces same output
- **Familiar**: Python-like syntax
- **Fast**: Compiled bytecode execution
- **Auditable**: Easy to review for security

## Filter Execution Stages

```
SMTP Connection  →  HELO/EHLO  →  MAIL FROM  →  RCPT TO  →  DATA  →  Queue  →  Delivery
       │               │             │            │           │        │         │
       ▼               ▼             ▼            ▼           ▼        ▼         ▼
   connect.star    helo.star    mailfrom.star  rcptto.star  data.star queue.star delivery.star
```

## Filter Directory Structure

```
/etc/mail/filters/
├── connect.star          # Connection-level filtering
├── helo.star             # HELO/EHLO filtering
├── mailfrom.star         # Sender filtering
├── rcptto.star           # Recipient filtering
├── data.star             # Message content filtering
├── queue.star            # Pre-queue filtering
├── delivery.star         # Delivery-time filtering
├── lib/
│   ├── reputation.star   # Shared reputation functions
│   ├── dns.star          # DNS lookup functions
│   └── utils.star        # Utility functions
└── config.yaml           # Filter configuration
```

## Starlark Filter API

### Global Context

Every filter has access to these global objects:

```python
# Connection information
connection = {
    "remote_addr": "192.168.1.100",
    "remote_port": 54321,
    "local_addr": "10.0.0.1",
    "local_port": 25,
    "tls": False,
    "cipher": "",
    "tls_version": "",
}

# HELO/EHLO information
helo = {
    "hostname": "mail.example.com",
    "is_ehlo": True,
}

# Sender information
sender = {
    "email": "user@example.com",
    "domain": "example.com",
    "local": "user",
}

# Recipient information
recipient = {
    "email": "dest@msgs.global",
    "domain": "msgs.global",
    "local": "dest",
}

# Message information
message = {
    "from": "user@example.com",
    "to": ["dest@msgs.global"],
    "cc": [],
    "subject": "Test Subject",
    "size": 1024,
    "headers": {
        "Message-ID": "<abc123@example.com>",
        "Date": "Mon, 09 Mar 2026 10:00:00 -0700",
        # ... all headers ...
    },
    "body": "Message body text...",
    "has_attachments": False,
    "attachments": [],
}

# Server configuration
config = {
    "domain": "apps.afterdarksys.com",
    "local_domains": ["apps.afterdarksys.com", "afterdarksys.com"],
    "relay_domains": ["partner1.com", "partner2.com"],
}
```

### Built-in Functions

```python
# DNS Lookups
def dns_lookup(hostname, record_type):
    """
    Lookup DNS record.
    Args:
        hostname: Hostname to lookup
        record_type: "A", "AAAA", "MX", "TXT", "PTR"
    Returns:
        List of records or None
    """
    pass

def dns_reverse_lookup(ip):
    """Reverse DNS lookup"""
    pass

def check_dnsbl(ip, dnsbl_server):
    """
    Check if IP is listed in DNSBL.
    Args:
        ip: IP address to check
        dnsbl_server: DNSBL server (e.g., "zen.spamhaus.org")
    Returns:
        True if listed, False otherwise
    """
    pass

# SPF/DKIM/DMARC
def check_spf(ip, domain, sender):
    """
    Check SPF record.
    Returns: "pass", "fail", "softfail", "neutral", "none", "temperror", "permerror"
    """
    pass

def verify_dkim(message):
    """
    Verify DKIM signature.
    Returns: {"valid": True, "domain": "example.com", "selector": "default"}
    """
    pass

def check_dmarc(domain):
    """
    Check DMARC policy.
    Returns: {"policy": "reject", "pct": 100}
    """
    pass

# Reputation Checking
def get_reputation(email_or_ip):
    """
    Get sender reputation score (0.0 to 1.0).
    Returns: 0.0 = bad, 1.0 = excellent
    """
    pass

def get_sender_history(email, days=30):
    """
    Get sender history.
    Returns: {"messages_sent": 150, "bounces": 2, "complaints": 0}
    """
    pass

# Rate Limiting
def check_rate_limit(key, limit, window_seconds):
    """
    Check rate limit.
    Args:
        key: Unique identifier (e.g., sender email, IP)
        limit: Max requests in window
        window_seconds: Time window
    Returns: {"allowed": True, "current": 5, "limit": 100, "reset_at": 1234567890}
    """
    pass

# Content Analysis
def scan_attachment(attachment, engine="clamav"):
    """
    Scan attachment for viruses.
    Returns: {"clean": True, "engine": "clamav"}
    """
    pass

def classify_content(text):
    """
    ML-based content classification.
    Returns: {"category": "spam", "confidence": 0.95}
    """
    pass

# String Utilities
def matches_regex(text, pattern):
    """Check if text matches regex pattern"""
    pass

def contains_any(text, words):
    """Check if text contains any word from list"""
    pass

def extract_urls(text):
    """Extract all URLs from text"""
    pass

def extract_emails(text):
    """Extract all email addresses from text"""
    pass

# Actions
def accept():
    """Accept the message"""
    return {"action": "accept"}

def reject(code, message):
    """Reject with SMTP code"""
    return {"action": "reject", "code": code, "message": message}

def tempfail(code, message):
    """Temporary failure"""
    return {"action": "tempfail", "code": code, "message": message}

def discard():
    """Silently discard message"""
    return {"action": "discard"}

def quarantine(reason):
    """Move to quarantine"""
    return {"action": "quarantine", "reason": reason}

def add_header(name, value):
    """Add header to message"""
    return {"action": "add_header", "name": name, "value": value}

def remove_header(name):
    """Remove header from message"""
    return {"action": "remove_header", "name": name}

def rewrite_sender(new_sender):
    """Rewrite sender address"""
    return {"action": "rewrite_sender", "sender": new_sender}

def rewrite_recipient(old_recipient, new_recipient):
    """Rewrite recipient address"""
    return {"action": "rewrite_recipient", "old": old_recipient, "new": new_recipient}
```

## Example Filters

### 1. Connection Filter (`connect.star`)

```python
# /etc/mail/filters/connect.star
"""
Connection-level filtering
Runs when SMTP connection is established
"""

def filter(connection, config):
    """
    Filter function called for each connection.

    Args:
        connection: Connection info dict
        config: Server config dict

    Returns:
        Action dict or None (None = accept)
    """

    remote_ip = connection["remote_addr"]

    # Check DNSBL (Spamhaus)
    if check_dnsbl(remote_ip, "zen.spamhaus.org"):
        return reject(550, "5.7.1 IP listed in Spamhaus DNSBL")

    # Check DNSBL (SpamCop)
    if check_dnsbl(remote_ip, "bl.spamcop.net"):
        return reject(550, "5.7.1 IP listed in SpamCop DNSBL")

    # Require reverse DNS
    reverse = dns_reverse_lookup(remote_ip)
    if not reverse:
        return tempfail(450, "4.7.1 No reverse DNS record found")

    # Check reputation
    reputation = get_reputation(remote_ip)
    if reputation < 0.2:
        return reject(550, "5.7.1 Poor sender reputation")
    elif reputation < 0.5:
        # Low reputation = greylist
        return tempfail(451, "4.7.1 Greylisted due to low reputation")

    # Accept
    return None
```

### 2. HELO/EHLO Filter (`helo.star`)

```python
# /etc/mail/filters/helo.star
"""
HELO/EHLO hostname filtering
"""

def filter(connection, helo, config):
    hostname = helo["hostname"]
    remote_ip = connection["remote_addr"]

    # Reject if HELO matches local domain (spoofing)
    if hostname in config["local_domains"]:
        return reject(550, "5.7.1 Cannot HELO as local domain")

    # Reject localhost
    if hostname in ["localhost", "localhost.localdomain"]:
        return reject(550, "5.7.1 Invalid HELO hostname")

    # Reject bare IP addresses
    if matches_regex(hostname, r"^\d+\.\d+\.\d+\.\d+$"):
        return reject(550, "5.7.1 HELO with bare IP not allowed")

    # Check HELO matches reverse DNS
    reverse = dns_reverse_lookup(remote_ip)
    if reverse and hostname != reverse[0]:
        # Log mismatch but don't reject
        print("HELO mismatch:", hostname, "vs", reverse[0])

    return None
```

### 3. Sender Filter (`mailfrom.star`)

```python
# /etc/mail/filters/mailfrom.star
"""
MAIL FROM sender filtering
"""

load("lib/reputation.star", "check_sender_reputation")

def filter(connection, helo, sender, config):
    email = sender["email"]
    domain = sender["domain"]

    # Reject empty sender for regular mail (allow for bounces)
    if email == "" and not connection["tls"]:
        return reject(550, "5.7.1 Empty sender not allowed without TLS")

    # Check SPF
    spf_result = check_spf(connection["remote_addr"], domain, email)
    if spf_result == "fail":
        return reject(550, "5.7.1 SPF check failed")
    elif spf_result == "softfail":
        # Log but accept
        print("SPF softfail for", email)

    # Check sender reputation
    reputation = check_sender_reputation(email, domain)
    if reputation["score"] < 0.3:
        return reject(550, "5.7.1 Poor sender reputation")

    # Check sender history
    history = get_sender_history(email, days=30)
    if history["messages_sent"] > 10000 and history["complaints"] > 100:
        return reject(550, "5.7.1 Too many complaints")

    # Rate limiting
    rate = check_rate_limit(email, limit=100, window_seconds=3600)
    if not rate["allowed"]:
        return tempfail(450, "4.7.1 Rate limit exceeded")

    return None
```

### 4. Recipient Filter (`rcptto.star`)

```python
# /etc/mail/filters/rcptto.star
"""
RCPT TO recipient filtering
"""

def filter(connection, helo, sender, recipient, config):
    rcpt_email = recipient["email"]
    rcpt_domain = recipient["domain"]

    # Check if recipient domain is local or relay
    is_local = rcpt_domain in config["local_domains"]
    is_relay = rcpt_domain in config["relay_domains"]

    if not is_local and not is_relay:
        # Not local and not relay = reject
        if not connection["authenticated"]:
            return reject(550, "5.7.1 Relaying denied")

    # Check recipient exists (for local domains)
    if is_local:
        # TODO: Check LDAP/database
        pass

    # Per-recipient rate limiting
    rate = check_rate_limit(
        key="rcpt:" + rcpt_email,
        limit=1000,
        window_seconds=3600
    )
    if not rate["allowed"]:
        return tempfail(450, "4.2.1 Mailbox busy, try again later")

    return None
```

### 5. Data/Content Filter (`data.star`)

```python
# /etc/mail/filters/data.star
"""
Message content filtering
"""

def filter(connection, helo, sender, recipient, message, config):
    # Check message size
    if message["size"] > 50 * 1024 * 1024:  # 50MB
        return reject(552, "5.3.4 Message too large")

    # Check DKIM
    dkim_result = verify_dkim(message)
    if not dkim_result["valid"]:
        # Add header indicating failed DKIM
        add_header("X-DKIM-Status", "fail")

    # Scan attachments
    for attachment in message["attachments"]:
        scan_result = scan_attachment(attachment, engine="clamav")
        if not scan_result["clean"]:
            return reject(554, "5.7.0 Virus detected in attachment")

    # Content classification
    classification = classify_content(message["body"])
    if classification["category"] == "spam" and classification["confidence"] > 0.95:
        return reject(550, "5.7.1 Message classified as spam")

    # Check for suspicious URLs
    urls = extract_urls(message["body"])
    for url in urls:
        # Check URL reputation
        if is_phishing_url(url):
            return quarantine("Phishing URL detected")

    # Check for sensitive data (DLP)
    if contains_ssn(message["body"]):
        # Check if sending to external domain
        if recipient["domain"] not in config["local_domains"]:
            return reject(550, "5.7.1 Cannot send sensitive data externally")

    # Add headers
    add_header("X-Processed-By", config["domain"])
    add_header("X-Spam-Score", str(classification.get("spam_score", 0)))

    return None

def is_phishing_url(url):
    """Check if URL is known phishing site"""
    # Check against phishing database
    return False

def contains_ssn(text):
    """Check if text contains SSN pattern"""
    return matches_regex(text, r"\b\d{3}-\d{2}-\d{4}\b")
```

### 6. Complex Spam Filter

```python
# /etc/mail/filters/data.star
"""
Advanced spam filtering
"""

def filter(connection, helo, sender, recipient, message, config):
    score = 0
    reasons = []

    # SPF check
    spf = check_spf(connection["remote_addr"], sender["domain"], sender["email"])
    if spf == "fail":
        score += 5
        reasons.append("SPF_FAIL")

    # DKIM check
    dkim = verify_dkim(message)
    if not dkim["valid"]:
        score += 3
        reasons.append("DKIM_INVALID")

    # DMARC check
    dmarc = check_dmarc(sender["domain"])
    if dmarc["policy"] == "reject" and (spf != "pass" or not dkim["valid"]):
        score += 10
        reasons.append("DMARC_POLICY_VIOLATION")

    # Subject analysis
    subject = message.get("subject", "")
    spam_words = ["viagra", "casino", "winner", "prize", "click here", "buy now"]
    if contains_any(subject.lower(), spam_words):
        score += 5
        reasons.append("SPAM_SUBJECT")

    # All caps subject
    if subject == subject.upper() and len(subject) > 10:
        score += 2
        reasons.append("ALL_CAPS_SUBJECT")

    # Excessive links
    urls = extract_urls(message["body"])
    if len(urls) > 10:
        score += 3
        reasons.append("EXCESSIVE_URLS")

    # Short message with links (likely spam)
    if len(message["body"]) < 100 and len(urls) > 2:
        score += 5
        reasons.append("SHORT_MESSAGE_WITH_URLS")

    # Check sender reputation
    reputation = get_reputation(sender["email"])
    if reputation < 0.3:
        score += 10
        reasons.append("POOR_REPUTATION")

    # ML classification
    ml = classify_content(message["body"])
    if ml["category"] == "spam":
        score += ml["confidence"] * 10
        reasons.append("ML_SPAM_CLASSIFICATION")

    # Add spam score header
    add_header("X-Spam-Score", str(score))
    add_header("X-Spam-Reasons", ", ".join(reasons))

    # Decision
    if score >= 15:
        return reject(550, "5.7.1 Message rejected as spam (score: " + str(score) + ")")
    elif score >= 10:
        return quarantine("High spam score: " + str(score))
    elif score >= 5:
        add_header("X-Spam-Flag", "YES")
        # Move to Junk folder
        rewrite_recipient(recipient["email"], recipient["email"] + "+junk")

    return None
```

### 7. Custom Business Logic

```python
# /etc/mail/filters/delivery.star
"""
Delivery-time business logic
"""

def filter(connection, helo, sender, recipient, message, config):
    # Auto-forward invoices to accounting
    if "invoice" in message.get("subject", "").lower():
        if recipient["domain"] == "msgs.global":
            # Forward to accounting
            add_recipient("accounting@msgs.global")
            add_header("X-Auto-Forwarded", "Invoices")

    # Vacation auto-reply
    if is_on_vacation(recipient["email"]):
        send_auto_reply(
            to=sender["email"],
            subject="Out of Office",
            body=get_vacation_message(recipient["email"])
        )

    # Large attachment notification
    if message["has_attachments"] and message["size"] > 10 * 1024 * 1024:
        # Notify recipient about large attachment
        send_notification(
            to=recipient["email"],
            subject="Large attachment received",
            body="You have received a message with a large attachment (" +
                 str(message["size"] / 1024 / 1024) + " MB)"
        )

    return None

def is_on_vacation(email):
    """Check if user is on vacation"""
    # Check database or LDAP
    return False

def get_vacation_message(email):
    """Get user's vacation message"""
    return "I am currently out of office..."

def send_auto_reply(to, subject, body):
    """Send auto-reply message"""
    pass

def send_notification(to, subject, body):
    """Send notification email"""
    pass

def add_recipient(email):
    """Add recipient to message"""
    return {"action": "add_recipient", "email": email}
```

## Filter Configuration

```yaml
# /etc/mail/filters/config.yaml
filters:
  enabled: true

  # Filter execution stages
  stages:
    - name: "connect"
      script: "connect.star"
      timeout: 2000ms  # 2 seconds
      enabled: true

    - name: "helo"
      script: "helo.star"
      timeout: 1000ms
      enabled: true

    - name: "mailfrom"
      script: "mailfrom.star"
      timeout: 2000ms
      enabled: true

    - name: "rcptto"
      script: "rcptto.star"
      timeout: 1000ms
      enabled: true

    - name: "data"
      script: "data.star"
      timeout: 10000ms  # 10 seconds for content analysis
      enabled: true

    - name: "queue"
      script: "queue.star"
      timeout: 5000ms
      enabled: false

    - name: "delivery"
      script: "delivery.star"
      timeout: 5000ms
      enabled: true

  # Global settings
  max_recursion_depth: 1000
  max_execution_time: 30000ms  # 30 seconds total
  enable_debug: false
  log_all_executions: true
```

## Testing Filters

### Test Framework

```python
# test_filters.py
from starlark import Starlark

def test_spam_filter():
    # Load filter
    filter_code = open("filters/data.star").read()
    starlark = Starlark()
    starlark.load(filter_code)

    # Test case: spam message
    context = {
        "message": {
            "from": "spam@spammer.com",
            "to": ["victim@msgs.global"],
            "subject": "BUY VIAGRA NOW!!!",
            "body": "Click here: http://spam.bad/viagra",
            "size": 500,
        },
        "sender": {
            "email": "spam@spammer.com",
            "domain": "spammer.com",
        },
        # ... other context ...
    }

    result = starlark.call("filter", context)

    assert result["action"] == "reject"
    assert "spam" in result["message"].lower()
```

### Command-Line Testing

```bash
# Test filter against sample message
./bin/test-filter \
    --filter /etc/mail/filters/data.star \
    --stage data \
    --message sample-messages/spam.eml

# Output:
# Filter: data.star
# Result: reject
# Code: 550
# Message: 5.7.1 Message rejected as spam (score: 25)
# Execution time: 45ms
```

## Performance Considerations

1. **Execution Timeout**: Each filter has a timeout (default 2s)
2. **Resource Limits**: CPU and memory limits enforced
3. **Caching**: DNS lookups and reputation checks cached
4. **Parallel Execution**: Filters can run in parallel when safe

## Security

1. **Sandboxed**: No file system or network access
2. **No Infinite Loops**: Recursion and iteration limits
3. **Type Safety**: Starlark is strongly typed
4. **Auditable**: All filter changes logged

## See Also

- [Starlark Language Spec](https://github.com/bazelbuild/starlark/blob/master/spec.md)
- [SMTP Banner Configuration](./SMTP_BANNER_AND_STATUS.md)
- [Access Control](./ACCESS_CONTROL.md)
- [Policy Engine](../internal/policy/README.md)
