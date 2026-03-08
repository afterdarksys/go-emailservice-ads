# Quick Start Guide

## Prerequisites

- Go 1.25.7 or later
- Basic understanding of SMTP and email systems

## Installation

```bash
# Clone the repository
cd /Users/ryan/development/go-emailservice-ads

# Download dependencies
go mod tidy

# Build binaries
go build -o bin/goemailservices ./cmd/goemailservices
go build -o bin/mailctl ./cmd/mailctl

# Verify installation
./bin/goemailservices --help
./bin/mailctl --help
```

## Running the Service

### 1. Start the Email Service

```bash
# Run with default config (creates config.yaml if not exists)
./bin/goemailservices --config config.yaml
```

**Default Configuration:**
- SMTP Port: 2525
- REST API: 8080
- gRPC API: 50051
- Domain: localhost
- Mode: test
- Log Level: debug

### 2. Verify Service is Running

```bash
# Check health
curl http://localhost:8080/health

# Or use the CLI
./bin/mailctl health

# Expected output:
# Health: ok
# Uptime: 5.234s
```

### 3. Send a Test Email

```bash
# Using telnet
telnet localhost 2525

EHLO test.local
MAIL FROM:<sender@example.com>
RCPT TO:<recipient@example.com>
DATA
Subject: Test Email
From: sender@example.com
To: recipient@example.com

This is a test message.
.
QUIT

# Or using swaks (SMTP test tool)
swaks --to recipient@example.com \
      --from sender@example.com \
      --server localhost:2525 \
      --body "Test message"
```

### 4. Check Queue Statistics

```bash
# View queue metrics
./bin/mailctl --api http://localhost:8080 \
              --username admin \
              --password changeme \
              queue stats

# Expected output:
# Queue Statistics:
# ─────────────────────────────────────
# TIER        ENQUEUED  PROCESSED  FAILED
# emergency   0         0          0
# msa         0         0          0
# int         1         1          0
# out         0         0          0
# bulk        0         0          0
#
# Storage Statistics:
#   Pending:    0
#   Processing: 0
#   DLQ:        0
#   Total:      0
```

### 5. List Pending Messages

```bash
# List all pending messages
./bin/mailctl --username admin --password changeme queue list

# List pending messages for specific tier
./bin/mailctl --username admin --password changeme queue list int
```

## Common Operations

### Managing Dead Letter Queue

```bash
# List failed messages
./bin/mailctl --username admin --password changeme dlq list

# Retry a failed message
./bin/mailctl --username admin --password changeme dlq retry <message-id>
```

### Message Inspection

```bash
# Get message details
./bin/mailctl --username admin --password changeme message get <message-id>

# Output shows full message metadata:
# id: abc-123-def
# message_id: abc-123-def
# from: sender@example.com
# to:
#   - recipient@example.com
# data: <base64-encoded>
# tier: int
# attempts: 0
# created_at: 2026-03-08T12:00:00Z
# status: pending
```

### Delete a Message

```bash
./bin/mailctl --username admin --password changeme message delete <message-id>
```

## Production Configuration

### Create Production Config

```yaml
# config.yaml
server:
  addr: ":25"
  domain: "mail.yourdomain.com"
  max_message_bytes: 52428800  # 50MB
  max_recipients: 100
  mode: "production"
  tls:
    cert: "/etc/mail/certs/fullchain.pem"
    key: "/etc/mail/certs/privkey.pem"

api:
  rest_addr: ":8080"
  grpc_addr: ":50051"

logging:
  level: "info"
```

### Run as Systemd Service

```ini
# /etc/systemd/system/goemailservices.service
[Unit]
Description=Go Email Service
After=network.target

[Service]
Type=simple
User=mail
Group=mail
WorkingDirectory=/opt/goemailservices
ExecStart=/opt/goemailservices/bin/goemailservices --config /etc/goemailservices/config.yaml
Restart=always
RestartSec=5

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/mail-storage

[Install]
WantedBy=multi-user.target
```

**Enable and start:**
```bash
sudo systemctl daemon-reload
sudo systemctl enable goemailservices
sudo systemctl start goemailservices
sudo systemctl status goemailservices
```

## Testing Disaster Recovery

### Test 1: Crash Recovery

```bash
# Start service
./bin/goemailservices --config config.yaml &
PID=$!

# Send test messages
for i in {1..10}; do
  echo "Sending message $i"
  swaks --to test$i@example.com --from sender@example.com --server localhost:2525
done

# Kill service (simulate crash)
kill -9 $PID

# Restart service
./bin/goemailservices --config config.yaml &

# Check messages were recovered
./bin/mailctl --username admin --password changeme queue stats

# You should see messages restored from journal
```

### Test 2: Deduplication

```bash
# Send same message twice
MESSAGE_ID=$(uuidgen)

swaks --to recipient@example.com \
      --from sender@example.com \
      --server localhost:2525 \
      --header "Message-ID: <$MESSAGE_ID>"

swaks --to recipient@example.com \
      --from sender@example.com \
      --server localhost:2525 \
      --header "Message-ID: <$MESSAGE_ID>"

# Check queue stats - duplicates should be counted
./bin/mailctl --username admin --password changeme queue stats
# Look for "Duplicates: 1"
```

### Test 3: Rate Limiting

```bash
# Send burst of messages to bulk tier
for i in {1..200}; do
  swaks --to bulk$i@example.com \
        --from newsletter@example.com \
        --server localhost:2525 &
done
wait

# Monitor processing rate (should be ~100/s for bulk tier)
watch -n 1 './bin/mailctl --username admin --password changeme queue stats'
```

## Performance Tuning

### Worker Pool Sizing

Edit `internal/smtpd/queue.go:66-72` to adjust worker counts:

```go
qm.spawnWorkers("emergency", qm.emergency, 50)   // High priority
qm.spawnWorkers("msa", qm.msa, 200)              // User submissions
qm.spawnWorkers("int", qm.intQ, 500)             // Internal (highest volume)
qm.spawnWorkers("out", qm.out, 200)              // Outbound delivery
qm.spawnWorkers("bulk", qm.bulk, 100)            // Bulk mail
```

**Guidelines:**
- More workers = higher concurrency but more memory
- Start conservative, increase if queues back up
- Monitor CPU and memory usage

### Rate Limiter Tuning

Edit `internal/smtpd/queue.go:55-62`:

```go
emergencyLimiter: rate.NewLimiter(rate.Inf, 0),        // Unlimited
msaLimiter:       rate.NewLimiter(1000, 2000),         // 1000/s, burst 2000
intLimiter:       rate.NewLimiter(5000, 10000),        // 5000/s, burst 10000
outLimiter:       rate.NewLimiter(500, 1000),          // 500/s, burst 1000
bulkLimiter:      rate.NewLimiter(100, 500),           // 100/s, burst 500
```

**Guidelines:**
- First parameter: sustained rate (messages/second)
- Second parameter: burst capacity
- Adjust based on downstream system capacity

### Channel Buffer Sizes

Edit `internal/smtpd/queue.go:53-57`:

```go
emergency: make(chan *Message, 10000),
msa:       make(chan *Message, 50000),
intQ:      make(chan *Message, 100000),
out:       make(chan *Message, 50000),
bulk:      make(chan *Message, 100000),
```

**Guidelines:**
- Larger buffers = more memory but better burst handling
- Buffer size should be >= worker_count * avg_message_processing_time
- Monitor buffer utilization under load

## Troubleshooting

### Service won't start

**Check logs:**
```bash
./bin/goemailservices --config config.yaml 2>&1 | tee service.log
```

**Common issues:**
- Port already in use: `lsof -i :2525`
- Permission denied: Run as root or use port > 1024
- Config file not found: Check path with `--config`

### Messages stuck in queue

**Symptoms:**
- High "Pending" count
- Low "Processed" count

**Diagnosis:**
```bash
# Check queue stats
./bin/mailctl queue stats

# List pending messages
./bin/mailctl queue list

# Check specific message
./bin/mailctl message get <message-id>
```

**Solutions:**
- Increase worker count
- Check delivery mechanism (not implemented yet - messages stay in queue)
- Review rate limiter settings

### High memory usage

**Check:**
```bash
# Monitor memory
ps aux | grep goemailservices

# Check queue depths
./bin/mailctl queue stats
```

**Solutions:**
- Reduce channel buffer sizes
- Reduce worker counts
- Enable journal rotation more frequently
- Clear delivered messages from storage

### API not responding

**Check:**
```bash
# Verify API is listening
curl -v http://localhost:8080/health

# Check for port conflicts
lsof -i :8080
```

**Solutions:**
- Check API address in config
- Verify no firewall blocking
- Check logs for API errors

## Next Steps

1. **Implement Delivery:** Add actual message delivery to external MTAs
2. **Add TLS:** Configure TLS certificates for production
3. **Set up Monitoring:** Integrate Prometheus/Grafana
4. **Configure Replication:** Set up secondary nodes for HA
5. **Tune Performance:** Adjust workers and rate limits for your workload
6. **Security Hardening:** Change default credentials, enable TLS

## Environment Variables

```bash
# API Configuration
export MAIL_API_URL=http://localhost:8080
export MAIL_USER=admin
export MAIL_PASS=changeme

# Use in CLI
./bin/mailctl --api $MAIL_API_URL \
              --username $MAIL_USER \
              --password $MAIL_PASS \
              queue stats
```

## Useful Aliases

Add to your `~/.bashrc` or `~/.zshrc`:

```bash
# Aliases for mailctl
alias mctl='./bin/mailctl --api http://localhost:8080 --username admin --password changeme'
alias mqstats='mctl queue stats'
alias mqlist='mctl queue list'
alias mdlq='mctl dlq list'

# Usage:
# mqstats
# mqlist int
# mdlq
```

## Additional Resources

- [Disaster Recovery Documentation](DISASTER_RECOVERY.md)
- [README](README)
- [Go SMTP Library](https://github.com/emersion/go-smtp)
- [Starlark (MailScript)](https://github.com/google/starlark-go)
