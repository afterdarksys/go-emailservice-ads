# ADS PreMail Quick Reference

## Installation

```bash
# One-line setup
sudo ./scripts/setup-adspremail.sh
```

## Service Management

```bash
# Start
sudo systemctl start adspremail

# Stop
sudo systemctl stop adspremail

# Restart
sudo systemctl restart adspremail

# Status
sudo systemctl status adspremail

# Enable auto-start
sudo systemctl enable adspremail

# View logs
sudo journalctl -u adspremail -f
```

## nftables Management

```bash
# View blacklist
sudo nft list set inet filter adspremail_blacklist

# View rate limit set
sudo nft list set inet filter adspremail_ratelimit

# View all ADS PreMail rules
sudo nft list table inet filter

# Manually add IP to blacklist (1 hour)
sudo nft add element inet filter adspremail_blacklist { 1.2.3.4 timeout 1h }

# Remove IP from blacklist
sudo nft delete element inet filter adspremail_blacklist { 1.2.3.4 }

# Reload nftables config
sudo nft -f /etc/nftables.d/adspremail.conf
```

## Database Queries

```bash
# Connect to database
psql -h localhost -U premail -d emailservice
```

### Top Spammers (Last Hour)

```sql
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

### Blocklisted IPs

```sql
SELECT
    ip_address,
    current_score,
    blocklist_expires,
    notes
FROM ip_characteristics
WHERE is_blocklisted = true
  AND (blocklist_expires IS NULL OR blocklist_expires > NOW())
ORDER BY current_score DESC;
```

### Hourly Statistics

```sql
SELECT
    hour_bucket,
    ip_address,
    violation_count,
    max_score,
    avg_score
FROM hourly_spammer_stats
WHERE hour_bucket >= NOW() - INTERVAL '24 hours'
ORDER BY hour_bucket DESC, violation_count DESC
LIMIT 50;
```

### Recent Connection Events

```sql
SELECT
    timestamp,
    ip_address,
    event_type,
    score,
    action
FROM connection_events
WHERE timestamp > NOW() - INTERVAL '1 hour'
ORDER BY timestamp DESC
LIMIT 100;
```

### Clean Up Old Data

```sql
-- Delete events older than 90 days
DELETE FROM connection_events
WHERE timestamp < NOW() - INTERVAL '90 days';

-- Delete hourly stats older than 90 days
DELETE FROM hourly_spammer_stats
WHERE hour_bucket < NOW() - INTERVAL '90 days';
```

## Configuration

### Edit Config

```bash
nano config-premail.yaml
sudo systemctl restart adspremail
```

### Key Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `proxy.listen_addr` | `:2525` | ADS PreMail listening port |
| `proxy.backend_servers` | `["127.0.0.1:25"]` | Backend mail servers |
| `scoring.thresholds.drop` | `91` | Score threshold for DROP |
| `analyzer.pre_banner_timeout` | `2s` | Time to wait before banner |
| `nftables.enabled` | `true` | Enable nftables integration |

### Environment Variables

```bash
# Edit .env.premail
nano .env.premail

# Required
PREMAIL_DB_PASSWORD='your_secure_password'

# Optional
DNSSCIENCE_API_KEY='your_api_key'
```

## Monitoring

### Real-time Stats

```bash
# Watch blacklist size
watch -n 5 'sudo nft list set inet filter adspremail_blacklist | grep elements'

# Watch connection events
watch -n 2 'psql -U premail -d emailservice -c "SELECT COUNT(*) FROM connection_events WHERE timestamp > NOW() - INTERVAL '"'"'1 minute'"'"';"'
```

### Check if Working

```bash
# Test SMTP connection
telnet localhost 25

# Should be redirected to ADS PreMail on 2525
# Check if banner shows "ADS PreMail"
```

## Common Tasks

### Whitelist an IP

```sql
-- Mark IP as known good (gives -50 score bonus)
UPDATE ip_characteristics
SET reputation_class = 'clean'
WHERE ip_address = '1.2.3.4';
```

### Manually Block an IP

```sql
-- Add to blocklist for 24 hours
UPDATE ip_characteristics
SET is_blocklisted = true,
    blocklist_expires = NOW() + INTERVAL '24 hours',
    notes = 'Manually blocked - spam attack'
WHERE ip_address = '1.2.3.4';
```

```bash
# Also add to nftables blacklist
sudo nft add element inet filter adspremail_blacklist { 1.2.3.4 timeout 24h }
```

### Unblock an IP

```sql
-- Remove from blocklist
UPDATE ip_characteristics
SET is_blocklisted = false,
    blocklist_expires = NULL
WHERE ip_address = '1.2.3.4';
```

```bash
# Remove from nftables
sudo nft delete element inet filter adspremail_blacklist { 1.2.3.4 }
```

### Reset IP Reputation

```sql
-- Reset all metrics for an IP
DELETE FROM ip_characteristics WHERE ip_address = '1.2.3.4';
DELETE FROM hourly_spammer_stats WHERE ip_address = '1.2.3.4';
DELETE FROM connection_events WHERE ip_address = '1.2.3.4';
```

## Troubleshooting

### Service Won't Start

```bash
# Check logs
sudo journalctl -u adspremail -n 50

# Check if port is in use
sudo lsof -i :2525

# Test config
./bin/adspremail --config config-premail.yaml --version
```

### Database Connection Failed

```bash
# Test database connection
psql -h localhost -U premail -d emailservice

# Check credentials in .env.premail
cat .env.premail

# Verify PostgreSQL is running
sudo systemctl status postgresql
```

### nftables Not Working

```bash
# Check if nftables is running
sudo systemctl status nftables

# List all tables
sudo nft list tables

# Reload ADS PreMail config
sudo nft -f /etc/nftables.d/adspremail.conf

# Check for errors
sudo journalctl -u nftables -n 50
```

### Mail Not Being Forwarded

```bash
# Check backend server is listening
nc -zv 127.0.0.1 25

# Check DNAT rule
sudo nft list table inet nat

# Test connection to backend
telnet 127.0.0.1 25
```

## Performance Tuning

### High Traffic

Edit `config-premail.yaml`:

```yaml
proxy:
  max_connections: 5000  # Increase connection limit

database:
  retention_days: 30     # Reduce retention to save space
```

### Aggressive Filtering

```yaml
scoring:
  thresholds:
    drop: 70             # Lower threshold = more aggressive
```

### Lenient Filtering

```yaml
scoring:
  thresholds:
    drop: 95             # Higher threshold = more lenient
```

## Backup & Restore

### Create Backup

```bash
# Database backup
pg_dump -U premail -d emailservice > adspremail_db_backup.sql

# Configuration backup
tar -czf adspremail_config_backup.tar.gz \
    config-premail.yaml \
    .env.premail \
    .config_versions/
```

### Restore Backup

```bash
# Database restore
psql -U premail -d emailservice < adspremail_db_backup.sql

# Configuration restore
tar -xzf adspremail_config_backup.tar.gz
sudo systemctl restart adspremail
```

## Scoring Examples

| Scenario | Score | Action |
|----------|-------|--------|
| Clean email client | 0-10 | ALLOW |
| New IP, proper SMTP | 10-20 | ALLOW |
| Quick disconnect | 30-40 | MONITOR |
| Failed auth + quick disconnect | 45-55 | THROTTLE |
| Pre-banner talk | 100 | DROP (instant) |
| Known spammer | 60-80 | TARPIT |
| High volume + no auth | 70-85 | TARPIT |
| Multiple violations | 90-100 | DROP |

## Important Notes

1. **Requires root:** ADS PreMail needs root access for nftables
2. **Backend configuration:** Backend mail server should listen on port 25 (or custom)
3. **Transparent mode:** Uses DNAT to redirect port 25 → 2525
4. **Database growth:** ~1KB per IP, monitor disk usage
5. **Cleanup:** Run retention cleanup regularly

## Getting Help

```bash
# View README
less ADS_PREMAIL_README.md

# View architecture docs
less docs/ADS_PREMAIL_ARCHITECTURE.md

# Check version
./bin/adspremail --version
```

## Useful Dashboards

### Create monitoring views:

```sql
-- Create view for current hour's top spammers
CREATE VIEW current_hour_spammers AS
SELECT
    hour_bucket,
    ip_address,
    violation_count,
    max_score,
    avg_score
FROM hourly_spammer_stats
WHERE hour_bucket = date_trunc('hour', NOW())
ORDER BY violation_count DESC, max_score DESC
LIMIT 100;

-- Create view for active blocklist
CREATE VIEW active_blocklist AS
SELECT
    ip_address,
    current_score,
    violation_count,
    blocklist_expires,
    last_seen,
    notes
FROM ip_characteristics
WHERE is_blocklisted = true
  AND (blocklist_expires IS NULL OR blocklist_expires > NOW())
ORDER BY current_score DESC;
```

Then query with:
```sql
SELECT * FROM current_hour_spammers;
SELECT * FROM active_blocklist;
```
