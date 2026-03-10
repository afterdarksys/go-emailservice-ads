# Quick Deployment Guide

## What Was Created

A complete Ansible automation for deploying the Go Email Service stack to apps.afterdarksys.com with:

✅ **Full Docker stack deployment** (Email service, PostgreSQL, Redis, Prometheus, Grafana)
✅ **Standard SMTP ports** (25, 587, 465) configured
✅ **Automated SSL/TLS** (Let's Encrypt or self-signed)
✅ **Systemd service** for automatic startup and management
✅ **Automated backups** with daily cron job
✅ **Security hardening** (firewall, system limits, user isolation)
✅ **Monitoring stack** (Prometheus + Grafana)
✅ **Update, backup, and restore** playbooks

## Quick Deploy (3 Steps)

### 1. Install Ansible

```bash
# macOS
brew install ansible

# Ubuntu
sudo apt install ansible

# Verify
ansible --version
```

### 2. Configure Deployment

Edit `deploy/ansible/group_vars/emailservers.yml`:

```yaml
# Change these critical values:
admin_password: "CHANGE-ME-TO-SECURE-PASSWORD"
postgres_password: "CHANGE-ME-TO-SECURE-PASSWORD"

# For production with real SSL:
letsencrypt_enabled: true
letsencrypt_email: "admin@apps.afterdarksys.com"
```

### 3. Deploy!

```bash
cd deploy/ansible

# Test connection
ansible -i inventories/production.ini emailservers -m ping

# Deploy (takes ~10 minutes)
./quick-start.sh

# Or manually:
ansible-playbook -i inventories/production.ini deploy.yml
```

## What Gets Deployed

### Services on apps.afterdarksys.com

| Service | Port | Description |
|---------|------|-------------|
| SMTP | 25 | Standard SMTP (MX delivery) |
| Submission | 587 | Authenticated email submission |
| SMTPS | 465 | SMTP over TLS |
| IMAP | 143 | Email retrieval |
| IMAPS | 993 | IMAP over TLS |
| REST API | 8080 | Management API |
| gRPC | 50051 | gRPC API |
| AfterSMTP QUIC | 4434/udp | Next-gen protocol |
| AfterSMTP gRPC | 4433 | AfterSMTP streaming |
| Prometheus | 9091 | Metrics collection |
| Grafana | 3000 | Monitoring dashboards |
| PostgreSQL | 5432 | Metadata database |
| Redis | 6379 | Cache/coordination |

### File Structure on Server

```
/opt/go-emailservice-ads/
├── source/                    # Application source code
├── config.yaml                # Email service configuration
├── docker-compose.yml         # Docker stack definition
├── .env                       # Environment variables
└── backup/                    # Backup storage
    ├── backup.sh              # Automated backup script
    └── emailservice_backup_*.tar.gz

/var/lib/mail-storage/         # Mail data (WAL + Journal)
/var/log/mail/                 # Application logs
/etc/ssl/emailservice/         # SSL certificates
/etc/systemd/system/emailservice.service  # Systemd service
```

## Post-Deployment

### 1. Verify Deployment

```bash
# Check service status
ssh root@apps.afterdarksys.com systemctl status emailservice

# Check health
curl http://apps.afterdarksys.com:8080/health

# View logs
ssh root@apps.afterdarksys.com docker compose -f /opt/go-emailservice-ads/docker-compose.yml logs -f
```

### 2. Configure DNS

Add these DNS records:

```dns
# MX Record
apps.afterdarksys.com.    IN  MX  10  apps.afterdarksys.com.

# SPF Record
apps.afterdarksys.com.    IN  TXT "v=spf1 mx a ~all"

# DMARC Record
_dmarc.apps.afterdarksys.com.  IN  TXT "v=DMARC1; p=quarantine; rua=mailto:dmarc@apps.afterdarksys.com"
```

### 3. Test Email

```bash
ssh root@apps.afterdarksys.com
cd /opt/go-emailservice-ads/source/tests
python3 test_smtp.py
```

### 4. Access Monitoring

- **Grafana**: http://apps.afterdarksys.com:3000
  - Username: `admin`
  - Password: (your admin_password)

- **Prometheus**: http://apps.afterdarksys.com:9091

## Common Operations

### Update Application

```bash
cd deploy/ansible
ansible-playbook -i inventories/production.ini update.yml
```

### Manual Backup

```bash
cd deploy/ansible
ansible-playbook -i inventories/production.ini backup.yml
```

### Restore from Backup

```bash
cd deploy/ansible
ansible-playbook -i inventories/production.ini restore.yml
```

### View Logs

```bash
ssh root@apps.afterdarksys.com
docker compose -f /opt/go-emailservice-ads/docker-compose.yml logs -f mail-primary
```

### Restart Service

```bash
ssh root@apps.afterdarksys.com
systemctl restart emailservice
```

### Check Queue Stats

```bash
ssh root@apps.afterdarksys.com
docker exec mail-primary /usr/local/bin/mailctl \
  --api http://localhost:8080 \
  --username admin \
  --password YOUR_PASSWORD \
  queue stats
```

## Security Best Practices

### Use Ansible Vault for Secrets

```bash
# Create vault file
cd deploy/ansible
ansible-vault create group_vars/emailservers_vault.yml

# Add secrets:
vault_admin_password: "secure-password"
vault_postgres_password: "secure-db-password"

# Deploy with vault
ansible-playbook -i inventories/production.ini deploy.yml --ask-vault-pass
```

### Change Default Passwords

After deployment, immediately change:
1. Admin password in `config.yaml`
2. Grafana admin password
3. PostgreSQL password

## Troubleshooting

### Service won't start

```bash
# Check systemd logs
ssh root@apps.afterdarksys.com journalctl -u emailservice -f

# Check Docker logs
ssh root@apps.afterdarksys.com docker compose -f /opt/go-emailservice-ads/docker-compose.yml logs
```

### SSL certificate issues

```bash
# For Let's Encrypt, renew manually
ssh root@apps.afterdarksys.com
certbot renew --force-renewal
systemctl restart emailservice

# Or redeploy with self-signed for testing
ansible-playbook -i inventories/production.ini deploy.yml \
  -e "letsencrypt_enabled=false" --tags emailservice
```

### Can't connect to SMTP

```bash
# Check if port is open
ssh root@apps.afterdarksys.com netstat -tulpn | grep :25

# Check firewall
ssh root@apps.afterdarksys.com ufw status

# Test connection
telnet apps.afterdarksys.com 25
```

## Architecture

The deployment creates a complete email infrastructure:

```
Internet
   ↓
Firewall (UFW) - Ports 25,587,465,143,993,8080,3000,9091
   ↓
Docker Network (mailnet)
   ├── mail-primary (Email Service)
   │   ├── SMTP Server (:25, :587, :465)
   │   ├── IMAP Server (:143, :993)
   │   ├── REST API (:8080)
   │   ├── gRPC API (:50051)
   │   └── AfterSMTP (:4433, :4434)
   ├── postgres (Metadata)
   ├── redis (Cache/Coordination)
   ├── prometheus (Metrics)
   └── grafana (Dashboards)
```

## File Locations

| Purpose | Location |
|---------|----------|
| Ansible playbooks | `deploy/ansible/` |
| Inventory | `deploy/ansible/inventories/production.ini` |
| Variables | `deploy/ansible/group_vars/emailservers.yml` |
| Secrets (vault) | `deploy/ansible/group_vars/emailservers_vault.yml` |
| Roles | `deploy/ansible/roles/` |
| Quick start | `deploy/ansible/quick-start.sh` |
| Full README | `deploy/ansible/README.md` |

## Advanced Features

### Enable Elasticsearch

```yaml
# In group_vars/emailservers.yml
elasticsearch_enabled: true
elasticsearch_endpoints:
  - "http://elasticsearch.internal:9200"
```

### Enable SSO

```yaml
# In group_vars/emailservers.yml
sso_enabled: true

# In vault file
vault_ads_client_id: "your-client-id"
vault_ads_client_secret: "your-client-secret"
```

### High Availability

For multiple servers, update inventory:

```ini
[emailservers]
mail1.afterdarksys.com
mail2.afterdarksys.com
mail3.afterdarksys.com
```

Then deploy to all:
```bash
ansible-playbook -i inventories/production.ini deploy.yml
```

## Support

- **Full Documentation**: `deploy/ansible/README.md`
- **Project README**: `../../README.md`
- **Check Logs**: `ssh root@apps.afterdarksys.com docker compose logs`
- **Service Status**: `ssh root@apps.afterdarksys.com systemctl status emailservice`

## Summary

✅ **Complete automation** - Deploy entire stack with one command
✅ **Production-ready** - SSL/TLS, backups, monitoring, security hardening
✅ **Easy updates** - Update, backup, restore playbooks included
✅ **Standard ports** - Uses ports 25, 587, 465 as configured
✅ **Well documented** - Comprehensive README and guides

**Ready to deploy!** Run `./quick-start.sh` to begin.
