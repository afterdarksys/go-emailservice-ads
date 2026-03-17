# ADS PreMail Setup Guide

Complete installation and configuration guide for ADS PreMail.

## Prerequisites

### System Requirements

- **OS**: Linux (Ubuntu 20.04+, Debian 11+, RHEL 8+, or compatible)
- **CPU**: 2+ cores recommended
- **RAM**: 2GB minimum, 4GB+ recommended
- **Disk**: 10GB minimum for database and logs
- **Network**: Static IP address recommended

### Required Software

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install -y nftables postgresql postgresql-client golang-go

# RHEL/CentOS
sudo dnf install -y nftables postgresql postgresql-server golang
```

### Verify Prerequisites

```bash
# Check nftables
nft --version

# Check PostgreSQL
psql --version

# Check Go
go version
```

## Automated Installation

The fastest way to get ADS PreMail running:

```bash
cd /path/to/go-emailservice-ads

# Run automated setup (requires root)
sudo ./scripts/setup-adspremail.sh
```

This script will:
1. ✅ Check prerequisites
2. ✅ Build ADS PreMail binary
3. ✅ Create PostgreSQL database and user
4. ✅ Configure nftables rules
5. ✅ Create systemd service
6. ✅ Generate configuration files

**Then skip to [Starting ADS PreMail](#starting-ads-premail)**

## Manual Installation

For custom setups or to understand the process:

### Step 1: Build ADS PreMail

```bash
cd /path/to/go-emailservice-ads

# Build binary
go build -o bin/adspremail ./cmd/adspremail

# Verify build
./bin/adspremail --version
```

### Step 2: Initialize Configuration

```bash
# Generate default configuration
./bin/adspremail --init --config config-premail.yaml

# Edit configuration
nano config-premail.yaml
```

### Step 3: Set Up PostgreSQL Database

```bash
# Switch to postgres user
sudo -u postgres psql

# In PostgreSQL prompt:
CREATE DATABASE emailservice;
CREATE USER premail WITH PASSWORD 'your_secure_password_here';
GRANT ALL PRIVILEGES ON DATABASE emailservice TO premail;
\q
```

### Step 4: Configure Environment Variables

```bash
# Create .env.premail file
cat > .env.premail <<EOF
PREMAIL_DB_PASSWORD='your_secure_password_here'
# Optional: DNSSCIENCE_API_KEY='your_api_key'
EOF

# Secure the file
chmod 600 .env.premail
```

### Step 5: Update Configuration

Edit `config-premail.yaml` with your database credentials:

```yaml
database:
  host: "localhost"
  port: 5432
  database: "emailservice"
  user: "premail"
  password: "${PREMAIL_DB_PASSWORD}"  # Will read from .env.premail
  ssl_mode: "require"
```

### Step 6: Set Up nftables

Create nftables configuration:

```bash
sudo mkdir -p /etc/nftables.d

sudo tee /etc/nftables.d/adspremail.conf <<'EOF'
#!/usr/sbin/nft -f

# ADS PreMail nftables rules
table inet filter {
    # IP sets with automatic timeout
    set adspremail_blacklist {
        type ipv4_addr
        flags timeout
        timeout 24h
    }

    set adspremail_ratelimit {
        type ipv4_addr
        flags timeout
        timeout 1h
    }

    set adspremail_monitor {
        type ipv4_addr
        flags timeout
        timeout 30m
    }

    # Prerouting chain
    chain adspremail_prerouting {
        type filter hook prerouting priority 0;
        ip saddr @adspremail_blacklist drop
        ip saddr @adspremail_ratelimit meta mark set 0x3
        ip saddr @adspremail_monitor meta mark set 0x1
    }

    # Input chain
    chain adspremail_input {
        type filter hook input priority 0;
        meta mark 0x3 limit rate 1/minute accept
        meta mark 0x3 drop
    }
}

# NAT table for transparent proxy
table inet nat {
    chain prerouting {
        type nat hook prerouting priority -100;
        tcp dport 25 dnat to :2525
    }
}
EOF

# Make executable
sudo chmod +x /etc/nftables.d/adspremail.conf

# Load rules
sudo nft -f /etc/nftables.d/adspremail.conf
```

Make nftables persistent:

```bash
# Add to main nftables config
echo 'include "/etc/nftables.d/adspremail.conf"' | sudo tee -a /etc/nftables.conf

# Enable nftables service
sudo systemctl enable nftables
```

### Step 7: Create Systemd Service

```bash
sudo tee /etc/systemd/system/adspremail.service <<EOF
[Unit]
Description=ADS PreMail - Transparent SMTP Protection Layer
After=network.target postgresql.service nftables.service
Wants=postgresql.service nftables.service

[Service]
Type=simple
User=root
WorkingDirectory=$(pwd)
EnvironmentFile=$(pwd)/.env.premail
ExecStart=$(pwd)/bin/adspremail --config $(pwd)/config-premail.yaml
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$(pwd)

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd
sudo systemctl daemon-reload
```

## Configuration

### Essential Settings

Edit `config-premail.yaml`:

#### Proxy Configuration

```yaml
proxy:
  listen_addr: ":2525"              # ADS PreMail listening port
  backend_servers:
    - "127.0.0.1:25"                # Primary backend mail server
    # - "127.0.0.1:2526"            # Optional: Backup server
  server_name: "msgs.global"        # Your domain
  read_timeout: 5m
  write_timeout: 5m
  max_connections: 1000
```

#### Scoring Thresholds

```yaml
scoring:
  thresholds:
    allow: 30      # 0-30: Clean traffic
    monitor: 50    # 31-50: Suspicious, watch
    throttle: 70   # 51-70: Likely spam, slow down
    tarpit: 90     # 71-90: High confidence spam, tarpit
    drop: 91       # 91-100: Definite spam, drop
```

#### Database Connection

```yaml
database:
  host: "localhost"
  port: 5432
  database: "emailservice"
  user: "premail"
  password: "${PREMAIL_DB_PASSWORD}"
  ssl_mode: "require"
  retention_days: 90  # Auto-cleanup old data
```

#### nftables Integration

```yaml
nftables:
  enabled: true
  table_name: "inet filter"
  blacklist_set: "adspremail_blacklist"
  ratelimit_set: "adspremail_ratelimit"
  monitor_set: "adspremail_monitor"
```

### Optional: dnsscience.io Integration

```yaml
reputation:
  dnsscience_enabled: true
  dnsscience_api_url: "https://api.dnsscience.io"
  dnsscience_api_key: "${DNSSCIENCE_API_KEY}"
  feed_interval: 1h
  batch_size: 100
```

Get API key from: https://dnsscience.io/signup

## Backend Mail Server Configuration

Configure your backend mail server to work with ADS PreMail:

### For ADS Go Mail Services

```yaml
# config.yaml
server:
  addr: ":25"  # Listen on port 25
  # Optionally bind to localhost only if same server:
  # bind_addr: "127.0.0.1:25"
```

### For Postfix

```bash
# /etc/postfix/main.cf

# If ADS PreMail is on same server:
inet_interfaces = 127.0.0.1
smtp_bind_address = 127.0.0.1

# If ADS PreMail is on different server:
inet_interfaces = all
mynetworks = 127.0.0.1, [::1], 10.0.0.0/8  # Add ADS PreMail server IP
```

Restart Postfix:
```bash
sudo systemctl restart postfix
```

### For Exim

```bash
# /etc/exim4/update-exim4.conf.conf
dc_local_interfaces='127.0.0.1'  # If same server
```

## Starting ADS PreMail

### First Start

```bash
# Load environment variables
source .env.premail

# Start ADS PreMail
sudo systemctl start adspremail

# Check status
sudo systemctl status adspremail

# View logs
sudo journalctl -u adspremail -f
```

### Enable Auto-Start

```bash
sudo systemctl enable adspremail
```

## Verification

### 1. Check Service Status

```bash
sudo systemctl status adspremail
```

Expected output:
```
● adspremail.service - ADS PreMail - Transparent SMTP Protection Layer
   Loaded: loaded (/etc/systemd/system/adspremail.service; enabled)
   Active: active (running) since ...
```

### 2. Check Listening Ports

```bash
sudo netstat -tlnp | grep adspremail
```

Expected output:
```
tcp6  0  0 :::2525  :::*  LISTEN  12345/adspremail
```

### 3. Test SMTP Connection

```bash
telnet localhost 25
```

Should see:
```
220 msgs.global ESMTP ADS PreMail
```

### 4. Check nftables Rules

```bash
sudo nft list tables
```

Should include:
```
table inet filter
table inet nat
```

```bash
sudo nft list table inet filter
```

Should show ADS PreMail sets and rules.

### 5. Check Database Connection

```bash
psql -h localhost -U premail -d emailservice
```

```sql
\dt  -- List tables
```

Should show:
- `ip_characteristics`
- `hourly_spammer_stats`
- `connection_events`

### 6. Send Test Email

```bash
# From another server
telnet your-server-ip 25
```

```smtp
EHLO test.example.com
MAIL FROM:<test@example.com>
RCPT TO:<user@msgs.global>
DATA
Subject: Test Email

This is a test message.
.
QUIT
```

Check logs:
```bash
sudo journalctl -u adspremail -f
```

Should see connection scored and forwarded.

## Troubleshooting

### Service Won't Start

**Check logs:**
```bash
sudo journalctl -u adspremail -xe
```

**Common issues:**
- Port 2525 already in use: `sudo lsof -i :2525`
- Database connection failed: Check credentials in `.env.premail`
- nftables errors: Run `sudo nft -f /etc/nftables.d/adspremail.conf`

### Can't Connect to Port 25

**Check firewall:**
```bash
# Ubuntu/Debian
sudo ufw allow 25/tcp

# RHEL/CentOS
sudo firewall-cmd --permanent --add-port=25/tcp
sudo firewall-cmd --reload
```

**Check nftables DNAT:**
```bash
sudo nft list table inet nat
```

Should show redirect rule for port 25 → 2525.

### Backend Not Receiving Mail

**Check backend is listening:**
```bash
nc -zv 127.0.0.1 25
```

**Check ADS PreMail can connect:**
```bash
sudo journalctl -u adspremail | grep "backend"
```

**Test direct connection:**
```bash
telnet 127.0.0.1 25
```

### Database Errors

**Check PostgreSQL is running:**
```bash
sudo systemctl status postgresql
```

**Test connection:**
```bash
psql -h localhost -U premail -d emailservice
```

**Check credentials:**
```bash
cat .env.premail
```

## Next Steps

1. **Monitor Operations**: See [ADS_PREMAIL_OPERATIONS.md](ADS_PREMAIL_OPERATIONS.md)
2. **Fine-tune Scoring**: Adjust thresholds based on your traffic
3. **Set Up Monitoring**: Create dashboards for key metrics
4. **Enable dnsscience.io**: Share threat intelligence
5. **Configure Backups**: Schedule regular database backups

## Security Hardening

### Firewall Rules

```bash
# Only allow port 25 from internet
# Block direct access to 2525 from outside
sudo iptables -A INPUT -p tcp --dport 2525 ! -s 127.0.0.1 -j DROP
```

### Database Security

```sql
-- Revoke unnecessary privileges
REVOKE ALL ON DATABASE emailservice FROM PUBLIC;
GRANT CONNECT ON DATABASE emailservice TO premail;
```

### File Permissions

```bash
# Secure configuration
chmod 600 config-premail.yaml .env.premail

# Secure database directory
sudo chown -R postgres:postgres /var/lib/postgresql
```

## Production Checklist

- [ ] PostgreSQL configured with strong password
- [ ] `.env.premail` file permissions set to 600
- [ ] nftables rules loaded and persistent
- [ ] Systemd service enabled for auto-start
- [ ] Backend mail server configured correctly
- [ ] Firewall allows port 25
- [ ] Database backups scheduled
- [ ] Monitoring/alerting configured
- [ ] Tested email flow end-to-end
- [ ] Documentation reviewed by team

---

For operational procedures, see [ADS_PREMAIL_OPERATIONS.md](ADS_PREMAIL_OPERATIONS.md)
