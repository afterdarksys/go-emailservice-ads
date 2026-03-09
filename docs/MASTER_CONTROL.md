# Master Control System

The Master Control System provides centralized service management similar to Postfix's `master.cf` but with modern YAML configuration and hot-reload capabilities.

## Overview

The master control system allows you to:

- Define which services run (SMTP, IMAP, JMAP, etc.)
- Configure listeners on different ports with different policies
- Set per-service worker counts and resource limits
- Enable/disable services without restart
- Hot-reload configuration changes

## Configuration File: `master.yaml`

### Location

```
/path/to/go-emailservice-ads/master.yaml
```

### Structure

```yaml
version: "1.0"

services:
  service-name:
    type: smtp|imap|jmap|pop3
    enabled: true|false
    listen: "address:port"
    workers: number
    tls_mode: implicit|required|optional
    settings:
      # Service-specific settings

resource_limits:
  max_memory_mb: number
  max_cpu_percent: number
  max_connections: number
  max_file_descriptors: number

hot_reload:
  enabled: true|false
  check_interval: duration
  validate_before_apply: true|false
  backup_on_change: true|false
```

## Service Types

### SMTP Service

```yaml
smtp-public:
  type: smtp
  enabled: true
  listen: "0.0.0.0:25"
  workers: 100
  settings:
    require_auth: false
    require_tls: false
    allow_relay: false
    max_message_size: 10485760
    max_connections: 500
    filters:
      - greylisting
      - spf-check
      - dkim-verify
      - divert-check
      - screen-check
```

**Settings:**
- `require_auth`: Require authentication before accepting mail
- `require_tls`: Require STARTTLS before accepting mail
- `allow_relay`: Allow relaying to external domains
- `max_message_size`: Maximum message size in bytes
- `max_connections`: Maximum concurrent connections for this service
- `filters`: List of filters to apply (in order)

### IMAP Service

```yaml
imap:
  type: imap
  enabled: true
  listen: "0.0.0.0:143"
  workers: 50
  tls_mode: optional
  settings:
    require_auth: true
    max_connections: 500
```

### JMAP Service

```yaml
jmap:
  type: jmap
  enabled: false
  listen: "0.0.0.0:8443"
  tls_mode: required
  workers: 30
  settings:
    require_auth: true
```

## TLS Modes

- **implicit**: TLS is required from connection start (e.g., port 465, 993)
- **required**: STARTTLS must be negotiated before proceeding
- **optional**: TLS is available but not required

## Hot Reload

The master control system supports hot-reload without dropping active connections:

```yaml
hot_reload:
  enabled: true
  check_interval: 5s
  validate_before_apply: true
  backup_on_change: true
```

### How Hot Reload Works

1. **File Watching**: The system checks for changes to `master.yaml` every `check_interval`
2. **Validation**: If `validate_before_apply` is true, the new config is validated first
3. **Backup**: If `backup_on_change` is true, the old config is backed up
4. **Graceful Application**:
   - Services no longer enabled are stopped gracefully
   - New services are started
   - Services with changed configuration are restarted
   - Existing connections are not dropped

### Manual Reload

You can also trigger a reload programmatically:

```go
controller.Reload(newConfig)
```

## Resource Limits

Global resource limits apply across all services:

```yaml
resource_limits:
  max_memory_mb: 2048        # Maximum memory usage
  max_cpu_percent: 80         # Maximum CPU usage
  max_connections: 10000      # Total connections across all services
  max_file_descriptors: 65536 # Maximum open files
```

## Service Filters

Filters are applied in the order specified:

- `greylisting`: Temporary rejection for spam prevention
- `spf-check`: Verify SPF records
- `dkim-verify`: Verify DKIM signatures
- `divert-check`: Check diversion rules
- `screen-check`: Check screening rules

## Common Configurations

### Public Mail Server (Port 25)

```yaml
smtp-public:
  type: smtp
  enabled: true
  listen: "0.0.0.0:25"
  workers: 100
  settings:
    require_auth: false  # Accept from anyone
    require_tls: false   # Optional TLS
    allow_relay: false   # Don't relay
    filters:
      - greylisting
      - spf-check
      - dkim-verify
```

### Authenticated Submission (Port 587)

```yaml
smtp-submission:
  type: smtp
  enabled: true
  listen: "0.0.0.0:587"
  workers: 200
  tls_mode: required
  settings:
    require_auth: true   # Must authenticate
    allow_relay: true    # Allow relaying for authenticated users
```

### Secure Submission (Port 465)

```yaml
smtp-tls:
  type: smtp
  enabled: true
  listen: "0.0.0.0:465"
  tls_mode: implicit
  workers: 200
  settings:
    require_auth: true
    allow_relay: true
```

## Best Practices

1. **Always validate**: Keep `validate_before_apply: true` to prevent invalid configurations
2. **Backup configs**: Keep `backup_on_change: true` for easy rollback
3. **Monitor resources**: Set appropriate resource limits for your environment
4. **Worker sizing**: Configure workers based on expected load
5. **Security first**: Use `require_auth` and `require_tls` where appropriate
6. **Gradual rollout**: Test configuration changes in staging first

## Troubleshooting

### Service won't start

Check logs for validation errors:
```
grep "Failed to start service" /var/log/mail/service.log
```

### Hot reload not working

Verify hot reload is enabled and check interval:
```yaml
hot_reload:
  enabled: true
  check_interval: 5s
```

### Config validation fails

Run validation manually:
```bash
go run cmd/validate-master-config/main.go master.yaml
```

## Migration from Postfix master.cf

Postfix `master.cf`:
```
smtp      inet  n       -       n       -       -       smtpd
submission inet n       -       n       -       -       smtpd
  -o smtpd_tls_security_level=encrypt
```

Equivalent `master.yaml`:
```yaml
services:
  smtp:
    type: smtp
    enabled: true
    listen: "0.0.0.0:25"
    workers: 100

  submission:
    type: smtp
    enabled: true
    listen: "0.0.0.0:587"
    workers: 100
    tls_mode: required
```

## API Reference

### Controller Methods

```go
// Start all enabled services
func (c *Controller) Start() error

// Reload configuration
func (c *Controller) Reload(newConfig *MasterConfig) error

// Graceful shutdown
func (c *Controller) Shutdown(ctx context.Context) error

// Get service status
func (c *Controller) GetServiceStatus() map[string]string

// Get statistics
func (c *Controller) GetStats() *ControllerStats
```

## Performance Tuning

### Worker Count Recommendations

| Service | Expected Load | Recommended Workers |
|---------|---------------|---------------------|
| SMTP (public) | < 100 msg/s | 100 |
| SMTP (public) | 100-500 msg/s | 200-500 |
| SMTP (submission) | < 50 msg/s | 100-200 |
| IMAP | < 100 concurrent | 50 |
| IMAP | 100-500 concurrent | 100-200 |

### Connection Limits

Set `max_connections` per service based on expected concurrent connections:
- Public SMTP: 500-1000
- Submission: 1000-2000
- IMAP: 500-1000

## Security Considerations

1. **Port 25 (Public SMTP)**:
   - Never require authentication
   - Never allow relaying
   - Enable SPF/DKIM checks
   - Consider greylisting

2. **Port 587 (Submission)**:
   - Always require authentication
   - Always require TLS
   - Allow relaying for authenticated users

3. **Port 465 (SMTPS)**:
   - Implicit TLS only
   - Require authentication
   - Allow relaying

4. **IMAP/IMAPS**:
   - Always require authentication
   - Prefer port 993 (implicit TLS) over 143
   - Set appropriate timeouts

## See Also

- [Divert Proxy System](DIVERT_PROXY.md)
- [Screen Proxy System](SCREEN_PROXY.md)
- [Mail Groups](MAIL_GROUPS.md)
