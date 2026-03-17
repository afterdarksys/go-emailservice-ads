# mail-test - Mail Protocol Testing & Debugging Tool

A comprehensive testing and debugging tool for mail protocols including SMTP, ESMTP, IMAP, IMAPS, POP3, and AfterSMTP.

## Features

- **SMTP/ESMTP Testing**: Connection, EHLO, STARTTLS, AUTH, message sending, interactive sessions, benchmarking
- **IMAP/IMAPS Testing**: Connection, authentication, mailbox listing, message operations
- **POP3 Testing**: Connection, authentication, mailbox statistics
- **Diagnostic Suite**: DNS records (MX, SPF, DKIM, DMARC), TLS configuration, authentication, deliverability
- **Remote Testing**: Run tests remotely via API or gRPC
- **Interactive Mode**: Manual protocol exploration
- **Benchmarking**: Performance testing

## Installation

```bash
go build -o mail-test ./cmd/mail-test
```

## Usage

### SMTP Testing

#### Test Connection
```bash
mail-test smtp connect --host mail.example.com
```

#### Test EHLO and Capabilities
```bash
mail-test smtp ehlo --host mail.example.com --port 25 --verbose
```

#### Test STARTTLS
```bash
mail-test smtp starttls --host mail.example.com --port 587
```

#### Test Authentication
```bash
mail-test smtp auth --host mail.example.com --port 587 \
  --username user@example.com --password secret
```

#### Send Test Message
```bash
mail-test smtp send --host mail.example.com --port 587 \
  --username user@example.com --password secret
```

#### Interactive SMTP Session
```bash
mail-test smtp interactive --host mail.example.com --port 25 --verbose
```

#### Benchmark SMTP Performance
```bash
mail-test smtp benchmark --host mail.example.com
```

### IMAP Testing

#### Test Connection
```bash
mail-test imap connect --host imap.example.com --port 143
```

#### Test Authentication
```bash
mail-test imap auth --host imap.example.com \
  --username user@example.com --password secret
```

#### List Mailboxes
```bash
mail-test imap list --host imap.example.com \
  --username user@example.com --password secret
```

#### Select Mailbox
```bash
mail-test imap select INBOX --host imap.example.com \
  --username user@example.com --password secret
```

### POP3 Testing

#### Test Connection
```bash
mail-test pop3 connect --host pop.example.com --port 110
```

#### Test Authentication
```bash
mail-test pop3 auth --host pop.example.com \
  --username user@example.com --password secret
```

#### Get Statistics
```bash
mail-test pop3 stat --host pop.example.com \
  --username user@example.com --password secret
```

### Diagnostics

#### Full Diagnostic Suite
```bash
mail-test diag full --host mail.example.com \
  --username user@example.com --password secret
```

#### DNS Records Check
```bash
mail-test diag dns --host example.com
```

#### TLS Configuration Check
```bash
mail-test diag tls --host mail.example.com --port 25
```

#### Authentication Methods Check
```bash
mail-test diag auth --host mail.example.com \
  --username user@example.com --password secret
```

#### Deliverability Check
```bash
mail-test diag deliverability --host mail.example.com
```

### Remote Testing

Run tests remotely via API:
```bash
mail-test smtp connect --remote --api-url https://api.example.com \
  --api-key your-api-key --host mail.example.com
```

Run tests remotely via gRPC:
```bash
mail-test smtp connect --remote --grpc-addr api.example.com:50051 \
  --api-key your-api-key --host mail.example.com
```

## Global Flags

- `-h, --host`: Mail server hostname (default: localhost)
- `-p, --port`: Server port (auto-detected if not specified)
- `--tls`: Use implicit TLS (not STARTTLS)
- `-k, --insecure`: Skip TLS certificate verification
- `-t, --timeout`: Connection timeout in seconds (default: 30)
- `-v, --verbose`: Verbose output (show protocol conversation)
- `-o, --output`: Output format: text, json, yaml (default: text)
- `-u, --username`: Username for authentication
- `--password`: Password for authentication
- `--remote`: Use remote testing via API/gRPC
- `--api-url`: API URL for remote testing (default: http://localhost:8080)
- `--grpc-addr`: gRPC address for remote testing (default: localhost:50051)
- `--api-key`: API key for remote testing

## Examples

### Quick Health Check
```bash
# Full diagnostic of mail server
mail-test diag full --host mail.example.com -u admin -p secret

# Check DNS configuration
mail-test diag dns --host example.com

# Check TLS/SSL configuration
mail-test diag tls --host mail.example.com
```

### Troubleshooting Connection Issues
```bash
# Test basic connectivity
mail-test smtp connect --host mail.example.com -v

# Test STARTTLS
mail-test smtp starttls --host mail.example.com --port 587 -v

# Test with TLS from start (port 465)
mail-test smtp connect --host mail.example.com --port 465 --tls
```

### Authentication Debugging
```bash
# Test SMTP AUTH
mail-test smtp auth --host mail.example.com --port 587 \
  -u user@example.com -p secret -v

# Test all auth methods
mail-test diag auth --host mail.example.com -u user -p secret
```

### Interactive Protocol Exploration
```bash
# Start interactive SMTP session
mail-test smtp interactive --host mail.example.com -v

# Example commands:
#   EHLO client.example.com
#   STARTTLS
#   AUTH PLAIN
#   MAIL FROM:<sender@example.com>
#   RCPT TO:<recipient@example.com>
#   DATA
#   QUIT
```

### Production Deployment Testing
```bash
# Test from remote server via API
mail-test --remote --api-url https://api.mycompany.com \
  --api-key $API_KEY \
  diag full --host mail.customer.com -u test -p testpass
```

## Exit Codes

- `0`: Success
- `1`: Error or test failure

## Integration with CI/CD

```yaml
# .gitlab-ci.yml example
test-mail-server:
  stage: test
  script:
    - ./mail-test diag full --host $MAIL_HOST -u $MAIL_USER -p $MAIL_PASS
    - ./mail-test smtp send --host $MAIL_HOST -u $MAIL_USER -p $MAIL_PASS
  only:
    - main
```

## Tips

1. **Use verbose mode** (`-v`) when troubleshooting to see the actual protocol conversation
2. **Test TLS properly**: Port 25 usually requires STARTTLS, port 465 requires implicit TLS (`--tls`), port 587 can use either
3. **DNS matters**: Always run `diag dns` first to ensure MX, SPF, DKIM, and DMARC records are correct
4. **Certificate validation**: Use `--insecure` only for testing with self-signed certificates
5. **Remote testing**: When testing from a different network, use `--remote` to leverage the API/gRPC interface

## License

Part of the go-emailservice-ads project.
