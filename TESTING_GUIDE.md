# Testing Guide - Production Features

This guide provides quick commands to test all the newly implemented production features.

## Prerequisites

1. Build the server:
```bash
cd /Users/ryan/development/go-emailservice-ads
go build -o bin/goemailservices ./cmd/goemailservices
```

2. Start the server:
```bash
./bin/goemailservices -config config.yaml
```

---

## 1. Testing Outbound Mail Delivery

### Send a Test Email via SMTP
```bash
# Using telnet
telnet localhost 2525
```

Then type:
```
EHLO test.local
AUTH PLAIN dGVzdHVzZXIAdGVzdHVzZXIAdGVzdHBhc3MxMjM=
MAIL FROM:<testuser@localhost.local>
RCPT TO:<recipient@gmail.com>
DATA
From: testuser@localhost.local
To: recipient@gmail.com
Subject: Test Outbound Delivery

This is a test of the outbound delivery system.
.
QUIT
```

### Verify MX Lookup
Check logs for MX record resolution:
```bash
tail -f service.log | grep "MX lookup"
```

### Verify STARTTLS
Check logs for TLS negotiation:
```bash
tail -f service.log | grep "STARTTLS"
```

---

## 2. Testing SPF Verification

### Test SPF Pass
```bash
# Send from an IP that matches the SPF record
telnet localhost 2525
```

```
EHLO test.local
MAIL FROM:<sender@gmail.com>
RCPT TO:<testuser@localhost.local>
DATA
Test
.
QUIT
```

### Verify SPF in Logs
```bash
tail -f service.log | grep "SPF"
```

Expected output:
- `SPF verification result="pass"` - Success
- `SPF verification result="fail"` - Rejected

---

## 3. Testing DKIM Verification

Send an email with DKIM signature and check logs:
```bash
tail -f service.log | grep "DKIM"
```

Expected output:
- `DKIM signature verified` - Success
- `DKIM verification failed` - Signature invalid

---

## 4. Testing DMARC Verification

DMARC automatically runs after SPF/DKIM checks:
```bash
tail -f service.log | grep "DMARC"
```

Expected output:
- `DMARC verification result="pass"` - Passed
- `DMARC verification policy="reject"` - Policy extracted

---

## 5. Testing Bounce Generation

### Trigger a Bounce (Invalid Recipient)
```bash
telnet localhost 2525
```

```
EHLO test.local
AUTH PLAIN dGVzdHVzZXIAdGVzdHVzZXIAdGVzdHBhc3MxMjM=
MAIL FROM:<testuser@localhost.local>
RCPT TO:<nonexistent@invalid-domain-that-doesnt-exist.com>
DATA
Test bounce
.
QUIT
```

### Check for Bounce Message
```bash
tail -f service.log | grep "Bounce"
```

Expected output:
- `Bounce message generated` - DSN created
- Check mail queue for bounce to original sender

---

## 6. Testing MAIL FROM Authorization

### Test Authorized Send
```bash
telnet localhost 2525
```

```
EHLO test.local
AUTH PLAIN dGVzdHVzZXIAdGVzdHVzZXIAdGVzdHBhc3MxMjM=
MAIL FROM:<testuser@localhost.local>
```
Expected: `250 OK`

### Test Unauthorized Send (Spoofing Attempt)
```bash
MAIL FROM:<admin@localhost.local>
```
Expected: `550 5.7.1 Not authorized to send from this address`

---

## 7. Testing Account Lockout Protection

### Trigger Account Lockout
Try 5 failed login attempts:
```bash
for i in {1..5}; do
  echo -e "EHLO test\nAUTH PLAIN dGVzdHVzZXIAdGVzdHVzZXIAd3JvbmdwYXNz\nQUIT" | telnet localhost 2525
  sleep 1
done
```

### Verify Lockout
6th attempt should fail with:
```
421 4.7.0 Too many failed attempts, try again later
```

### Check Lockout Stats
```bash
curl -u admin:changeme http://localhost:8080/api/v1/queue/stats
```

---

## 8. Testing Greylisting

### First Attempt (Should be Greylisted)
```bash
telnet localhost 2525
```

```
EHLO unknown-sender.com
MAIL FROM:<sender@unknown.com>
RCPT TO:<testuser@localhost.local>
```

Expected: `451 4.7.1 Greylisted, please retry in 5m0s`

### Second Attempt (After 5 Minutes - Should Pass)
Wait 5 minutes and retry the same command.

Expected: `250 OK` - Message accepted and auto-whitelisted

### Check Greylisting Stats
```bash
tail -f service.log | grep -i greylist
```

---

## 9. Testing DNS Caching

### Observe Cache Performance
```bash
# First lookup (cache miss)
tail -f service.log | grep "MX cache miss"

# Second lookup within 5 minutes (cache hit)
tail -f service.log | grep "MX cache hit"
```

### Check DNS Cache Stats
Query the API for cache statistics:
```bash
curl -u admin:changeme http://localhost:8080/api/v1/queue/stats
```

---

## 10. Testing Prometheus Metrics

### View All Metrics
```bash
curl http://localhost:8080/metrics
```

Expected output:
```
# HELP messages_received Total messages received
# TYPE messages_received counter
messages_received 42

# HELP messages_sent Total messages sent
# TYPE messages_sent counter
messages_sent 38

# HELP auth_failures Total failed authentications
# TYPE auth_failures counter
auth_failures 5
...
```

### Monitor Specific Metrics
```bash
# Messages received
curl -s http://localhost:8080/metrics | grep messages_received

# Queue depth
curl -s http://localhost:8080/metrics | grep queue_depth

# Authentication failures
curl -s http://localhost:8080/metrics | grep auth_failures
```

---

## 11. Testing Health Endpoints

### Health Check
```bash
curl http://localhost:8080/health
```

Expected:
```json
{
  "status": "ok",
  "uptime": "2h15m30s"
}
```

### Readiness Check
```bash
curl http://localhost:8080/ready
```

Expected:
```json
{
  "status": "ready",
  "checks": {
    "storage": true,
    "queue": true
  }
}
```

### Test Readiness Failure
Stop the storage backend and retry:
```
HTTP/1.1 503 Service Unavailable
{
  "status": "not_ready",
  "checks": {
    "storage": false,
    "queue": true
  }
}
```

---

## 12. Testing Queue Management API

### Get Queue Statistics
```bash
curl -u admin:changeme http://localhost:8080/api/v1/queue/stats
```

Expected:
```json
{
  "metrics": {
    "enqueued": {"emergency": 5, "msa": 100, ...},
    "processed": {"emergency": 4, "msa": 95, ...},
    "failed": {"emergency": 0, "msa": 2, ...},
    "duplicates": 3,
    "last_update": "2026-03-08T07:55:00Z"
  },
  "storage": {
    "pending": 10,
    "processing": 2,
    "dlq": 1,
    "total": 13
  }
}
```

### List Pending Messages
```bash
curl -u admin:changeme http://localhost:8080/api/v1/queue/pending
```

### List Dead Letter Queue
```bash
curl -u admin:changeme http://localhost:8080/api/v1/dlq/list
```

### Retry Failed Message
```bash
curl -X POST -u admin:changeme http://localhost:8080/api/v1/dlq/retry/MESSAGE_ID
```

---

## 13. Testing Connection Limits

### Test Max Connections
Open multiple simultaneous connections:
```bash
for i in {1..15}; do
  (telnet localhost 2525 &)
done
```

Expected: After 10 connections (default max_per_ip), additional connections may be rejected or queued.

### Verify in Logs
```bash
tail -f service.log | grep "connection"
```

---

## 14. Testing Rate Limiting

### Trigger Rate Limit
Send many messages rapidly from the same IP:
```bash
for i in {1..150}; do
  echo -e "EHLO test\nMAIL FROM:<test@test.com>\nRCPT TO:<user@test.com>\nDATA\nTest\n.\nQUIT" | telnet localhost 2525
done
```

Expected: After 100 messages/hour (default), additional messages may be rate-limited.

---

## Monitoring in Production

### Prometheus Setup
Add this scrape config to `prometheus.yml`:
```yaml
scrape_configs:
  - job_name: 'go-emailservice'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
    scrape_interval: 15s
```

### Grafana Dashboard Queries
```promql
# Message throughput
rate(messages_received[5m])

# Delivery success rate
rate(messages_delivered[5m]) / rate(messages_sent[5m])

# Authentication failure rate
rate(auth_failures[5m])

# Queue depth over time
queue_depth

# SPF/DKIM pass rates
rate(spf_pass[5m]) / (rate(spf_pass[5m]) + rate(spf_fail[5m]))
rate(dkim_pass[5m]) / (rate(dkim_pass[5m]) + rate(dkim_fail[5m]))
```

---

## Kubernetes Health Probes

### Liveness Probe
```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 30
```

### Readiness Probe
```yaml
readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
```

---

## Troubleshooting

### Check Logs
```bash
# Follow all logs
tail -f service.log

# Filter by component
tail -f service.log | grep "delivery"
tail -f service.log | grep "SPF"
tail -f service.log | grep "queue"

# Check errors only
tail -f service.log | grep "error"
tail -f service.log | grep "ERROR"
```

### Verify Configuration
```bash
# Test config loading
./bin/goemailservices -config config.yaml

# Check for config errors
echo $?  # Should be 0 if successful
```

### Debug Mode
Enable debug logging in `config.yaml`:
```yaml
logging:
  level: "debug"
```

---

## Performance Testing

### Load Test with vegeta
```bash
echo "GET http://localhost:8080/health" | vegeta attack -duration=30s -rate=100 | vegeta report
```

### SMTP Load Test
Use a tool like `smtp-source` (from Postfix):
```bash
smtp-source -s 10 -m 1000 -c -f sender@test.com -t recipient@test.com localhost:2525
```

---

## Success Criteria

✅ Outbound delivery: Messages successfully delivered to external MTAs
✅ SPF/DKIM/DMARC: Proper verification and policy enforcement
✅ Bounces: RFC 3464 compliant DSN generated and delivered
✅ Authorization: Spoofing attempts blocked
✅ Account lockout: Brute force attempts trigger lockout
✅ Greylisting: Spam reduced, legitimate mail retries successfully
✅ Metrics: Prometheus scraping works, data accurate
✅ Health checks: /health returns 200, /ready reflects component status
✅ API endpoints: All authenticated endpoints accessible
✅ Logs: Structured logging with appropriate levels

---

## Quick Test Script

```bash
#!/bin/bash
# quick-test.sh - Test all major features

echo "1. Testing health endpoint..."
curl -s http://localhost:8080/health | jq .

echo "2. Testing metrics endpoint..."
curl -s http://localhost:8080/metrics | head -20

echo "3. Testing SMTP connection..."
echo "QUIT" | telnet localhost 2525

echo "4. Testing authenticated API..."
curl -s -u admin:changeme http://localhost:8080/api/v1/queue/stats | jq .

echo "All basic tests complete!"
```

Run with:
```bash
chmod +x quick-test.sh
./quick-test.sh
```

---

## Summary

All production features can be tested using the commands above. Monitor the server logs (`service.log`) and metrics endpoint (`/metrics`) to verify proper operation.

For production deployment, ensure:
1. TLS certificates are properly configured
2. Firewall rules allow ports 25, 587, 993, 8080
3. DNS records (MX, SPF, DKIM, DMARC) are configured
4. Monitoring (Prometheus + Grafana) is set up
5. Backup strategy for message store is in place
6. Log aggregation is configured
7. Load testing has been performed

The system is now ready for production use! 🚀
