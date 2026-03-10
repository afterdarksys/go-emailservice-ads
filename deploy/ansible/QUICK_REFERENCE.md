# Quick Reference - deploy.sh

## One-Line Commands

```bash
# Deploy everything
./deploy.sh deploy

# Update application
./deploy.sh update

# Create backup
./deploy.sh backup

# Check status
./deploy.sh status

# View logs
./deploy.sh logs

# Get help
./deploy.sh help
```

## Common Workflows

### Initial Deployment
```bash
# 1. Setup Ansible environment
./deploy.sh setup

# 2. Test connection
./deploy.sh ping

# 3. Review configuration
./deploy.sh config

# 4. Deploy
./deploy.sh deploy
```

### Update Workflow
```bash
# 1. Create backup first
./deploy.sh backup --download

# 2. Update application
./deploy.sh update

# 3. Check status
./deploy.sh status

# 4. View logs if needed
./deploy.sh logs --follow
```

### Backup & Restore
```bash
# Create backup
./deploy.sh backup

# Create and download backup
./deploy.sh backup --download

# Restore from backup
./deploy.sh restore --backup-file /path/to/backup.tar.gz
```

### Monitoring & Debugging
```bash
# Check service status
./deploy.sh status

# View recent logs
./deploy.sh logs

# Follow logs in real-time
./deploy.sh logs --follow

# View specific service
./deploy.sh logs --service mail-primary --lines 100

# Open SSH shell
./deploy.sh shell
```

### Using Ansible Vault
```bash
# Create vault file
./deploy.sh vault create

# Edit vault file
./deploy.sh vault edit

# Deploy with vault
./deploy.sh deploy --vault

# Update with vault
./deploy.sh update --vault
```

## Advanced Usage

### Dry Run (Test Mode)
```bash
# See what would change without making changes
./deploy.sh deploy --check --diff

# Test update
./deploy.sh update --check
```

### Selective Deployment
```bash
# Deploy only Docker setup
./deploy.sh deploy --tags docker

# Deploy only email service
./deploy.sh deploy --tags emailservice

# Skip backup tasks
./deploy.sh deploy --skip-tags backup
```

### Force Mode (No Prompts)
```bash
# Deploy without confirmation
./deploy.sh deploy --force

# Update without confirmation
./deploy.sh update --force
```

### Verbose Output
```bash
# Verbose mode
./deploy.sh deploy -v

# Very verbose mode
./deploy.sh deploy --vvv
```

### Override Variables
```bash
# Deploy with custom domain
./deploy.sh deploy --extra-vars "email_domain=mail.example.com"

# Deploy without Let's Encrypt
./deploy.sh deploy --extra-vars "letsencrypt_enabled=false"

# Multiple variables
./deploy.sh deploy --extra-vars "email_domain=mail.example.com letsencrypt_enabled=true"
```

## Command Reference

| Command | Description | Example |
|---------|-------------|---------|
| `deploy` | Full deployment | `./deploy.sh deploy` |
| `update` | Update app only | `./deploy.sh update` |
| `backup` | Create backup | `./deploy.sh backup --download` |
| `restore` | Restore from backup | `./deploy.sh restore --backup-file /path/to/backup.tar.gz` |
| `status` | Show service status | `./deploy.sh status` |
| `logs` | View logs | `./deploy.sh logs --follow` |
| `ping` | Test connection | `./deploy.sh ping` |
| `shell` | SSH to server | `./deploy.sh shell` |
| `setup` | Install Ansible deps | `./deploy.sh setup` |
| `vault` | Manage vault | `./deploy.sh vault edit` |
| `config` | Show config | `./deploy.sh config` |

## Option Reference

| Option | Description | Example |
|--------|-------------|---------|
| `--force` | Skip confirmations | `./deploy.sh deploy --force` |
| `--vault` | Use Ansible vault | `./deploy.sh deploy --vault` |
| `-v` | Verbose output | `./deploy.sh deploy -v` |
| `--vvv` | Very verbose | `./deploy.sh deploy --vvv` |
| `--check` | Dry run | `./deploy.sh deploy --check` |
| `--diff` | Show changes | `./deploy.sh deploy --diff` |
| `--tags <tags>` | Run specific tags | `./deploy.sh deploy --tags docker` |
| `--skip-tags <tags>` | Skip tags | `./deploy.sh deploy --skip-tags backup` |
| `--extra-vars <vars>` | Override variables | `./deploy.sh deploy --extra-vars "domain=mail.example.com"` |
| `--download` | Download backup | `./deploy.sh backup --download` |
| `--backup-file <path>` | Backup to restore | `./deploy.sh restore --backup-file /path/to/file` |
| `--service <name>` | Service for logs | `./deploy.sh logs --service mail-primary` |
| `--lines <n>` | Log lines to show | `./deploy.sh logs --lines 100` |
| `--follow` / `-f` | Follow logs | `./deploy.sh logs -f` |

## Vault Commands

| Command | Description |
|---------|-------------|
| `./deploy.sh vault create` | Create new encrypted vault |
| `./deploy.sh vault edit` | Edit encrypted vault |
| `./deploy.sh vault view` | View vault contents |
| `./deploy.sh vault encrypt` | Encrypt existing file |
| `./deploy.sh vault decrypt` | Decrypt file |

## Configuration Files

| File | Purpose |
|------|---------|
| `inventories/production.ini` | Server inventory |
| `group_vars/emailservers.yml` | Configuration variables |
| `group_vars/emailservers_vault.yml` | Encrypted secrets |
| `deploy.yml` | Main deployment playbook |
| `update.yml` | Update playbook |
| `backup.yml` | Backup playbook |
| `restore.yml` | Restore playbook |

## Troubleshooting

### Connection Issues
```bash
# Test connection
./deploy.sh ping

# If fails, test SSH manually
ssh root@apps.afterdarksys.com

# Check inventory file
cat inventories/production.ini
```

### Deployment Issues
```bash
# Run in check mode first
./deploy.sh deploy --check --diff

# Use verbose mode
./deploy.sh deploy --vvv

# Deploy specific role
./deploy.sh deploy --tags docker
```

### Service Issues
```bash
# Check status
./deploy.sh status

# View logs
./deploy.sh logs --service mail-primary --lines 200

# SSH to server
./deploy.sh shell
```

## Quick Tips

1. **Always backup before updating:**
   ```bash
   ./deploy.sh backup --download
   ./deploy.sh update
   ```

2. **Test deployments in check mode:**
   ```bash
   ./deploy.sh deploy --check --diff
   ```

3. **Use vault for secrets:**
   ```bash
   ./deploy.sh vault create
   # Add: vault_admin_password, vault_postgres_password
   ./deploy.sh deploy --vault
   ```

4. **Monitor deployments:**
   ```bash
   # In one terminal
   ./deploy.sh deploy

   # In another terminal
   ./deploy.sh logs --follow
   ```

5. **Quick status check:**
   ```bash
   ./deploy.sh status && \
   curl -s http://apps.afterdarksys.com:8080/health | jq .
   ```

## Environment Variables

Set these for non-interactive use:

```bash
# Skip confirmations
export FORCE=yes

# Use vault
export USE_VAULT=yes

# Verbose output
export VERBOSE=v
```

## Integration with CI/CD

```bash
# Example GitLab CI
deploy:
  script:
    - ./deploy.sh deploy --force --vault
  only:
    - main
```

## Need Help?

```bash
# Show detailed help
./deploy.sh help

# View README
cat README.md

# View deployment guide
cat DEPLOYMENT_GUIDE.md
```
