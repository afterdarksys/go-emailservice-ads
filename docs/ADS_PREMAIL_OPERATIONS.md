# ADS PreMail Operations Guide

Day-to-day management and monitoring of ADS PreMail.

## Daily Operations

### Check System Status

```bash
# Service status
sudo systemctl status adspremail

# Recent logs
sudo journalctl -u adspremail --since "1 hour ago"

# Active connections
sudo lsof -i :2525
```

### Monitor Blacklist Size

```bash
# View current blacklist
sudo nft list set inet filter adspremail_blacklist

# Count blacklisted IPs
sudo nft list set inet filter adspremail_blacklist | grep -o 'elements' | wc -l
```

### View Top Spammers

```sql
-- Connect to database
psql -h localhost -U premail -d emailservice

-- Top spammers in last hour
SELECT
    ip_address,
    violation_count,
    current_score,
    reputation_class,
    last_seen
FROM ip_characteristics
WHERE last_seen > NOW() - INTERVAL '1 hour'
ORDER BY violation_count DESC, current_score DESC
LIMIT 20;
```

### Check Backend Health

```bash
# Test backend connectivity
nc -zv 127.0.0.1 25

# Check if backend is receiving mail
sudo tail -f /var/log/mail.log  # Postfix
# or
sudo journalctl -u goemailservices -f  # ADS Go Mail Services
```

## Monitoring

### Key Metrics to Track

#### Connection Metrics

```sql
-- Connections per hour
SELECT
    date_trunc('hour', timestamp) as hour,
    COUNT(*) as total_connections,
    SUM(CASE WHEN action = 'drop' THEN 1 ELSE 0 END) as dropped,
    SUM(CASE WHEN action = 'allow' THEN 1 ELSE 0 END) as allowed
FROM connection_events
WHERE timestamp > NOW() - INTERVAL '24 hours'
GROUP BY hour
ORDER BY hour DESC;
```

#### Threat Distribution

```sql
-- Actions taken in last hour
SELECT
    action,
    COUNT(*) as count,
    ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (), 2) as percentage
FROM connection_events
WHERE timestamp > NOW() - INTERVAL '1 hour'
GROUP BY action
ORDER BY count DESC;
```

#### Score Distribution

```sql
-- Score ranges
SELECT
    CASE
        WHEN score BETWEEN 0 AND 30 THEN '0-30 (ALLOW)'
        WHEN score BETWEEN 31 AND 50 THEN '31-50 (MONITOR)'
        WHEN score BETWEEN 51 AND 70 THEN '51-70 (THROTTLE)'
        WHEN score BETWEEN 71 AND 90 THEN '71-90 (TARPIT)'
        ELSE '91-100 (DROP)'
    END as score_range,
    COUNT(*) as count
FROM connection_events
WHERE timestamp > NOW() - INTERVAL '1 hour'
GROUP BY score_range
ORDER BY score_range;
```

### Real-Time Monitoring

#### Watch Logs

```bash
# Follow logs with filtering
sudo journalctl -u adspremail -f | grep -E 'DROP|score|violation'

# Watch connections
watch -n 5 'sudo lsof -i :2525 | tail -20'

# Monitor blacklist changes
watch -n 10 'sudo nft list set inet filter adspremail_blacklist | grep elements'
```

#### Database Monitoring

```sql
-- Create monitoring view
CREATE VIEW real_time_stats AS
SELECT
    COUNT(DISTINCT ip_address) as unique_ips,
    COUNT(*) as total_connections,
    SUM(CASE WHEN action = 'drop' THEN 1 ELSE 0 END) as dropped,
    SUM(CASE WHEN action = 'allow' THEN 1 ELSE 0 END) as allowed,
    AVG(score) as avg_score
FROM connection_events
WHERE timestamp > NOW() - INTERVAL '5 minutes';

-- Query it
SELECT * FROM real_time_stats;
```

## IP Management

### Whitelist an IP

```sql
-- Mark as known good (gives -50 score bonus)
INSERT INTO ip_characteristics (ip_address, reputation_class, current_score, notes)
VALUES ('1.2.3.4', 'clean', 0, 'Manually whitelisted - trusted sender')
ON CONFLICT (ip_address) DO UPDATE
SET reputation_class = 'clean',
    current_score = 0,
    notes = 'Manually whitelisted - trusted sender';
```

### Blacklist an IP

```sql
-- Add to database blacklist
UPDATE ip_characteristics
SET is_blocklisted = true,
    blocklist_expires = NOW() + INTERVAL '24 hours',
    current_score = 100,
    reputation_class = 'blocklisted',
    notes = 'Manually blacklisted - spam attack from this IP'
WHERE ip_address = '1.2.3.4';
```

```bash
# Add to nftables blacklist
sudo nft add element inet filter adspremail_blacklist { 1.2.3.4 timeout 24h }
```

### Remove IP from Blacklist

```sql
-- Remove from database
UPDATE ip_characteristics
SET is_blocklisted = false,
    blocklist_expires = NULL
WHERE ip_address = '1.2.3.4';
```

```bash
# Remove from nftables
sudo nft delete element inet filter adspremail_blacklist { 1.2.3.4 }
```

### View IP History

```sql
-- Complete history for an IP
SELECT
    ip_address,
    first_seen,
    last_seen,
    total_connections,
    messages_sent,
    violation_count,
    current_score,
    reputation_class,
    is_blocklisted,
    notes
FROM ip_characteristics
WHERE ip_address = '1.2.3.4';

-- Recent events for an IP
SELECT timestamp, event_type, score, action, details
FROM connection_events
WHERE ip_address = '1.2.3.4'
ORDER BY timestamp DESC
LIMIT 50;
```

## Configuration Management

### View Current Configuration

```bash
cat config-premail.yaml
```

### Update Configuration

```bash
# Edit config
nano config-premail.yaml

# Restart service to apply
sudo systemctl restart adspremail

# Verify restart
sudo systemctl status adspremail
```

### Configuration Versioning

The configuration manager automatically versions all changes:

```bash
# View version history
ls -la .config_versions/

# Each version is stored as: v1.json, v2.json, etc.
```

To rollback (requires code integration):
```go
// In application code
configMgr.Rollback(versionNum, "Rollback reason")
```

### Adjust Scoring Thresholds

Edit `config-premail.yaml`:

```yaml
scoring:
  thresholds:
    # More aggressive (block more):
    drop: 70

    # More lenient (block less):
    drop: 95
```

Restart after changes:
```bash
sudo systemctl restart adspremail
```

## Backup and Restore

### Create Backup

#### Database Backup

```bash
# Backup database
pg_dump -U premail -d emailservice > adspremail_db_$(date +%Y%m%d).sql

# Compressed backup
pg_dump -U premail -d emailservice | gzip > adspremail_db_$(date +%Y%m%d).sql.gz
```

#### Configuration Backup

```bash
# Backup configuration and versions
tar -czf adspremail_config_$(date +%Y%m%d).tar.gz \
    config-premail.yaml \
    .env.premail \
    .config_versions/
```

#### Complete System Backup

```bash
# All components
tar -czf adspremail_full_backup_$(date +%Y%m%d).tar.gz \
    config-premail.yaml \
    .env.premail \
    .config_versions/ \
    bin/adspremail \
    /etc/nftables.d/adspremail.conf \
    /etc/systemd/system/adspremail.service

# Database separately
pg_dump -U premail -d emailservice | gzip > adspremail_db_$(date +%Y%m%d).sql.gz
```

### Restore from Backup

#### Database Restore

```bash
# Restore database
gunzip < adspremail_db_20260310.sql.gz | psql -U premail -d emailservice
```

#### Configuration Restore

```bash
# Extract configuration
tar -xzf adspremail_config_20260310.tar.gz

# Restart service
sudo systemctl restart adspremail
```

### Automated Backups

Create backup script `/usr/local/bin/backup-adspremail.sh`:

```bash
#!/bin/bash
BACKUP_DIR="/var/backups/adspremail"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p "$BACKUP_DIR"

# Database backup
pg_dump -U premail -d emailservice | gzip > "$BACKUP_DIR/db_$DATE.sql.gz"

# Config backup
tar -czf "$BACKUP_DIR/config_$DATE.tar.gz" \
    /path/to/config-premail.yaml \
    /path/to/.env.premail \
    /path/to/.config_versions/

# Keep only last 30 days
find "$BACKUP_DIR" -name "*.gz" -mtime +30 -delete

echo "Backup completed: $DATE"
```

Add to crontab:
```bash
sudo crontab -e

# Daily backup at 2 AM
0 2 * * * /usr/local/bin/backup-adspremail.sh
```

## Maintenance

### Clean Up Old Data

```sql
-- Delete connection events older than 90 days
DELETE FROM connection_events
WHERE timestamp < NOW() - INTERVAL '90 days';

-- Delete hourly stats older than 90 days
DELETE FROM hourly_spammer_stats
WHERE hour_bucket < NOW() - INTERVAL '90 days';

-- Vacuum database
VACUUM ANALYZE;
```

Automate with cron:
```bash
# Add to postgres crontab
sudo -u postgres crontab -e

# Weekly cleanup on Sunday at 3 AM
0 3 * * 0 psql -U premail -d emailservice -c "DELETE FROM connection_events WHERE timestamp < NOW() - INTERVAL '90 days'; DELETE FROM hourly_spammer_stats WHERE hour_bucket < NOW() - INTERVAL '90 days'; VACUUM ANALYZE;"
```

### Log Rotation

Create `/etc/logrotate.d/adspremail`:

```
/var/log/adspremail/*.log {
    daily
    rotate 30
    compress
    delaycompress
    notifempty
    create 0640 root adm
    sharedscripts
    postrotate
        systemctl reload adspremail
    endscript
}
```

### Database Maintenance

```bash
# Check database size
sudo -u postgres psql -d emailservice -c "SELECT pg_size_pretty(pg_database_size('emailservice'));"

# Reindex tables
sudo -u postgres psql -d emailservice -c "REINDEX DATABASE emailservice;"

# Analyze tables
sudo -u postgres psql -d emailservice -c "ANALYZE;"
```

## Performance Tuning

### High-Traffic Optimization

Edit `config-premail.yaml`:

```yaml
proxy:
  max_connections: 5000  # Increase connection limit

database:
  retention_days: 30     # Reduce retention to save space

analyzer:
  hourly_connection_limit: 500  # Adjust for your volume
```

### PostgreSQL Tuning

Edit `/etc/postgresql/*/main/postgresql.conf`:

```conf
# Memory settings
shared_buffers = 256MB
effective_cache_size = 1GB
work_mem = 16MB

# Connection settings
max_connections = 200

# Performance
random_page_cost = 1.1
effective_io_concurrency = 200
```

Restart PostgreSQL:
```bash
sudo systemctl restart postgresql
```

### nftables Performance

For very high traffic, optimize nftables:

```bash
# Increase table size
sudo nft add set inet filter adspremail_blacklist '{ size 100000; }'
```

## Troubleshooting

### High CPU Usage

```bash
# Check process
top -p $(pgrep adspremail)

# Check connection count
sudo lsof -i :2525 | wc -l

# Review recent high-score connections
sudo journalctl -u adspremail | grep "score: 9[0-9]" | tail -20
```

**Solutions:**
- Increase scoring thresholds (block more aggressively)
- Add more backend servers for load distribution
- Optimize database queries

### High Memory Usage

```bash
# Check memory
ps aux | grep adspremail

# Check tracked IPs
psql -U premail -d emailservice -c "SELECT COUNT(*) FROM ip_characteristics;"
```

**Solutions:**
- Reduce retention period
- Clean up old data more frequently
- Add more RAM

### Database Performance Issues

```sql
-- Find slow queries
SELECT query, mean_time, calls
FROM pg_stat_statements
ORDER BY mean_time DESC
LIMIT 10;

-- Check table sizes
SELECT
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
```

**Solutions:**
- Add indexes on frequently queried columns
- Increase shared_buffers
- Run VACUUM ANALYZE regularly

### Spam Still Getting Through

```sql
-- Analyze false negatives (spam that got through)
SELECT
    ip_address,
    score,
    action,
    details
FROM connection_events
WHERE action = 'allow'
  AND timestamp > NOW() - INTERVAL '24 hours'
ORDER BY score DESC
LIMIT 50;
```

**Solutions:**
- Lower DROP threshold (more aggressive)
- Adjust scoring weights
- Add IPs to manual blacklist

### Legitimate Mail Being Blocked

```sql
-- Analyze false positives (legitimate mail blocked)
SELECT
    ip_address,
    score,
    action,
    details
FROM connection_events
WHERE action = 'drop'
  AND timestamp > NOW() - INTERVAL '24 hours'
ORDER BY timestamp DESC
LIMIT 50;
```

**Solutions:**
- Whitelist legitimate sender IPs
- Increase DROP threshold (more lenient)
- Adjust scoring weights
- Check for pre-banner talk false positives

## Reporting

### Daily Summary Report

```sql
-- Daily summary
SELECT
    DATE(timestamp) as date,
    COUNT(*) as total_connections,
    COUNT(DISTINCT ip_address) as unique_ips,
    SUM(CASE WHEN action = 'allow' THEN 1 ELSE 0 END) as allowed,
    SUM(CASE WHEN action = 'drop' THEN 1 ELSE 0 END) as dropped,
    SUM(CASE WHEN action = 'tarpit' THEN 1 ELSE 0 END) as tarpitted,
    ROUND(100.0 * SUM(CASE WHEN action = 'drop' THEN 1 ELSE 0 END) / COUNT(*), 2) as block_rate
FROM connection_events
WHERE timestamp > NOW() - INTERVAL '7 days'
GROUP BY DATE(timestamp)
ORDER BY date DESC;
```

### Top Spammer Report

```sql
-- Top 50 spammers this week
SELECT
    ip_address,
    SUM(violation_count) as total_violations,
    MAX(max_score) as highest_score,
    COUNT(DISTINCT hour_bucket) as active_hours
FROM hourly_spammer_stats
WHERE hour_bucket > NOW() - INTERVAL '7 days'
GROUP BY ip_address
ORDER BY total_violations DESC
LIMIT 50;
```

### Export Reports

```bash
# Export to CSV
psql -U premail -d emailservice -c "COPY (
    SELECT * FROM connection_events
    WHERE timestamp > NOW() - INTERVAL '24 hours'
) TO STDOUT CSV HEADER" > report_$(date +%Y%m%d).csv
```

## Emergency Procedures

### System Under Attack

```bash
# 1. Immediately lower DROP threshold
nano config-premail.yaml
# Change: drop: 50  (from 91)
sudo systemctl restart adspremail

# 2. Add attacking IPs to blacklist
sudo nft add element inet filter adspremail_blacklist { 1.2.3.4 timeout 24h }

# 3. Monitor attack
sudo journalctl -u adspremail -f | grep DROP
```

### Database Full

```sql
-- Emergency cleanup
DELETE FROM connection_events WHERE timestamp < NOW() - INTERVAL '7 days';
DELETE FROM hourly_spammer_stats WHERE hour_bucket < NOW() - INTERVAL '7 days';
VACUUM FULL;
```

### Service Crash

```bash
# Check crash logs
sudo journalctl -u adspremail --since "10 minutes ago"

# Check core dump
sudo coredumpctl list

# Restart service
sudo systemctl restart adspremail
```

---

For architecture details, see [ADS_PREMAIL_ARCHITECTURE.md](ADS_PREMAIL_ARCHITECTURE.md)
