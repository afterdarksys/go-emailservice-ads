# Domain Onboarding Guide for msgs.global Platform

Complete guide for onboarding new domains (e.g., `meowmail.email`) to the msgs.global mail platform for full-stack testing.

## Overview

The `mailctl` CLI tool provides comprehensive domain, user, and alias management for the msgs.global platform. This guide walks through onboarding a new domain from scratch.

## Prerequisites

```bash
# Build mailctl
go build -o mailctl ./cmd/mailctl

# Set API credentials (if required)
export MAILCTL_API=https://api.msgs.global
export MAILCTL_USER=admin
export MAILCTL_PASS=your-admin-password
```

## Quick Start: Onboard meowmail.email

### Step 1: Add the Domain

```bash
# Add domain to the platform
./mailctl domain add meowmail.email \
  --api $MAILCTL_API \
  --username $MAILCTL_USER \
  --password $MAILCTL_PASS

# Verify domain was added
./mailctl domain list --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS
```

Expected output:
```
✓ Domain added: meowmail.email

DOMAIN              STATUS    USERS    CREATED
meowmail.email      active    0        2026-03-17T12:00:00Z
```

### Step 2: Add Users

```bash
# Add primary user
./mailctl user add user@meowmail.email \
  --password "SecurePassword123!" \
  --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS

# Add additional users
./mailctl user add admin@meowmail.email --password "AdminPass456!" \
  --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS

./mailctl user add support@meowmail.email --password "SupportPass789!" \
  --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS

# List users for the domain
./mailctl user list meowmail.email --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS
```

Expected output:
```
✓ User added: user@meowmail.email
✓ User added: admin@meowmail.email
✓ User added: support@meowmail.email

EMAIL                    STATUS    QUOTA       CREATED
user@meowmail.email      active    5GB         2026-03-17T12:01:00Z
admin@meowmail.email     active    10GB        2026-03-17T12:01:15Z
support@meowmail.email   active    5GB         2026-03-17T12:01:30Z
```

### Step 3: Configure Email Aliases

```bash
# Add common aliases
./mailctl alias add info@meowmail.email --target support@meowmail.email \
  --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS

./mailctl alias add contact@meowmail.email --target support@meowmail.email \
  --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS

./mailctl alias add postmaster@meowmail.email --target admin@meowmail.email \
  --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS

./mailctl alias add abuse@meowmail.email --target admin@meowmail.email \
  --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS

# List aliases
./mailctl alias list meowmail.email --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS
```

Expected output:
```
✓ Alias added: info@meowmail.email -> support@meowmail.email
✓ Alias added: contact@meowmail.email -> support@meowmail.email
✓ Alias added: postmaster@meowmail.email -> admin@meowmail.email
✓ Alias added: abuse@meowmail.email -> admin@meowmail.email

SOURCE                         TARGET                      CREATED
info@meowmail.email           support@meowmail.email      2026-03-17T12:02:00Z
contact@meowmail.email        support@meowmail.email      2026-03-17T12:02:05Z
postmaster@meowmail.email     admin@meowmail.email        2026-03-17T12:02:10Z
abuse@meowmail.email          admin@meowmail.email        2026-03-17T12:02:15Z
```

### Step 4: Verify Domain Configuration

```bash
# Get detailed domain information
./mailctl domain info meowmail.email \
  --api $MAILCTL_API -u $MAILCTL_USER -p $MAILCTL_PASS
```

Expected output (YAML):
```yaml
domain: meowmail.email
status: active
user_count: 3
alias_count: 4
created_at: "2026-03-17T12:00:00Z"
updated_at: "2026-03-17T12:02:15Z"
settings:
  max_mailbox_size: 10GB
  max_message_size: 50MB
  spam_filter: enabled
  virus_scan: enabled
dns_verified: true
mx_records:
  - priority: 10
    host: mx1.msgs.global
  - priority: 20
    host: mx2.msgs.global
```

### Step 5: Test Mail Delivery

Using the `mail-test` tool:

```bash
# Test SMTP connection
./mail-test smtp connect --host mail.msgs.global --port 587 -v

# Test authentication
./mail-test smtp auth \
  --host mail.msgs.global --port 587 \
  --username user@meowmail.email \
  --password "SecurePassword123!"

# Send test message
./mail-test smtp send \
  --host mail.msgs.global --port 587 \
  --username user@meowmail.email \
  --password "SecurePassword123!"

# Test IMAP
./mail-test imap auth \
  --host imap.msgs.global \
  --username user@meowmail.email \
  --password "SecurePassword123!"
```

## Complete CLI Reference

### Domain Management

```bash
# Add domain
mailctl domain add <domain>

# List all domains
mailctl domain list

# Get domain info
mailctl domain info <domain>

# Delete domain
mailctl domain delete <domain>
```

### User Management

```bash
# Add user
mailctl user add <email> --password <password>

# List users (all or by domain)
mailctl user list
mailctl user list <domain>

# Get user info
mailctl user info <email>

# Change password
mailctl user passwd <email> --password <new-password>

# Delete user
mailctl user delete <email>
```

### Alias Management

```bash
# Add alias
mailctl alias add <source> --target <destination>

# List aliases (all or by domain)
mailctl alias list
mailctl alias list <domain>

# Delete alias
mailctl alias delete <source>
```

### Tenant Management (Multi-tenant Mode)

```bash
# Add tenant
mailctl tenant add <tenant-id> --name <name>

# List tenants
mailctl tenant list

# Delete tenant
mailctl tenant delete <tenant-id>
```

### Queue Management

```bash
# Show queue statistics
mailctl queue stats

# List pending messages
mailctl queue list [tier]

# Dead letter queue
mailctl dlq list
mailctl dlq retry <message-id>

# Message operations
mailctl message get <message-id>
mailctl message delete <message-id>
```

## Example: Full Stack Testing Setup

### Scenario: Set up meowmail.email for comprehensive testing

```bash
#!/bin/bash
set -e

# Configuration
DOMAIN="meowmail.email"
API_URL="https://api.msgs.global"
ADMIN_USER="admin"
ADMIN_PASS="admin-password"

# Function to run mailctl with credentials
mctl() {
  ./mailctl --api $API_URL -u $ADMIN_USER -p $ADMIN_PASS "$@"
}

echo "=== Onboarding $DOMAIN ==="

# 1. Add domain
echo "Adding domain..."
mctl domain add $DOMAIN

# 2. Create user accounts
echo "Creating users..."
mctl user add test@$DOMAIN --password "TestPass123!"
mctl user add admin@$DOMAIN --password "AdminPass456!"
mctl user add noreply@$DOMAIN --password "NoReplyPass789!"

# 3. Create aliases
echo "Creating aliases..."
mctl alias add info@$DOMAIN --target test@$DOMAIN
mctl alias add support@$DOMAIN --target admin@$DOMAIN
mctl alias add postmaster@$DOMAIN --target admin@$DOMAIN
mctl alias add abuse@$DOMAIN --target admin@$DOMAIN
mctl alias add hostmaster@$DOMAIN --target admin@$DOMAIN

# 4. Verify setup
echo ""
echo "=== Setup Complete ==="
echo ""
echo "Domain Information:"
mctl domain info $DOMAIN

echo ""
echo "Users:"
mctl user list $DOMAIN

echo ""
echo "Aliases:"
mctl alias list $DOMAIN

echo ""
echo "=== Ready for Testing ==="
echo "SMTP: mail.msgs.global:587 (STARTTLS)"
echo "IMAP: imap.msgs.global:143 (STARTTLS) or :993 (TLS)"
echo "Test account: test@$DOMAIN / TestPass123!"
```

Save as `onboard-domain.sh` and run:
```bash
chmod +x onboard-domain.sh
./onboard-domain.sh
```

## DNS Configuration

For production use, configure these DNS records:

```dns
; MX Records
meowmail.email.  3600  IN  MX  10  mx1.msgs.global.
meowmail.email.  3600  IN  MX  20  mx2.msgs.global.

; SPF Record
meowmail.email.  3600  IN  TXT  "v=spf1 include:msgs.global ~all"

; DMARC Record
_dmarc.meowmail.email.  3600  IN  TXT  "v=DMARC1; p=quarantine; rua=mailto:dmarc@meowmail.email"

; DKIM Record (get from msgs.global admin)
default._domainkey.meowmail.email.  3600  IN  TXT  "v=DKIM1; k=rsa; p=..."
```

Verify DNS with mail-test:
```bash
./mail-test diag dns --host meowmail.email
```

## Testing Checklist

After onboarding, verify:

- [ ] Domain is listed: `mailctl domain list`
- [ ] Users can authenticate: `mail-test smtp auth`
- [ ] Send test email: `mail-test smtp send`
- [ ] Receive test email: `mail-test imap auth`
- [ ] Aliases work correctly (send to alias, receive at target)
- [ ] DNS records are correct: `mail-test diag dns`
- [ ] TLS works: `mail-test diag tls`
- [ ] Deliverability check: `mail-test diag deliverability`

## Automation & CI/CD Integration

### GitLab CI Example

```yaml
onboard-test-domain:
  stage: setup
  script:
    - go build -o mailctl ./cmd/mailctl
    - ./mailctl domain add test-$CI_COMMIT_SHORT_SHA.example.com
    - ./mailctl user add testuser@test-$CI_COMMIT_SHORT_SHA.example.com --password testpass
  only:
    - branches
```

### GitHub Actions Example

```yaml
- name: Onboard Test Domain
  run: |
    go build -o mailctl ./cmd/mailctl
    ./mailctl --api ${{ secrets.API_URL }} \
      -u ${{ secrets.API_USER }} \
      -p ${{ secrets.API_PASS }} \
      domain add test-${{ github.sha }}.example.com
```

## Troubleshooting

### Domain Already Exists
```bash
# Check if domain exists
./mailctl domain list | grep meowmail.email

# View domain info
./mailctl domain info meowmail.email

# If needed, delete and re-add
./mailctl domain delete meowmail.email
./mailctl domain add meowmail.email
```

### User Authentication Fails
```bash
# Verify user exists
./mailctl user info user@meowmail.email

# Reset password
./mailctl user passwd user@meowmail.email --password "NewPassword123!"

# Test authentication
./mail-test smtp auth --host mail.msgs.global -u user@meowmail.email -p "NewPassword123!"
```

### Alias Not Working
```bash
# Check alias configuration
./mailctl alias list meowmail.email

# Verify target user exists
./mailctl user info support@meowmail.email

# Re-create alias if needed
./mailctl alias delete info@meowmail.email
./mailctl alias add info@meowmail.email --target support@meowmail.email
```

## API Endpoints

The `mailctl` tool uses these API endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/domains` | GET | List domains |
| `/api/v1/domains` | POST | Add domain |
| `/api/v1/domains/:domain` | GET | Get domain info |
| `/api/v1/domains/:domain` | DELETE | Delete domain |
| `/api/v1/users` | GET | List users |
| `/api/v1/users` | POST | Add user |
| `/api/v1/users/:email` | GET | Get user info |
| `/api/v1/users/:email` | DELETE | Delete user |
| `/api/v1/users/:email/password` | PUT | Update password |
| `/api/v1/aliases` | GET | List aliases |
| `/api/v1/aliases` | POST | Add alias |
| `/api/v1/aliases/:source` | DELETE | Delete alias |
| `/api/v1/tenants` | GET | List tenants |
| `/api/v1/tenants` | POST | Add tenant |
| `/api/v1/tenants/:id` | DELETE | Delete tenant |

## Production Deployment

For production domains:

1. **Configure DNS first** (MX, SPF, DKIM, DMARC)
2. **Onboard domain** using mailctl
3. **Create users** with strong passwords
4. **Set up aliases** for RFC-required addresses (postmaster, abuse, etc.)
5. **Test thoroughly** using mail-test
6. **Monitor** queue and delivery stats
7. **Enable logging** for troubleshooting

## Support

For issues or questions:
- Check logs: `mailctl queue stats`
- Test connectivity: `mail-test diag full`
- Review documentation: `./mailctl --help`
- GitHub Issues: https://github.com/afterdarksys/go-emailservice-ads/issues

---

**Ready to onboard your domain!** 🚀

Start with: `./mailctl domain add meowmail.email`
