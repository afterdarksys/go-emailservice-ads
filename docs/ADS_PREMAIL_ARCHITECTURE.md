# ADS PreMail - Transparent SMTP Protection Layer

## Overview

ADS PreMail is a transparent SMTP proxy that sits in front of mail servers to provide real-time spam detection and filtering using composite behavioral analysis. Inspired by Symantec/Turntides 8160 connection-level filtering.

## Architecture

```
Internet → nftables → ADS PreMail → Backend Mail Server
```

### Components

1. **Transparent Proxy** (`internal/premail/proxy`)
   - Accepts SMTP connections on port 2525
   - Preserves source IP addresses
   - Implements SMTP protocol state machine
   - Proxies clean traffic to backend servers

2. **Connection Analyzer** (`internal/premail/analyzer`)
   - Pre-banner talker detection
   - Connection timing analysis
   - Quick connect/disconnect patterns
   - Protocol violation detection

3. **Composite Scoring Engine** (`internal/premail/scoring`)
   - Real-time threat scoring (0-100)
   - Multi-factor analysis
   - Weighted scoring algorithm
   - Dynamic threshold adjustment

4. **IP Reputation Database** (`internal/premail/reputation`)
   - PostgreSQL-backed characteristics storage
   - Hourly top spammer analysis
   - Historical pattern tracking
   - dnsscience.io integration

5. **nftables Integration** (`internal/premail/nftables`)
   - Dynamic packet marking
   - Automatic blacklist updates
   - Rate limiting via nftables sets
   - Real-time rule updates

## Composite Scoring System

### Score Components (Total: 100 points)

#### Protocol Violations (Instant Actions)
- **Pre-banner talking**: 100 (instant DROP)
- **Invalid SMTP commands**: +40
- **Malformed HELO/EHLO**: +25
- **Missing required commands**: +20

#### Connection Patterns (Medium-High Weight)
- **Quick connect/disconnect ratio** (>50% within 2s): +30
- **Connection frequency spike** (>100/hour): +25
- **No auth attempts on submission**: +20
- **Failed auth attempts**: +15 per failure (max 45)
- **Multiple recipients, no auth**: +20

#### Volume Anomalies (Medium Weight)
- **High recipient count** (>50 per message): +20
- **Messages per connection** (>10): +15
- **Unusual message sizes** (<100b or >10MB): +10
- **Rapid-fire connections**: +15

#### Historical Reputation (Persistent)
- **Previously flagged as spammer**: +30
- **In hourly top spammers list**: +25
- **Multiple recent violations**: +20
- **New IP never seen before**: +10
- **Known good sender**: -50

#### Timing Patterns (Low Weight)
- **Bot-like regular intervals**: +15
- **Off-hours bulk sending** (2am-6am UTC): +10
- **Identical inter-message timing**: +10

### Action Thresholds

```
Score  Action         Details
─────  ──────────────────────────────────────────────────
0-30   ALLOW          Clean traffic → forward to backend
                      - No marking
                      - Normal processing
                      - Update "clean" reputation

31-50  MONITOR        Suspicious → forward but watch
                      - Mark packets with fwmark 0x1
                      - Log to database
                      - Increase monitoring frequency

51-70  THROTTLE       Likely spam → rate limit
                      - Mark packets with fwmark 0x2
                      - Rate limit to 1 msg/minute
                      - Add to short-term watchlist (1 hour)
                      - Still forward to backend

71-90  TARPIT         High confidence spam
                      - Mark packets with fwmark 0x3
                      - Introduce 30s delays in responses
                      - Add to nftables rate limit set
                      - Log extensively
                      - May forward to backend (configurable)

91-100 DROP           Definite spam
                      - Mark packets with fwmark 0x4
                      - Add to nftables blacklist set
                      - TCP RST / connection drop
                      - Add to PostgreSQL blocklist
                      - Never reaches backend
                      - Feed to dnsscience.io
```

## Database Schema

### ip_characteristics table
```sql
CREATE TABLE ip_characteristics (
    id BIGSERIAL PRIMARY KEY,
    ip_address INET NOT NULL,
    first_seen TIMESTAMP NOT NULL DEFAULT NOW(),
    last_seen TIMESTAMP NOT NULL DEFAULT NOW(),

    -- Connection metrics
    total_connections BIGINT DEFAULT 0,
    quick_disconnects BIGINT DEFAULT 0,
    pre_banner_talks BIGINT DEFAULT 0,

    -- Volume metrics
    messages_sent BIGINT DEFAULT 0,
    recipients_count BIGINT DEFAULT 0,
    failed_auth_attempts BIGINT DEFAULT 0,

    -- Scoring
    current_score INTEGER DEFAULT 0,
    max_score_7d INTEGER DEFAULT 0,
    violation_count BIGINT DEFAULT 0,

    -- Reputation
    reputation_class VARCHAR(20) DEFAULT 'unknown',
    is_blocklisted BOOLEAN DEFAULT FALSE,
    blocklist_expires TIMESTAMP,

    -- Metadata
    last_violation TIMESTAMP,
    notes TEXT,

    UNIQUE(ip_address)
);

CREATE INDEX idx_ip_score ON ip_characteristics(current_score DESC);
CREATE INDEX idx_ip_blocklist ON ip_characteristics(is_blocklisted, blocklist_expires);
CREATE INDEX idx_ip_last_seen ON ip_characteristics(last_seen DESC);
```

### hourly_spammer_stats table
```sql
CREATE TABLE hourly_spammer_stats (
    id BIGSERIAL PRIMARY KEY,
    hour_bucket TIMESTAMP NOT NULL,
    ip_address INET NOT NULL,
    connection_count BIGINT DEFAULT 0,
    message_count BIGINT DEFAULT 0,
    violation_count BIGINT DEFAULT 0,
    avg_score NUMERIC(5,2),
    max_score INTEGER,

    UNIQUE(hour_bucket, ip_address)
);

CREATE INDEX idx_hourly_bucket ON hourly_spammer_stats(hour_bucket DESC);
CREATE INDEX idx_hourly_violations ON hourly_spammer_stats(violation_count DESC);
```

### connection_events table
```sql
CREATE TABLE connection_events (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW(),
    ip_address INET NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    score INTEGER,
    action VARCHAR(20),
    details JSONB,

    -- For Elasticsearch correlation
    trace_id UUID
);

CREATE INDEX idx_events_ip ON connection_events(ip_address, timestamp DESC);
CREATE INDEX idx_events_type ON connection_events(event_type);
CREATE INDEX idx_events_score ON connection_events(score DESC);
```

## nftables Integration

### Sets and Maps

```nftables
# Blacklist set (DROP immediately)
set adspremail_blacklist {
    type ipv4_addr
    flags timeout
    timeout 24h
}

# Rate limit set (TARPIT)
set adspremail_ratelimit {
    type ipv4_addr
    flags timeout
    timeout 1h
}

# Monitoring set (just mark)
set adspremail_monitor {
    type ipv4_addr
    flags timeout
    timeout 30m
}
```

### Packet Marking Rules

```nftables
table inet filter {
    chain prerouting {
        type filter hook prerouting priority 0;

        # Check blacklist first
        ip saddr @adspremail_blacklist drop

        # Apply marks
        ip saddr @adspremail_ratelimit meta mark set 0x3
        ip saddr @adspremail_monitor meta mark set 0x1
    }

    chain input {
        type filter hook input priority 0;

        # Rate limiting for tarpit IPs
        meta mark 0x3 limit rate 1/minute accept
        meta mark 0x3 drop
    }
}

# DNAT for transparent proxy
table inet nat {
    chain prerouting {
        type nat hook prerouting priority -100;

        # Redirect port 25 to ADS PreMail on 2525
        tcp dport 25 dnat to :2525
    }
}
```

## Configuration

### config.yaml addition

```yaml
adspremail:
  enabled: true

  # Listening
  listen_addr: ":2525"

  # Backend mail servers
  backend_servers:
    - "127.0.0.1:25"      # Primary
    - "127.0.0.1:2526"    # Backup

  # Scoring thresholds
  thresholds:
    allow: 30
    monitor: 50
    throttle: 70
    tarpit: 90
    drop: 91

  # Database
  database:
    host: "localhost"
    port: 5432
    database: "emailservice"
    user: "premail"
    password: "${PREMAIL_DB_PASSWORD}"

  # nftables integration
  nftables:
    enabled: true
    table_name: "inet filter"
    blacklist_set: "adspremail_blacklist"
    ratelimit_set: "adspremail_ratelimit"
    monitor_set: "adspremail_monitor"

  # Reputation feed
  reputation:
    dnsscience_enabled: true
    dnsscience_api_key: "${DNSSCIENCE_API_KEY}"
    feed_interval: "1h"

  # Analysis
  analyzer:
    pre_banner_timeout: "2s"
    quick_disconnect_threshold: "2s"
    hourly_connection_limit: 100

  # Tarpit settings
  tarpit:
    delay_seconds: 30
    max_delay_seconds: 300
```

## Deployment

### Transparent Mode Setup

```bash
# 1. Enable IP forwarding
sysctl -w net.ipv4.ip_forward=1

# 2. Configure nftables
nft -f /etc/nftables/adspremail.conf

# 3. Start ADS PreMail
./bin/adspremail --config config.yaml

# 4. Backend mail server listens on :25 (or :2526)
```

### Docker Deployment

```dockerfile
FROM alpine:latest
RUN apk add --no-cache nftables
COPY bin/adspremail /usr/local/bin/
COPY config.yaml /etc/adspremail/
ENTRYPOINT ["/usr/local/bin/adspremail"]
```

## Monitoring

### Metrics (Prometheus)

```
adspremail_connections_total{action="allow|monitor|throttle|tarpit|drop"}
adspremail_score_distribution_bucket
adspremail_current_score{ip}
adspremail_blacklist_size
adspremail_pre_banner_violations_total
adspremail_backend_forwards_total
adspremail_backend_failures_total
```

### Grafana Dashboards

1. **Real-time Threat Map**
   - Connection attempts by IP
   - Score distribution
   - Top offenders

2. **Action Breakdown**
   - Allow/Monitor/Throttle/Tarpit/Drop ratios
   - Trend over time

3. **Hourly Top Spammers**
   - Updated every hour
   - Automatic blocklist generation

## Performance Characteristics

- **Latency overhead**: <5ms for clean connections
- **Throughput**: 10,000+ connections/second
- **Memory**: ~100MB base + 50 bytes per tracked IP
- **Database**: ~1KB per IP in characteristics table
- **Backend protection**: 70-90% spam blocked before reaching mail server

## Security Considerations

1. **DoS Protection**: Rate limiting at nftables level
2. **Database isolation**: Separate DB user with minimal privileges
3. **Configuration secrets**: Environment variables only
4. **Logging**: No PII in logs, only IPs and metrics
5. **Fail-open**: If PreMail crashes, nftables rules remain active

## Future Enhancements

1. **Machine Learning**: Train models on connection patterns
2. **Distributed Reputation**: Share data across multiple PreMail instances
3. **GeoIP Integration**: Geographic anomaly detection
4. **ASN-based Scoring**: Score entire AS blocks
5. **Honeypot Integration**: Feed honeypot data into scoring
