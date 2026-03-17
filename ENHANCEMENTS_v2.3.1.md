# Mail Server Enhancements v2.3.1

## Overview

This release adds extensive server tuning capabilities and a comprehensive protocol testing/debugging tool, bringing the platform's configurability on par with enterprise MTAs like Postfix.

## 1. Enhanced Server Configuration Options

### Granular Timeout Configuration

Added Postfix-style granular timeout settings for all SMTP operations:

```yaml
timeouts:
  connect: "30s"       # Connection timeout
  helo: "300s"         # HELO/EHLO command timeout
  mail: "300s"         # MAIL FROM timeout
  rcpt: "300s"         # RCPT TO timeout
  data_init: "120s"    # DATA command timeout
  data_block: "180s"   # Data transfer timeout per block
  data_done: "600s"    # Final "." timeout
  rset: "20s"          # RSET command timeout
  quit: "300s"         # QUIT command timeout
  starttls: "300s"     # STARTTLS negotiation timeout
  command: "300s"      # Generic command timeout
```

### Error Handling Controls

```yaml
soft_error_limit: 10              # Errors before logging warning
hard_error_limit: 20              # Errors before disconnecting
junk_command_limit: 100           # Unknown commands before disconnect
error_sleep_time: "1s"            # Delay after error
```

### Command Restrictions

```yaml
forbidden_commands:                # Commands to reject
  - "CONNECT"
  - "GET"
  - "POST"
helo_required: false               # Require HELO/EHLO before other commands
```

### Client Behavior Limits

```yaml
client_message_rate_limit: 0       # Messages per hour per client (0 = unlimited)
client_recipient_rate_limit: 0     # Recipients per hour per client
client_new_tls_session_rate: 0     # New TLS sessions per hour per client
recipient_overshoot_limit: 1000    # Allow RCPT beyond max_recipients before rejecting
```

### Proxy Protocol Support

For deployment behind HAProxy or nginx:

```yaml
proxy_protocol:
  enabled: false
  timeout: "5s"
  trusted_networks: []             # CIDR format, e.g., ["10.0.0.0/8"]
```

### Additional Restrictions

```yaml
delay_reject: true                 # Delay rejection until RCPT TO
delay_open_until_valid_rcpt: true  # Don't open queue file until valid RCPT
```

### Configurable SMTP Banner

```yaml
banner: "mail.example.com ESMTP Mail Service"
```

## 2. mail-test - Protocol Testing & Debugging Tool

A comprehensive CLI tool for testing and debugging mail protocols locally or remotely.

### Features

- **SMTP/ESMTP Testing**
  - Connection and greeting validation
  - EHLO capabilities detection
  - STARTTLS negotiation
  - Authentication (PLAIN, LOGIN, etc.)
  - Test message sending
  - Interactive SMTP sessions
  - Performance benchmarking

- **IMAP/IMAPS Testing**
  - Connection testing
  - Authentication validation
  - Mailbox operations
  - TLS verification

- **POP3 Testing**
  - Connection and greeting
  - Authentication
  - Mailbox statistics

- **Comprehensive Diagnostics**
  - DNS records (MX, SPF, DKIM, DMARC)
  - TLS/SSL configuration and certificate validation
  - Authentication method testing
  - Mail deliverability scoring
  - Blacklist checking

- **Remote Testing**
  - Test via REST API
  - Test via gRPC
  - API key authentication

### Installation

```bash
go build -o mail-test ./cmd/mail-test
```

### Usage Examples

#### Quick Server Health Check
```bash
mail-test diag full --host mail.example.com -u admin -p secret
```

#### Test SMTP Connection
```bash
mail-test smtp connect --host mail.example.com --port 25 -v
```

#### Test STARTTLS
```bash
mail-test smtp starttls --host mail.example.com --port 587 -v
```

#### Interactive SMTP Session
```bash
mail-test smtp interactive --host mail.example.com -v
```

#### Check DNS Records
```bash
mail-test diag dns --host example.com
```

#### Test TLS Configuration
```bash
mail-test diag tls --host mail.example.com
```

#### Remote Testing via API
```bash
mail-test --remote --api-url https://api.example.com \
  --api-key $API_KEY \
  smtp connect --host mail.customer.com
```

### Output

The tool provides colored, formatted output with clear status indicators:
- ✓ Success (green)
- ✗ Error (red)
- ⚠ Warning (yellow)
- Protocol conversations (when using `-v`)

### Documentation

See `cmd/mail-test/README.md` for comprehensive usage documentation.

## 3. Code Structure Improvements

### New Packages

- `internal/mailtest/` - Protocol testing library
  - `config.go` - Test configuration
  - `smtp.go` - SMTP testing functions
  - `imap.go` - IMAP testing functions
  - `pop3.go` - POP3 testing functions
  - `diag.go` - Diagnostic suite
  - `utils.go` - Formatting and output utilities

### Updated Config Structure

- `internal/config/config.go`
  - Added `TimeoutConfig` type
  - Added `ProxyProtocolConfig` type
  - Enhanced `Server` struct with new fields
  - Updated defaults to match Postfix conventions

## 4. Comparison with Postfix

Our configuration now includes equivalents for these Postfix parameters:

| Postfix Parameter | Our Equivalent |
|-------------------|----------------|
| `smtpd_banner` | `server.banner` |
| `smtpd_helo_timeout` | `server.timeouts.helo` |
| `smtpd_mail_timeout` | `server.timeouts.mail` |
| `smtpd_rcpt_timeout` | `server.timeouts.rcpt` |
| `smtpd_data_init_timeout` | `server.timeouts.data_init` |
| `smtpd_data_xfer_timeout` | `server.timeouts.data_block` |
| `smtpd_timeout` | `server.timeouts.command` |
| `smtpd_soft_error_limit` | `server.soft_error_limit` |
| `smtpd_hard_error_limit` | `server.hard_error_limit` |
| `smtpd_junk_command_limit` | `server.junk_command_limit` |
| `smtpd_error_sleep_time` | `server.error_sleep_time` |
| `smtpd_forbidden_commands` | `server.forbidden_commands` |
| `smtpd_helo_required` | `server.helo_required` |
| `smtpd_client_message_rate_limit` | `server.client_message_rate_limit` |
| `smtpd_client_recipient_rate_limit` | `server.client_recipient_rate_limit` |
| `smtpd_recipient_overshoot_limit` | `server.recipient_overshoot_limit` |
| `smtpd_delay_reject` | `server.delay_reject` |
| `smtpd_delay_open_until_valid_rcpt` | `server.delay_open_until_valid_rcpt` |
| `postfix_upstream_proxy_protocol` | `server.proxy_protocol.enabled` |
| `postfix_upstream_proxy_timeout` | `server.proxy_protocol.timeout` |

## 5. Benefits

### For Operators

- **Fine-tuned Performance**: Adjust timeouts for different network conditions
- **Enhanced Security**: Granular controls for rate limiting and command restrictions
- **Better Debugging**: Comprehensive testing tool for troubleshooting
- **Production Ready**: Enterprise-grade configuration options
- **Proxy Support**: Easy deployment behind load balancers

### For Developers

- **Testing Tool**: `mail-test` for development and CI/CD
- **Protocol Validation**: Verify server behavior locally or remotely
- **Documentation**: Extensive examples and use cases
- **API Integration**: Remote testing capabilities

### For Enterprise

- **Compliance**: Detailed control over mail handling
- **Monitoring**: Diagnostic capabilities for health checks
- **Flexibility**: Postfix-equivalent configuration
- **Integration**: API/gRPC remote testing

## 6. Backward Compatibility

All new configuration options have sensible defaults. Existing configurations will continue to work without modification. The new options are purely additive.

## 7. Future Enhancements

Potential additions based on Postfix features:

- Milter support (mail filter integration)
- Queue management controls (retry schedules, bounce lifetimes)
- Content filtering options
- Detailed recipient restrictions
- Header/body checks
- Address rewriting rules
- Virtual domain mapping

## 8. Testing

Build and test the new tools:

```bash
# Build the testing tool
go build ./cmd/mail-test

# Build with new config
go build ./internal/config/...

# Test SMTP server
./mail-test smtp connect --host localhost --port 2525 -v

# Full diagnostic
./mail-test diag full --host localhost -u testuser -p testpass123
```

## 9. Documentation Updates

- Added `cmd/mail-test/README.md` - Comprehensive tool documentation
- Updated `config.yaml` - Example configuration with all new options
- Updated Kubernetes configs with banner setting

## Release Notes

**Version**: 2.3.1
**Date**: 2026-03-17

### Added
- Granular timeout configuration for all SMTP operations
- Error handling controls (soft/hard limits, sleep times)
- Command restrictions and forbidden commands list
- Client behavior limits (message rate, recipient rate, TLS session rate)
- Proxy protocol support for HAProxy/nginx deployments
- SMTP banner configuration
- `mail-test` CLI tool for protocol testing and diagnostics
- SMTP, IMAP, and POP3 testing capabilities
- DNS diagnostic suite (MX, SPF, DKIM, DMARC)
- TLS/SSL configuration validator
- Mail deliverability checker
- Remote testing via API and gRPC

### Changed
- Enhanced configuration structure with Postfix-equivalent options
- Improved defaults following Postfix conventions

### Fixed
- N/A (new features only)

---

For questions or issues, see: https://github.com/afterdarksys/go-emailservice-ads
