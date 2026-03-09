# After Dark Systems SSO Setup Guide

## Overview

The email service now supports **Single Sign-On (SSO)** authentication with After Dark Systems for **@msgs.global** users.

## Features

- **OAuth 2.0 / OpenID Connect** integration
- **Directory Service** authentication for SMTP/IMAP
- **Fallback** to local authentication for non-SSO domains
- **Account lockout protection** still applies
- **Transparent** to end users

---

## Configuration

### 1. Enable SSO in config.yaml

The SSO configuration is already added to `config.yaml`:

```yaml
sso:
  enabled: true
  provider: "afterdarksystems"
  directory_url: "https://directory.msgs.global"
  auth_url: "https://sso.afterdarksystems.com/oauth2/authorize"
  token_url: "https://sso.afterdarksystems.com/oauth2/token"
  userinfo_url: "https://sso.afterdarksystems.com/oauth2/userinfo"
  client_id: "${ADS_CLIENT_ID}"
  client_secret: "${ADS_CLIENT_SECRET}"
  redirect_url: "https://msgs.global/oauth/callback"
  scopes:
    - "openid"
    - "email"
    - "profile"
```

### 2. Set Environment Variables

Create a `.env` file or export these variables:

```bash
export ADS_CLIENT_ID="your-client-id-here"
export ADS_CLIENT_SECRET="your-client-secret-here"
```

Or set them in your shell profile:

```bash
# ~/.bashrc or ~/.zshrc
export ADS_CLIENT_ID="..."
export ADS_CLIENT_SECRET="..."
```

### 3. Update Directory Service URL (if needed)

If the After Dark Systems directory service is hosted at a different URL, update:

```yaml
sso:
  directory_url: "https://your-directory-url.com"
```

---

## How It Works

### Authentication Flow

#### SMTP/IMAP Password Authentication

1. User connects to SMTP/IMAP with **username@msgs.global** and password
2. Server detects **@msgs.global** domain
3. Server calls **After Dark Systems directory service** at:
   ```
   POST https://directory.msgs.global/v1/auth/verify
   Content-Type: application/x-www-form-urlencoded

   email=user@msgs.global&password=secretpass
   ```
4. Directory service validates credentials and returns user info:
   ```json
   {
     "sub": "user-uuid",
     "email": "user@msgs.global",
     "email_verified": true,
     "name": "User Name",
     "roles": ["user"],
     "groups": []
   }
   ```
5. Server grants access if authentication succeeds

#### OAuth 2.0 Flow (Web/API)

1. Client requests authorization URL
2. User redirects to SSO provider
3. User authenticates with After Dark Systems
4. Callback returns authorization code
5. Server exchanges code for access token
6. Server calls UserInfo endpoint to get user profile
7. User is authenticated

---

## Testing SSO Authentication

### Test with SMTP

```bash
# Set up test user (replace with actual credentials)
export MSGS_EMAIL="testuser@msgs.global"
export MSGS_PASSWORD="your-actual-password"

# Test SMTP AUTH
telnet localhost 2525
EHLO test.local
AUTH PLAIN $(echo -ne "\0${MSGS_EMAIL}\0${MSGS_PASSWORD}" | base64)
# Should return: 235 2.7.0 Authentication successful
```

### Test with openssl s_client (if using TLS)

```bash
openssl s_client -connect localhost:2525 -starttls smtp
# After EHLO, use AUTH PLAIN with base64 encoded credentials
```

### Check Logs

```bash
# Start service with debug logging
./bin/goemailservices -config config.yaml

# Watch for SSO authentication messages:
# INFO  SSO authentication enabled  {"provider": "afterdarksystems", "directory_url": "https://directory.msgs.global"}
# INFO  SSO directory authentication successful  {"email": "user@msgs.global", "name": "User Name"}
```

---

## Directory Service API

The email service expects the following endpoints from the After Dark Systems directory service:

### POST /v1/auth/verify

**Request:**
```http
POST /v1/auth/verify HTTP/1.1
Host: directory.msgs.global
Content-Type: application/x-www-form-urlencoded
Accept: application/json

email=user@msgs.global&password=secretpass
```

**Success Response (200 OK):**
```json
{
  "sub": "unique-user-id",
  "email": "user@msgs.global",
  "email_verified": true,
  "name": "User Name",
  "preferred_username": "username",
  "roles": ["user", "admin"],
  "groups": ["engineering", "security"]
}
```

**Failure Responses:**
- **401 Unauthorized**: Invalid credentials
- **403 Forbidden**: Account locked/disabled
- **500 Internal Server Error**: Directory service error

---

## Troubleshooting

### "Authentication failed" Error

**Possible causes:**

1. **Directory service unavailable**
   - Check if `https://directory.msgs.global` is accessible
   - Check firewall rules
   - Check DNS resolution

2. **Invalid credentials**
   - Verify username is full email: `user@msgs.global` (not just `user`)
   - Verify password is correct
   - Check for account lockout in directory service

3. **SSO not enabled**
   - Verify `sso.enabled: true` in config.yaml
   - Restart the email service after config changes

4. **OAuth credentials not set**
   - Check `ADS_CLIENT_ID` and `ADS_CLIENT_SECRET` environment variables
   - Note: These are only needed for web-based OAuth flow, not SMTP AUTH

### Check SSO Status

```bash
# Verify SSO configuration loaded
./bin/goemailservices -config config.yaml | grep -i sso

# Expected output:
# INFO  SSO authentication enabled  {"provider": "afterdarksystems", ...}
```

### Test Directory Connectivity

```bash
# Test directory service endpoint
curl -v -X POST https://directory.msgs.global/v1/auth/verify \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -H "Accept: application/json" \
  -d "email=testuser@msgs.global&password=testpass"

# Should return 200 OK with user JSON or 401 Unauthorized
```

### Enable Debug Logging

```yaml
# config.yaml
logging:
  level: "debug"  # Change from "info" to "debug"
```

Then restart the service and watch logs:

```bash
./bin/goemailservices -config config.yaml 2>&1 | grep -E "(SSO|directory|auth)"
```

---

## Security Considerations

1. **TLS Required**: Always use TLS for SMTP/IMAP (already enforced)
2. **Secure Storage**: Client secrets should be in environment variables, not committed to git
3. **Rate Limiting**: Account lockout still applies to SSO users (5 failures = 15 min lockout)
4. **HTTPS Only**: Directory service must use HTTPS in production
5. **Token Expiration**: OAuth tokens should have short lifetimes (recommended: 1 hour)

---

## Fallback Behavior

The email service will:

1. **Try SSO first** for @msgs.global users
2. **Fall back to local auth** for:
   - Non-@msgs.global domains
   - When SSO is disabled
   - When directory service is unreachable (logs error, rejects auth)

Local users in `config.yaml` continue to work normally:

```yaml
auth:
  default_users:
    - username: "admin"
      password: "admin123"
      email: "admin@localhost.local"
```

---

## Integration Checklist

- [ ] SSO enabled in `config.yaml`
- [ ] Directory URL configured correctly
- [ ] Environment variables set (`ADS_CLIENT_ID`, `ADS_CLIENT_SECRET`)
- [ ] Directory service `/v1/auth/verify` endpoint responds correctly
- [ ] TLS certificates valid
- [ ] Firewall allows outbound HTTPS to directory service
- [ ] Test authentication with real @msgs.global account
- [ ] Monitor logs for SSO authentication success/failures

---

## Support

For issues with:
- **Email service SSO integration**: Check logs, verify config
- **After Dark Systems SSO**: Contact After Dark Systems support
- **Directory service errors**: Check directory service logs and API endpoints

---

**Status**: ✅ SSO Integration Complete

**Last Updated**: 2026-03-09
