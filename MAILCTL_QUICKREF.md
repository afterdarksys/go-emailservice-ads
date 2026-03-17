# mailctl Quick Reference

## Setup

```bash
# Build tool
go build -o mailctl ./cmd/mailctl

# Set credentials (optional)
export API_URL="https://api.msgs.global"
export API_USER="admin"
export API_PASS="password"
```

## Quick Onboard meowmail.email

```bash
# One-liner with all credentials
alias mctl='./mailctl --api $API_URL -u $API_USER -p $API_PASS'

# Onboard domain
mctl domain add meowmail.email
mctl user add user@meowmail.email --password "Pass123!"
mctl user add admin@meowmail.email --password "Admin456!"
mctl alias add info@meowmail.email --target user@meowmail.email
mctl alias add support@meowmail.email --target admin@meowmail.email

# Verify
mctl domain info meowmail.email
mctl user list meowmail.email
mctl alias list meowmail.email
```

## Common Commands

### Domains
```bash
mctl domain add example.com              # Add domain
mctl domain list                         # List all domains
mctl domain info example.com             # Domain details
mctl domain delete example.com           # Remove domain
```

### Users
```bash
mctl user add user@example.com --password "secret"  # Add user
mctl user list                                      # List all users
mctl user list example.com                          # List domain users
mctl user info user@example.com                     # User details
mctl user passwd user@example.com --password "new"  # Change password
mctl user delete user@example.com                   # Remove user
```

### Aliases
```bash
mctl alias add info@example.com --target user@example.com  # Add alias
mctl alias list                                            # List all aliases
mctl alias list example.com                                # List domain aliases
mctl alias delete info@example.com                         # Remove alias
```

### Queue Management
```bash
mctl queue stats                         # Queue statistics
mctl queue list                          # Pending messages
mctl queue list emergency                # By tier
mctl dlq list                            # Dead letter queue
mctl dlq retry <msg-id>                  # Retry failed message
mctl message get <msg-id>                # Message details
mctl message delete <msg-id>             # Delete message
```

### Health & Status
```bash
mctl health                              # System health check
mctl replication status                  # Replication status
```

## Testing After Onboard

```bash
# Build test tool
go build -o mail-test ./cmd/mail-test

# Test SMTP
./mail-test smtp connect --host mail.msgs.global --port 587
./mail-test smtp auth -h mail.msgs.global -p 587 -u user@meowmail.email --password "Pass123!"
./mail-test smtp send -h mail.msgs.global -p 587 -u user@meowmail.email --password "Pass123!"

# Test IMAP
./mail-test imap connect --host imap.msgs.global
./mail-test imap auth -h imap.msgs.global -u user@meowmail.email --password "Pass123!"

# Full diagnostic
./mail-test diag full -h mail.msgs.global -u user@meowmail.email --password "Pass123!"
```

## Batch Operations

```bash
# Create multiple users
for user in alice bob charlie; do
  mctl user add $user@example.com --password "TempPass123!"
done

# Create standard aliases
for alias in info support sales; do
  mctl alias add $alias@example.com --target admin@example.com
done
```

## Global Flags

```bash
--api, -a         API URL (default: http://localhost:8080)
--username, -u    Admin username
--password, -p    Admin password
--insecure, -k    Skip TLS verification
```

## Examples

### Example 1: New Customer Domain
```bash
mctl domain add customer.com
mctl user add admin@customer.com --password "InitialPass123!"
mctl alias add postmaster@customer.com --target admin@customer.com
mctl alias add abuse@customer.com --target admin@customer.com
```

### Example 2: Add Team Members
```bash
DOMAIN="team.example.com"
mctl user add alice@$DOMAIN --password "AlicePass!"
mctl user add bob@$DOMAIN --password "BobPass!"
mctl user add charlie@$DOMAIN --password "CharliePass!"
```

### Example 3: Department Aliases
```bash
DOMAIN="company.com"
mctl alias add sales@$DOMAIN --target sales-team@$DOMAIN
mctl alias add support@$DOMAIN --target helpdesk@$DOMAIN
mctl alias add info@$DOMAIN --target contact@$DOMAIN
```

## Troubleshooting

```bash
# Check if domain exists
mctl domain list | grep meowmail.email

# Verify user
mctl user info user@meowmail.email

# Reset password
mctl user passwd user@meowmail.email --password "NewPass123!"

# Check queue issues
mctl queue stats
mctl dlq list

# Test connectivity
./mail-test smtp connect -h mail.msgs.global -v
```

## Tips

1. **Use aliases** to avoid typing credentials repeatedly:
   ```bash
   alias mctl='./mailctl --api https://api.msgs.global -u admin -p secret'
   ```

2. **Create shell script** for common onboarding:
   ```bash
   #!/bin/bash
   DOMAIN=$1
   mctl domain add $DOMAIN
   mctl user add admin@$DOMAIN --password "Admin123!"
   ```

3. **Test immediately** after onboarding:
   ```bash
   ./mail-test diag full -h mail.msgs.global -u user@$DOMAIN --password "Pass123!"
   ```

4. **Check DNS** before production:
   ```bash
   ./mail-test diag dns --host $DOMAIN
   ```

---

**Quick Start:** `./mailctl domain add meowmail.email && ./mailctl user add user@meowmail.email --password "Pass123!"`
