# Ansible Deployment for Go Email Service

This directory contains Ansible playbooks and roles to deploy the Go Email Service stack to apps.afterdarksys.com.

## Prerequisites

### On Control Machine (Your Local Machine)

1. **Install Ansible**:
   ```bash
   # macOS
   brew install ansible

   # Ubuntu/Debian
   sudo apt update
   sudo apt install ansible

   # Python pip
   pip3 install ansible
   ```

2. **Install Ansible Collections**:
   ```bash
   ansible-galaxy collection install community.docker
   ansible-galaxy collection install community.crypto
   ```

3. **Verify Installation**:
   ```bash
   ansible --version
   # Should show version 2.10 or higher
   ```

### On Target Server (apps.afterdarksys.com)

1. **SSH Access**: Ensure you have SSH access to the server
   ```bash
   ssh root@apps.afterdarksys.com
   ```

2. **Python**: Ensure Python 3 is installed
   ```bash
   python3 --version
   ```

## Quick Start

### 1. Configure Inventory

Edit `inventories/production.ini` and update if needed:
```ini
[emailservers]
apps.afterdarksys.com ansible_user=root ansible_port=22
```

### 2. Configure Variables

Edit `group_vars/emailservers.yml` to customize deployment:

```yaml
# Key variables to review:
email_domain: "apps.afterdarksys.com"
admin_password: "your-secure-password-here"
postgres_password: "your-postgres-password-here"

# Ports (standard SMTP ports are available)
smtp_port: 25
submission_port: 587
smtps_port: 465

# SSL/TLS
letsencrypt_enabled: true  # Set to true for production
letsencrypt_email: "admin@apps.afterdarksys.com"
```

### 3. Test Connection

```bash
ansible -i inventories/production.ini emailservers -m ping
```

Expected output:
```
apps.afterdarksys.com | SUCCESS => {
    "ping": "pong"
}
```

### 4. Deploy

```bash
# Full deployment
ansible-playbook -i inventories/production.ini deploy.yml

# Or with vault for sensitive data
ansible-playbook -i inventories/production.ini deploy.yml --ask-vault-pass
```

The playbook will:
- Install and configure Docker
- Set up firewall rules
- Create SSL certificates
- Deploy the email service stack
- Configure monitoring (Prometheus + Grafana)
- Set up automated backups
- Configure systemd service

### 5. Verify Deployment

```bash
# Check service status
ssh root@apps.afterdarksys.com systemctl status emailservice

# Check health endpoint
curl http://apps.afterdarksys.com:8080/health

# View logs
ssh root@apps.afterdarksys.com docker compose -f /opt/go-emailservice-ads/docker-compose.yml logs -f
```

## Deployment Options

### Deploy Specific Roles

```bash
# Only install Docker
ansible-playbook -i inventories/production.ini deploy.yml --tags docker

# Only deploy application
ansible-playbook -i inventories/production.ini deploy.yml --tags emailservice

# Setup only, no deploy
ansible-playbook -i inventories/production.ini deploy.yml --tags setup
```

### Deploy with Custom Variables

```bash
# Override domain
ansible-playbook -i inventories/production.ini deploy.yml \
  -e "email_domain=mail.example.com"

# Deploy without Let's Encrypt
ansible-playbook -i inventories/production.ini deploy.yml \
  -e "letsencrypt_enabled=false"
```

## Additional Playbooks

### Update Deployment

Update the application without changing infrastructure:

```bash
ansible-playbook -i inventories/production.ini update.yml
```

### Backup

Create a manual backup:

```bash
ansible-playbook -i inventories/production.ini backup.yml
```

### Restore from Backup

```bash
ansible-playbook -i inventories/production.ini restore.yml \
  -e "backup_file=/opt/go-emailservice-ads/backup/emailservice_backup_20260309_120000.tar.gz"
```

### Rollback

Rollback to previous version:

```bash
ansible-playbook -i inventories/production.ini rollback.yml
```

## Security Best Practices

### Use Ansible Vault for Secrets

1. **Create a vault file**:
   ```bash
   ansible-vault create group_vars/emailservers_vault.yml
   ```

2. **Add sensitive variables**:
   ```yaml
   vault_admin_password: "secure-admin-password"
   vault_postgres_password: "secure-db-password"
   vault_ads_client_id: "sso-client-id"
   vault_ads_client_secret: "sso-client-secret"
   vault_es_api_key: "elasticsearch-api-key"
   ```

3. **Deploy with vault**:
   ```bash
   ansible-playbook -i inventories/production.ini deploy.yml --ask-vault-pass
   ```

### SSH Key Authentication

1. **Copy SSH key to server**:
   ```bash
   ssh-copy-id root@apps.afterdarksys.com
   ```

2. **Update inventory**:
   ```ini
   [emailservers]
   apps.afterdarksys.com ansible_user=root ansible_ssh_private_key_file=~/.ssh/id_rsa
   ```

## Configuration Files

### Directory Structure

```
deploy/ansible/
├── README.md                          # This file
├── deploy.yml                         # Main deployment playbook
├── update.yml                         # Update playbook (optional)
├── backup.yml                         # Backup playbook (optional)
├── restore.yml                        # Restore playbook (optional)
├── inventories/
│   └── production.ini                 # Server inventory
├── group_vars/
│   ├── emailservers.yml               # Main variables
│   └── emailservers_vault.yml         # Encrypted secrets (optional)
└── roles/
    ├── common/                        # Common system setup
    │   ├── tasks/main.yml
    │   └── ...
    ├── docker/                        # Docker installation
    │   ├── tasks/main.yml
    │   ├── handlers/main.yml
    │   └── ...
    └── emailservice/                  # Email service deployment
        ├── tasks/main.yml
        ├── handlers/main.yml
        ├── templates/
        │   ├── config.yaml.j2
        │   ├── docker-compose.production.yml.j2
        │   ├── env.j2
        │   ├── emailservice.service.j2
        │   └── backup.sh.j2
        └── ...
```

### Key Configuration Files

#### group_vars/emailservers.yml
Main configuration variables for the deployment. Edit this file to customize:
- Domain names
- Port mappings
- SSL/TLS settings
- Feature flags (Elasticsearch, SSO, AfterSMTP)
- Security settings
- Database credentials

#### inventories/production.ini
Server inventory defining which hosts to deploy to.

#### roles/emailservice/templates/
Jinja2 templates for configuration files:
- `config.yaml.j2` - Email service configuration
- `docker-compose.production.yml.j2` - Docker Compose stack
- `emailservice.service.j2` - Systemd service definition
- `backup.sh.j2` - Automated backup script

## Post-Deployment

### Access Services

- **SMTP**: `apps.afterdarksys.com:25` (SMTP)
- **Submission**: `apps.afterdarksys.com:587` (Authenticated submission)
- **SMTPS**: `apps.afterdarksys.com:465` (SMTP over TLS)
- **IMAP**: `apps.afterdarksys.com:143` (IMAP)
- **IMAPS**: `apps.afterdarksys.com:993` (IMAP over TLS)
- **API**: `http://apps.afterdarksys.com:8080` (REST API)
- **Grafana**: `http://apps.afterdarksys.com:3000` (Monitoring)
- **Prometheus**: `http://apps.afterdarksys.com:9091` (Metrics)

### Default Credentials

- **Admin User**: admin
- **Admin Password**: Set in `group_vars/emailservers.yml`
- **Grafana**: admin / (same as admin_password)

**⚠️ IMPORTANT**: Change default passwords immediately after deployment!

### Configure DNS Records

Add the following DNS records for your domain:

```dns
# MX Record
apps.afterdarksys.com.    IN  MX  10  apps.afterdarksys.com.

# A Record (if not already exists)
apps.afterdarksys.com.    IN  A   <server-ip>

# SPF Record
apps.afterdarksys.com.    IN  TXT "v=spf1 mx a ~all"

# DMARC Record
_dmarc.apps.afterdarksys.com.  IN  TXT "v=DMARC1; p=quarantine; rua=mailto:dmarc@apps.afterdarksys.com"
```

### Test Email Sending

```bash
# Using the provided test script
ssh root@apps.afterdarksys.com
cd /opt/go-emailservice-ads/source/tests
python3 test_smtp.py
```

### Monitor Service

```bash
# View real-time logs
ssh root@apps.afterdarksys.com
docker compose -f /opt/go-emailservice-ads/docker-compose.yml logs -f mail-primary

# Check queue stats
docker exec mail-primary /usr/local/bin/mailctl --api http://localhost:8080 \
  --username admin --password <your-password> queue stats

# View Grafana dashboards
# Open: http://apps.afterdarksys.com:3000
```

## Troubleshooting

### Service Won't Start

```bash
# Check systemd status
systemctl status emailservice

# Check Docker logs
docker compose -f /opt/go-emailservice-ads/docker-compose.yml logs

# Check if ports are in use
netstat -tulpn | grep -E ':(25|587|465|143|993|8080)'
```

### SSL Certificate Issues

```bash
# Check certificates
ls -la /etc/ssl/emailservice/

# Renew Let's Encrypt manually
certbot renew --force-renewal
systemctl restart emailservice

# Use self-signed for testing
ansible-playbook -i inventories/production.ini deploy.yml \
  -e "letsencrypt_enabled=false" --tags emailservice
```

### Can't Connect to Services

```bash
# Check firewall
ufw status

# Check Docker network
docker network inspect go-emailservice-ads_mailnet

# Check service health
curl http://localhost:8080/health
```

### Revert to Previous Deployment

```bash
# Stop service
systemctl stop emailservice

# Restore from backup
cd /opt/go-emailservice-ads/backup
tar -xzf emailservice_backup_<timestamp>.tar.gz

# Restore and restart
# (Use restore.yml playbook for full restore)
```

## Maintenance

### Update Application

```bash
# Pull latest code and redeploy
ansible-playbook -i inventories/production.ini deploy.yml --tags emailservice
```

### View Backups

```bash
ssh root@apps.afterdarksys.com
ls -lh /opt/go-emailservice-ads/backup/
```

### Manual Backup

```bash
ssh root@apps.afterdarksys.com
/opt/go-emailservice-ads/backup/backup.sh
```

### Check Disk Usage

```bash
ssh root@apps.afterdarksys.com
df -h /var/lib/mail-storage
du -sh /var/lib/mail-storage/*
```

### Cleanup Old Docker Images

```bash
ssh root@apps.afterdarksys.com
docker system prune -a --volumes
```

## Advanced Configuration

### Enable Elasticsearch

1. **Install Elasticsearch** (on same or separate server)

2. **Update variables**:
   ```yaml
   elasticsearch_enabled: true
   elasticsearch_endpoints:
     - "http://elasticsearch.internal:9200"
   ```

3. **Deploy**:
   ```bash
   ansible-playbook -i inventories/production.ini deploy.yml --tags emailservice
   ```

### Enable SSO

1. **Configure SSO variables**:
   ```yaml
   sso_enabled: true
   sso_provider: "afterdarksystems"
   ```

2. **Add credentials to vault**:
   ```yaml
   vault_ads_client_id: "your-client-id"
   vault_ads_client_secret: "your-client-secret"
   ```

3. **Deploy**:
   ```bash
   ansible-playbook -i inventories/production.ini deploy.yml \
     --ask-vault-pass --tags emailservice
   ```

### High Availability Setup

For HA deployment with multiple servers:

1. **Update inventory**:
   ```ini
   [emailservers]
   mail1.afterdarksys.com
   mail2.afterdarksys.com
   mail3.afterdarksys.com
   ```

2. **Configure load balancer** (HAProxy, NGINX, etc.)

3. **Deploy to all servers**:
   ```bash
   ansible-playbook -i inventories/production.ini deploy.yml
   ```

## Support

For issues or questions:
- Check logs: `/var/log/mail/`
- Review documentation: `../../README.md`
- Check service status: `systemctl status emailservice`
- View container logs: `docker compose logs`

## License

Internal use only - After Dark Systems infrastructure
