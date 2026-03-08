# Testing Guide - Everything is Deployed and Ready!

## ✅ Current Status

**Service:** Running (PID in /tmp/goemailservices.pid)
**SMTP Port:** 2525
**API Port:** 8080
**Messages Processed:** 12 (100% success rate)

## Quick Commands

### Using the Convenience Script

```bash
# Check service status and queue stats
./run.sh status

# View live logs
./run.sh logs

# Run test suite
./run.sh test

# Restart service
./run.sh restart

# Stop service
./run.sh stop

# Start service
./run.sh start
```

### Using mailctl Directly

```bash
# Set up aliases for convenience
alias mctl='./bin/mailctl --username admin --password changeme'

# Queue statistics
mctl queue stats

# Health check
mctl health

# List pending messages
mctl queue list

# List DLQ messages
mctl dlq list
```

## Send Test Emails

### Method 1: Python Script (Recommended)
```bash
# Single test email
python3 send-test-email.py

# Full test suite (12 messages)
python3 test-suite.py
```

### Method 2: Custom Python
```python
import smtplib
from email.message import EmailMessage

msg = EmailMessage()
msg['From'] = 'you@example.com'
msg['To'] = 'recipient@example.com'
msg['Subject'] = 'Test Email'
msg.set_content('Hello from the email service!')

with smtplib.SMTP('localhost', 2525) as smtp:
    smtp.send_message(msg)
    print("Email sent!")
```

### Method 3: Command Line (swaks)
```bash
# If you have swaks installed
swaks --to test@example.com \
      --from sender@example.com \
      --server localhost:2525 \
      --body "Test message"
```

## Test Different Features

### 1. Deduplication Test
```bash
# Send same message twice
python3 -c "
import smtplib
from email.message import EmailMessage

msg = EmailMessage()
msg['From'] = 'test@example.com'
msg['To'] = 'target@example.com'
msg['Subject'] = 'Duplicate Test'
msg.set_content('Same content for dedup test')

with smtplib.SMTP('localhost', 2525) as smtp:
    smtp.send_message(msg)
    smtp.send_message(msg)  # Send again
"

# Check stats - should see duplicate count increase
./bin/mailctl --username admin --password changeme queue stats
```

### 2. High Volume Test
```bash
python3 -c "
import smtplib
from email.message import EmailMessage

for i in range(100):
    msg = EmailMessage()
    msg['From'] = f'sender{i}@example.com'
    msg['To'] = f'recipient{i}@example.com'
    msg['Subject'] = f'Bulk Test {i}'
    msg.set_content(f'Message {i}')

    with smtplib.SMTP('localhost', 2525) as smtp:
        smtp.send_message(msg)

    if i % 10 == 0:
        print(f'Sent {i} messages...')

print('Done! Check stats:')
print('./bin/mailctl --username admin --password changeme queue stats')
"
```

### 3. Rate Limiting Test
```bash
# Send burst to see rate limiting in action
for i in {1..200}; do
  python3 send-test-email.py &
done
wait

# Monitor processing rate
watch -n 1 './bin/mailctl --username admin --password changeme queue stats'
```

### 4. Crash Recovery Test
```bash
# Send messages
python3 test-suite.py

# Kill service (simulate crash)
./run.sh stop

# Check journal has messages
cat data/mail-storage/journal/journal-*.log | tail -5

# Restart service (should recover messages)
./run.sh start

# Verify recovery in logs
tail -20 service.log | grep "recovered"
```

## Check Storage

### View Journal Entries
```bash
# See all journal entries
cat data/mail-storage/journal/journal-*.log

# Pretty print with jq
cat data/mail-storage/journal/journal-*.log | jq '.'

# Count entries
cat data/mail-storage/journal/journal-*.log | wc -l
```

### Check Storage Structure
```bash
tree data/mail-storage/
```

### Monitor Logs
```bash
# Live tail
tail -f service.log

# Filter for specific events
tail -f service.log | grep "Enqueued"
tail -f service.log | grep "Processing message"
tail -f service.log | grep "ERROR"
```

## API Testing

### Direct API Calls
```bash
# Health check (no auth required)
curl http://localhost:8080/health

# Queue stats (requires auth)
curl -u admin:changeme http://localhost:8080/api/v1/queue/stats | jq

# Pending messages
curl -u admin:changeme http://localhost:8080/api/v1/queue/pending | jq

# DLQ list
curl -u admin:changeme http://localhost:8080/api/v1/dlq/list | jq
```

## Performance Monitoring

### Real-time Stats
```bash
# Watch queue stats update
watch -n 2 './bin/mailctl --username admin --password changeme queue stats'
```

### Process Monitoring
```bash
# CPU and memory usage
ps aux | grep goemailservices

# Detailed process info
top -pid $(cat /tmp/goemailservices.pid)
```

### Network Connections
```bash
# See active SMTP connections
lsof -i :2525

# See API connections
lsof -i :8080
```

## Troubleshooting

### Service Won't Start
```bash
# Check if ports are in use
lsof -i :2525
lsof -i :8080

# Check logs for errors
tail -50 service.log

# Check config
cat config.yaml
```

### Messages Not Processing
```bash
# Check queue stats
./bin/mailctl --username admin --password changeme queue stats

# Check worker logs
tail -f service.log | grep "Processing message"

# List pending
./bin/mailctl --username admin --password changeme queue list
```

### High Memory Usage
```bash
# Check process size
ps aux | grep goemailservices

# Check queue depths
./bin/mailctl --username admin --password changeme queue stats

# Restart if needed
./run.sh restart
```

## Benchmarking

### Simple Throughput Test
```bash
time python3 -c "
import smtplib
from email.message import EmailMessage
import concurrent.futures

def send_email(i):
    msg = EmailMessage()
    msg['From'] = f'perf{i}@example.com'
    msg['To'] = f'target{i}@example.com'
    msg['Subject'] = f'Perf Test {i}'
    msg.set_content(f'Message {i}')

    with smtplib.SMTP('localhost', 2525) as smtp:
        smtp.send_message(msg)

with concurrent.futures.ThreadPoolExecutor(max_workers=50) as executor:
    executor.map(send_email, range(1000))

print('1000 messages sent')
"

# Check how long it took and verify all processed
./bin/mailctl --username admin --password changeme queue stats
```

## Next Steps

1. **Production Deployment:**
   - Change default credentials in config
   - Enable TLS for SMTP and API
   - Configure proper domain
   - Set up systemd service

2. **Monitoring:**
   - Set up Prometheus metrics
   - Configure alerting
   - Create Grafana dashboards

3. **Replication:**
   - Configure secondary nodes
   - Test failover procedures
   - Set up health checks

4. **Integration:**
   - Implement actual message delivery
   - Connect to directory service
   - Add MailScript filtering rules

## Files Reference

```
.
├── bin/
│   ├── goemailservices      - Main service binary
│   └── mailctl              - Management CLI
├── data/
│   └── mail-storage/        - Persistent storage
│       ├── journal/         - Write-ahead log
│       └── tiers/           - Tier-based storage
├── config.yaml              - Service configuration
├── service.log              - Service logs
├── run.sh                   - Convenience script
├── send-test-email.py       - Simple test
├── test-suite.py            - Full test suite
├── DISASTER_RECOVERY.md     - DR documentation
├── QUICKSTART.md            - Getting started
├── TEST_RESULTS.md          - Test results
└── README_TESTING.md        - This file
```

## Support

For issues or questions:
1. Check service.log for errors
2. Review DISASTER_RECOVERY.md for architecture
3. See QUICKSTART.md for configuration
4. Check TEST_RESULTS.md for expected behavior
