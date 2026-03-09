# Operational Procedures for go-emailservice-ads

## Table of Contents
1. [Backup and Restore](#backup-and-restore)
2. [Monitoring Setup](#monitoring-setup)
3. [Incident Response](#incident-response)
4. [Capacity Planning](#capacity-planning)
5. [Maintenance Procedures](#maintenance-procedures)
6. [Disaster Recovery](#disaster-recovery)

---

## 1. Backup and Restore

### 1.1 What to Backup

The following data must be backed up regularly:

1. **Message Store**: `/var/lib/goemailservices/mail-storage/`
   - Contains all queued and stored messages
   - Journal files for crash recovery
   - Critical for message delivery

2. **Configuration**: `/etc/goemailservices/config.yaml`
   - Service configuration
   - User credentials (encrypt these!)

3. **TLS Certificates**: `/etc/goemailservices/certs/`
   - SSL/TLS certificates and keys
   - DKIM signing keys

### 1.2 Backup Schedule

| Component | Frequency | Retention | Method |
|-----------|-----------|-----------|--------|
| Message Store | Every 15 minutes | 7 days | Incremental |
| Message Store | Daily at 2 AM | 30 days | Full |
| Configuration | On change | 90 days | Version control |
| Certificates | On change | 1 year | Encrypted archive |

### 1.3 Backup Script

```bash
#!/bin/bash
# backup-email-system.sh

BACKUP_DIR="/backup/goemailservices"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
DATA_DIR="/var/lib/goemailservices"
CONFIG_DIR="/etc/goemailservices"

# Create backup directory
mkdir -p "$BACKUP_DIR/$TIMESTAMP"

# Backup message store (with compression)
echo "Backing up message store..."
tar -czf "$BACKUP_DIR/$TIMESTAMP/message-store.tar.gz" \
    -C "$DATA_DIR" mail-storage/

# Backup configuration
echo "Backing up configuration..."
cp -r "$CONFIG_DIR" "$BACKUP_DIR/$TIMESTAMP/config"

# Calculate checksums
cd "$BACKUP_DIR/$TIMESTAMP"
sha256sum * > checksums.txt

# Cleanup old backups (keep 30 days)
find "$BACKUP_DIR" -type d -mtime +30 -exec rm -rf {} +

echo "Backup completed: $BACKUP_DIR/$TIMESTAMP"
```

### 1.4 Restore Procedure

```bash
#!/bin/bash
# restore-email-system.sh

BACKUP_FILE=$1
DATA_DIR="/var/lib/goemailservices"

if [ -z "$BACKUP_FILE" ]; then
    echo "Usage: $0 <backup-tar-file>"
    exit 1
fi

# Stop service
systemctl stop goemailservices

# Restore data
echo "Restoring from $BACKUP_FILE..."
tar -xzf "$BACKUP_FILE" -C "$DATA_DIR"

# Verify permissions
chown -R mailservice:mailservice "$DATA_DIR"

# Start service
systemctl start goemailservices

echo "Restore completed"
```

### 1.5 Testing Backups

**Monthly Backup Test Procedure:**

1. Restore to a test environment
2. Start the service
3. Send a test email
4. Verify message delivery
5. Check all endpoints (API, SMTP, IMAP)
6. Document test results

---

## 2. Monitoring Setup

### 2.1 Key Metrics to Monitor

#### System Metrics
- **CPU Usage**: Target < 70% average
- **Memory Usage**: Target < 80%
- **Disk Usage**: Alert at 80%, critical at 90%
- **Network I/O**: Monitor bandwidth utilization

#### Application Metrics
- **Queue Depth**: Alert if > 10,000 messages
- **Active Connections**: Monitor against max_connections
- **Message Throughput**: Messages/second
- **Delivery Success Rate**: Target > 95%
- **Authentication Failures**: Alert on spikes
- **API Response Time**: Target < 100ms (p95)

#### Email-Specific Metrics
- **SPF Failures**: Monitor for spoofing attempts
- **DKIM Verification Failures**: Indicates tampering
- **Greylisting Rate**: Should be < 10% in steady state
- **Bounce Rate**: Target < 2%
- **DLQ Size**: Alert if > 100 messages

### 2.2 Prometheus Configuration

Create `/opt/goemailservices/prometheus/prometheus.yml`:

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'goemailservices'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
    scrape_interval: 10s

alerting:
  alertmanagers:
    - static_configs:
        - targets: ['localhost:9093']

rule_files:
  - 'alerts.yml'
```

### 2.3 Alert Rules

Create `/opt/goemailservices/prometheus/alerts.yml`:

```yaml
groups:
  - name: email_service_alerts
    interval: 30s
    rules:
      - alert: HighQueueDepth
        expr: queue_depth > 10000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High queue depth detected"
          description: "Queue depth is {{ $value }} messages"

      - alert: HighFailureRate
        expr: rate(messages_failed[5m]) / rate(messages_received[5m]) > 0.1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High message failure rate"
          description: "Failure rate is {{ $value | humanizePercentage }}"

      - alert: ServiceDown
        expr: up == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Email service is down"

      - alert: HighAuthFailures
        expr: rate(auth_failures[5m]) > 10
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "High authentication failure rate"
          description: "Possible brute force attack"

      - alert: DiskSpacelow
        expr: node_filesystem_avail_bytes{mountpoint="/var/lib/goemailservices"} / node_filesystem_size_bytes{mountpoint="/var/lib/goemailservices"} < 0.2
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Low disk space"
          description: "Only {{ $value | humanizePercentage }} disk space remaining"
```

### 2.4 Grafana Dashboards

Import the following dashboard JSON or create manually:

**Key Panels:**
1. Message Flow (sent, received, delivered, failed)
2. Queue Depth Over Time
3. Authentication Success/Failure Rate
4. API Response Times (p50, p95, p99)
5. SPF/DKIM Verification Results
6. System Resources (CPU, Memory, Disk)
7. Active Connections
8. Top Error Messages (from logs)

### 2.5 Log Aggregation

**Using journalctl for systemd:**
```bash
# View logs
journalctl -u goemailservices -f

# Filter by priority
journalctl -u goemailservices -p err

# Last hour
journalctl -u goemailservices --since "1 hour ago"
```

**Forwarding to Centralized Logging:**

For ELK Stack, add to systemd service:
```ini
[Service]
StandardOutput=journal
StandardError=journal
```

Then configure filebeat to ship journald logs.

---

## 3. Incident Response

### 3.1 Incident Severity Levels

| Level | Response Time | Description |
|-------|---------------|-------------|
| **P1 - Critical** | 15 minutes | Service completely down |
| **P2 - High** | 1 hour | Major functionality degraded |
| **P3 - Medium** | 4 hours | Minor functionality issue |
| **P4 - Low** | Next business day | Cosmetic or documentation |

### 3.2 Common Incidents and Responses

#### Incident: Service Won't Start

**Symptoms:**
- systemd shows failed status
- Errors in logs about port conflicts

**Response:**
1. Check port availability:
   ```bash
   netstat -tuln | grep -E ':(25|587|993|8080|50051)'
   lsof -i :25
   ```

2. Check configuration:
   ```bash
   /opt/goemailservices/bin/goemailservices --config /etc/goemailservices/config.yaml --validate
   ```

3. Check permissions:
   ```bash
   ls -la /var/lib/goemailservices
   ls -la /etc/goemailservices/certs
   ```

4. Check TLS certificates:
   ```bash
   openssl x509 -in /etc/goemailservices/certs/tls.crt -noout -dates
   ```

#### Incident: High Queue Depth

**Symptoms:**
- queue_depth metric > 10,000
- Slow message delivery

**Response:**
1. Check delivery workers:
   ```bash
   curl -u admin:password http://localhost:8080/api/v1/queue/stats
   ```

2. Check network connectivity to MX hosts:
   ```bash
   dig gmail.com MX
   telnet gmail-smtp-in.l.google.com 25
   ```

3. Check DLQ for permanent failures:
   ```bash
   curl -u admin:password http://localhost:8080/api/v1/dlq/list
   ```

4. Increase worker count temporarily (if capacity allows)

#### Incident: Authentication Failures Spike

**Symptoms:**
- auth_failures metric spiking
- Locked out accounts

**Response:**
1. Check for brute force attack:
   ```bash
   journalctl -u goemailservices | grep "authentication failed" | tail -100
   ```

2. Identify attacking IPs:
   ```bash
   journalctl -u goemailservices | grep "IP locked" | awk '{print $NF}' | sort | uniq -c | sort -rn
   ```

3. Add firewall rules if needed:
   ```bash
   iptables -A INPUT -s <ATTACKING_IP> -j DROP
   ```

4. Consider reducing max_per_ip in config

#### Incident: Memory Leak

**Symptoms:**
- Memory usage continuously increasing
- OOM killer activates

**Response:**
1. Capture heap profile:
   ```bash
   # If pprof enabled
   curl http://localhost:8080/debug/pprof/heap > heap.prof
   go tool pprof -http=:8081 heap.prof
   ```

2. Restart service to restore functionality:
   ```bash
   systemctl restart goemailservices
   ```

3. Enable detailed logging temporarily
4. Contact development team with heap profile

### 3.3 Incident Communication Template

```
INCIDENT REPORT
===============
Incident ID: INC-20XX-XXXX
Severity: [P1/P2/P3/P4]
Status: [Investigating/Mitigating/Resolved]
Start Time: YYYY-MM-DD HH:MM UTC
End Time: YYYY-MM-DD HH:MM UTC (if resolved)

IMPACT:
- [Description of user impact]
- [Affected services]
- [Number of affected users/messages]

ROOT CAUSE:
- [What caused the incident]

RESOLUTION:
- [Steps taken to resolve]

PREVENTION:
- [Steps to prevent recurrence]
```

---

## 4. Capacity Planning

### 4.1 Baseline Performance

**Single Instance Capacity:**
- **Messages/second**: 500-1000 (depending on message size)
- **Concurrent Connections**: 1,000 (configurable)
- **Queue Capacity**: 1,000,000 messages
- **Storage**: 1 GB per 10,000 messages (average)

### 4.2 Scaling Guidelines

#### Vertical Scaling (Single Instance)

| Metric | Threshold | Action |
|--------|-----------|--------|
| CPU > 70% | Sustained > 5 min | Add CPU cores |
| Memory > 80% | Sustained | Increase RAM |
| Queue > 50K | Sustained | Add worker threads |
| Disk > 80% | - | Expand storage |

#### Horizontal Scaling (Multiple Instances)

**When to add instances:**
- Sustained CPU > 70% across all instances
- Message throughput approaching 80% of capacity
- Geographic distribution requirements

**Load Balancing Strategy:**
- SMTP: DNS round-robin or dedicated load balancer
- API: Nginx/HAProxy with least-connection algorithm
- IMAP: Session-aware load balancing required

### 4.3 Resource Requirements by Scale

| Daily Volume | Instances | CPU (each) | RAM (each) | Storage |
|--------------|-----------|------------|------------|---------|
| 100K msgs | 1 | 2 cores | 2 GB | 50 GB |
| 1M msgs | 3 | 4 cores | 4 GB | 200 GB |
| 10M msgs | 10 | 8 cores | 8 GB | 2 TB |
| 100M msgs | 50 | 16 cores | 16 GB | 20 TB |

### 4.4 Monitoring for Capacity

**Key Indicators:**
1. **CPU Saturation**: > 70% sustained
2. **Memory Pressure**: Swap usage > 0
3. **Queue Growth**: Increasing over 1 hour
4. **Disk I/O Wait**: > 10%
5. **Network Saturation**: Approaching link capacity

**Forecasting:**
```python
# Simple linear extrapolation
current_daily_volume = 500000  # messages
growth_rate = 0.15  # 15% monthly growth
months_ahead = 6

projected_volume = current_daily_volume * (1 + growth_rate) ** months_ahead
required_instances = math.ceil(projected_volume / 1000000 * 3)
print(f"Projected volume in {months_ahead} months: {projected_volume:,.0f}")
print(f"Required instances: {required_instances}")
```

### 4.5 Capacity Testing

**Load Testing Procedure:**
1. Set up isolated test environment
2. Use load testing tool (e.g., smtp-source, custom script)
3. Gradually increase load from 10% to 120% of capacity
4. Monitor all metrics
5. Identify breaking points
6. Document results

**Example Load Test Script:**
```bash
#!/bin/bash
# load-test.sh - Gradually increase SMTP load

for rate in 100 200 500 1000 2000 5000; do
    echo "Testing at $rate messages/minute..."
    smtp-source -c -l 1024 -m $rate -s $((rate/10)) \
        -f sender@test.com -t recipient@test.com \
        localhost:25
    sleep 60
done
```

---

## 5. Maintenance Procedures

### 5.1 Routine Maintenance

**Daily:**
- Check service health: `systemctl status goemailservices`
- Review error logs: `journalctl -u goemailservices -p err --since today`
- Monitor queue depth
- Check disk space

**Weekly:**
- Review metrics dashboards
- Check for certificate expiration (< 30 days)
- Review and clear DLQ if needed
- Update any blacklists/whitelists

**Monthly:**
- Test backup restoration
- Review and rotate logs
- Update system packages: `apt update && apt upgrade`
- Review security audit logs
- Capacity planning review

**Quarterly:**
- Update go-emailservice-ads to latest stable version
- Security audit
- Disaster recovery drill
- Review and update documentation

### 5.2 Certificate Renewal

**Manual Renewal:**
```bash
# Generate new certificate (Let's Encrypt example)
certbot certonly --standalone -d mail.example.com

# Copy to email service directory
cp /etc/letsencrypt/live/mail.example.com/fullchain.pem \
   /etc/goemailservices/certs/tls.crt
cp /etc/letsencrypt/live/mail.example.com/privkey.pem \
   /etc/goemailservices/certs/tls.key

# Update permissions
chown mailservice:mailservice /etc/goemailservices/certs/*

# Reload service (graceful)
systemctl reload goemailservices
```

**Automated Renewal (recommended):**
Create systemd timer for automatic renewal.

### 5.3 Log Rotation

Create `/etc/logrotate.d/goemailservices`:
```
/var/log/goemailservices/*.log {
    daily
    rotate 30
    compress
    delaycompress
    notifempty
    create 0640 mailservice mailservice
    sharedscripts
    postrotate
        systemctl reload goemailservices > /dev/null 2>&1 || true
    endscript
}
```

### 5.4 Database Maintenance

**Journal Cleanup:**
```bash
# Remove old journal files (older than 7 days)
find /var/lib/goemailservices/mail-storage/journal \
    -name "journal-*.log" -mtime +7 -delete
```

**DLQ Management:**
```bash
# List DLQ messages
curl -u admin:password http://localhost:8080/api/v1/dlq/list | jq

# Retry specific message
curl -X POST -u admin:password \
    http://localhost:8080/api/v1/dlq/retry/<message-id>

# Bulk retry after fixing issue
for msg in $(curl -u admin:password http://localhost:8080/api/v1/dlq/list | jq -r '.[].message_id'); do
    curl -X POST -u admin:password http://localhost:8080/api/v1/dlq/retry/$msg
done
```

---

## 6. Disaster Recovery

### 6.1 Disaster Scenarios

1. **Complete Data Center Failure**
   - RTO: 4 hours
   - RPO: 15 minutes

2. **Data Corruption**
   - RTO: 2 hours
   - RPO: 15 minutes

3. **Ransomware Attack**
   - RTO: 8 hours
   - RPO: 1 hour

### 6.2 Recovery Procedures

#### Scenario 1: Data Center Failure

**Failover to Secondary Site:**
1. Verify primary site is unreachable
2. Update DNS records to point to secondary site
3. Restore latest backup to secondary site
4. Start service on secondary site
5. Monitor for any issues
6. Notify users of switchover

**Time Estimate:** 2-4 hours

#### Scenario 2: Data Corruption

**Restore from Backup:**
1. Identify scope of corruption
2. Stop service
3. Move corrupted data to quarantine
4. Restore from most recent clean backup
5. Verify data integrity
6. Start service
7. Test functionality

**Time Estimate:** 1-2 hours

#### Scenario 3: Security Incident

**Containment and Recovery:**
1. Isolate affected systems
2. Preserve evidence
3. Identify attack vector
4. Patch vulnerability
5. Restore from clean backup
6. Reset all credentials
7. Monitor for reinfection

**Time Estimate:** 4-8 hours

### 6.3 DR Testing Schedule

- **Tabletop Exercise**: Quarterly
- **Simulated Failover**: Semi-annually
- **Full DR Drill**: Annually

### 6.4 Contact Information

**Escalation Chain:**
1. On-Call Engineer (Primary)
2. Senior Operations Engineer
3. Engineering Manager
4. CTO/VP Engineering

**External Contacts:**
- Hosting Provider Support
- DNS Provider Support
- Security Incident Response Team (if applicable)

---

## Appendix A: Useful Commands

```bash
# Service Management
systemctl start goemailservices
systemctl stop goemailservices
systemctl restart goemailservices
systemctl status goemailservices
systemctl reload goemailservices  # Graceful reload

# Logs
journalctl -u goemailservices -f
journalctl -u goemailservices --since "1 hour ago"
journalctl -u goemailservices -p err -n 100

# API Queries
curl http://localhost:8080/health
curl http://localhost:8080/ready
curl http://localhost:8080/metrics
curl -u admin:password http://localhost:8080/api/v1/queue/stats | jq

# Process Management
ps aux | grep goemailservices
pgrep -a goemailservices
lsof -p <PID>

# Network
netstat -tulpn | grep goemailservices
ss -tulpn | grep goemailservices
tcpdump -i any port 25 -w smtp-capture.pcap

# Resource Monitoring
top -p $(pgrep goemailservices)
htop -p $(pgrep goemailservices)
iotop -p $(pgrep goemailservices)

# Disk Usage
du -sh /var/lib/goemailservices/*
df -h /var/lib/goemailservices
```

## Appendix B: Performance Tuning

**Linux Kernel Tuning for High Performance:**

```bash
# Add to /etc/sysctl.conf

# Increase connection backlog
net.core.somaxconn = 65535

# Increase maximum connections
net.core.netdev_max_backlog = 5000

# Faster port recycling
net.ipv4.tcp_tw_reuse = 1

# Larger TCP buffers
net.ipv4.tcp_rmem = 4096 87380 16777216
net.ipv4.tcp_wmem = 4096 65536 16777216

# Apply changes
sysctl -p
```

---

**Document Version:** 1.0
**Last Updated:** 2026-03-08
**Maintained By:** Operations Team
**Review Schedule:** Quarterly
