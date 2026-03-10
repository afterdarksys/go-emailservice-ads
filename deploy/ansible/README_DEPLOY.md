# 🚀 Ansible Deployment - Getting Started

## TL;DR - Deploy in 3 Commands

```bash
cd deploy/ansible

# 1. Setup
./deploy.sh setup

# 2. Configure (edit admin_password and postgres_password)
vim group_vars/emailservers.yml

# 3. Deploy!
./deploy.sh deploy
```

## What You Get

✅ Complete email service stack deployed to apps.afterdarksys.com
✅ Standard SMTP ports (25, 587, 465) configured
✅ SSL/TLS with Let's Encrypt or self-signed certificates
✅ PostgreSQL, Redis, Prometheus, Grafana
✅ Automated backups with daily cron job
✅ Systemd service for auto-start and management
✅ Firewall configured with UFW
✅ Monitoring dashboards ready

## Using deploy.sh

The `deploy.sh` script is your main interface. It wraps all Ansible operations:

| Command | What It Does |
|---------|--------------|
| `./deploy.sh setup` | Install Ansible collections |
| `./deploy.sh ping` | Test connection to server |
| `./deploy.sh deploy` | Full deployment |
| `./deploy.sh update` | Update application only |
| `./deploy.sh backup` | Create backup |
| `./deploy.sh restore` | Restore from backup |
| `./deploy.sh status` | Check service status |
| `./deploy.sh logs` | View logs |
| `./deploy.sh shell` | SSH to server |
| `./deploy.sh help` | Show all commands |

## Common Tasks

### First Deployment
```bash
./deploy.sh setup     # Install Ansible deps
./deploy.sh ping      # Test connection
./deploy.sh deploy    # Deploy everything
./deploy.sh status    # Verify it's running
```

### Update Application
```bash
./deploy.sh backup --download   # Backup first
./deploy.sh update              # Update app
./deploy.sh status              # Check status
```

### Monitor Service
```bash
./deploy.sh status              # Quick status
./deploy.sh logs                # View logs
./deploy.sh logs --follow       # Follow logs in real-time
```

### Manage Secrets
```bash
./deploy.sh vault create        # Create vault
./deploy.sh vault edit          # Edit secrets
./deploy.sh deploy --vault      # Deploy with vault
```

## Configuration

Edit these files before deploying:

1. **group_vars/emailservers.yml** - Main configuration
   - Change `admin_password`
   - Change `postgres_password`
   - Set `letsencrypt_enabled: true` for production SSL

2. **inventories/production.ini** - Server list
   - Default: apps.afterdarksys.com
   - Add more servers if needed

3. **group_vars/emailservers_vault.yml** (optional) - Encrypted secrets
   - Create with: `./deploy.sh vault create`
   - Recommended for production

## Services Deployed

After deployment, these services run on apps.afterdarksys.com:

- **Port 25** - SMTP (mail reception)
- **Port 587** - Submission (authenticated sending)
- **Port 465** - SMTPS (SMTP over TLS)
- **Port 143** - IMAP (mail retrieval)
- **Port 993** - IMAPS (IMAP over TLS)
- **Port 8080** - REST API (management)
- **Port 3000** - Grafana (monitoring dashboards)
- **Port 9091** - Prometheus (metrics)

## Post-Deployment

1. **Configure DNS Records**:
   ```dns
   apps.afterdarksys.com.  IN  MX  10  apps.afterdarksys.com.
   apps.afterdarksys.com.  IN  TXT "v=spf1 mx a ~all"
   ```

2. **Access Services**:
   - API: http://apps.afterdarksys.com:8080/health
   - Grafana: http://apps.afterdarksys.com:3000 (admin/your-password)

3. **Test Email**:
   ```bash
   ssh root@apps.afterdarksys.com
   cd /opt/go-emailservice-ads/source/tests
   python3 test_smtp.py
   ```

## Documentation

- **deploy.sh.md** - Complete deploy.sh documentation
- **QUICK_REFERENCE.md** - Quick command reference
- **DEPLOYMENT_GUIDE.md** - Detailed deployment guide
- **README.md** - Full Ansible documentation

## Need Help?

```bash
./deploy.sh help        # Show all commands
./deploy.sh status      # Check service status
./deploy.sh logs -f     # View logs
./deploy.sh shell       # SSH to server
```

## Quick Reference

```bash
# Deploy
./deploy.sh deploy

# Update
./deploy.sh update

# Backup
./deploy.sh backup --download

# Restore
./deploy.sh restore --backup-file /path/to/backup.tar.gz

# Status
./deploy.sh status

# Logs
./deploy.sh logs --follow

# Help
./deploy.sh help
```

That's it! Start with `./deploy.sh setup` and go from there.
