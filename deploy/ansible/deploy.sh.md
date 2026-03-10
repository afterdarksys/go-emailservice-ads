# deploy.sh - Ansible Wrapper Script

## Overview

`deploy.sh` is a comprehensive wrapper script that provides a simple CLI interface to all Ansible deployment operations. It replaces the need to remember complex `ansible-playbook` commands.

## Features

✅ **Simple commands** - `./deploy.sh deploy` instead of `ansible-playbook -i inventories/production.ini deploy.yml`
✅ **Built-in safety** - Confirmation prompts for destructive operations
✅ **Pre-flight checks** - Tests connection and prerequisites before deployment
✅ **Vault support** - Easy Ansible vault integration
✅ **Flexible options** - Dry-run, verbose, tags, and more
✅ **Multiple commands** - Deploy, update, backup, restore, status, logs, etc.
✅ **Color output** - Easy-to-read colored terminal output

## Installation

Already included! Just make it executable:

```bash
cd deploy/ansible
chmod +x deploy.sh
```

## Quick Start

```bash
# Setup Ansible environment
./deploy.sh setup

# Test connection
./deploy.sh ping

# Deploy everything
./deploy.sh deploy

# Check status
./deploy.sh status
```

## All Commands

### Deployment Commands

```bash
# Full deployment
./deploy.sh deploy

# Update application only
./deploy.sh update

# Deploy without confirmation
./deploy.sh deploy --force

# Dry run (see what would change)
./deploy.sh deploy --check --diff

# Deploy with verbose output
./deploy.sh deploy -v

# Deploy specific components
./deploy.sh deploy --tags docker
./deploy.sh deploy --tags emailservice
```

### Backup & Restore

```bash
# Create backup
./deploy.sh backup

# Create and download backup locally
./deploy.sh backup --download

# Restore from backup
./deploy.sh restore --backup-file /path/to/backup.tar.gz

# Restore without confirmation (dangerous!)
./deploy.sh restore --backup-file /path/to/backup.tar.gz --force
```

### Monitoring & Debugging

```bash
# Check service status
./deploy.sh status

# View logs (last 50 lines)
./deploy.sh logs

# View more lines
./deploy.sh logs --lines 200

# Follow logs in real-time
./deploy.sh logs --follow
./deploy.sh logs -f

# View specific service logs
./deploy.sh logs --service mail-primary
./deploy.sh logs --service postgres --lines 100
```

### Connection & Testing

```bash
# Test connection to server
./deploy.sh ping

# Open SSH shell
./deploy.sh shell
```

### Setup & Configuration

```bash
# Install Ansible collections
./deploy.sh setup

# View current configuration
./deploy.sh config
```

### Vault Management

```bash
# Create new encrypted vault file
./deploy.sh vault create

# Edit encrypted vault file
./deploy.sh vault edit

# View vault contents
./deploy.sh vault view

# Encrypt existing file
./deploy.sh vault encrypt

# Decrypt file
./deploy.sh vault decrypt

# Deploy using vault
./deploy.sh deploy --vault
```

### Help

```bash
# Show detailed help
./deploy.sh help

# Show help for specific command
./deploy.sh deploy --help
```

## Command Options

### Global Options

| Option | Description | Example |
|--------|-------------|---------|
| `--force` | Skip confirmation prompts | `./deploy.sh deploy --force` |
| `--vault` | Use Ansible vault (prompt for password) | `./deploy.sh deploy --vault` |
| `-v`, `--verbose` | Verbose output | `./deploy.sh deploy -v` |
| `--vvv` | Very verbose output (debug) | `./deploy.sh deploy --vvv` |
| `--check` | Dry-run mode (don't make changes) | `./deploy.sh deploy --check` |
| `--diff` | Show differences | `./deploy.sh deploy --diff` |
| `--tags <tags>` | Run only specific tags | `./deploy.sh deploy --tags docker` |
| `--skip-tags <tags>` | Skip specific tags | `./deploy.sh deploy --skip-tags backup` |
| `--extra-vars <vars>` | Override variables | `./deploy.sh deploy --extra-vars "domain=mail.example.com"` |

### Backup Options

| Option | Description | Example |
|--------|-------------|---------|
| `--download` | Download backup to local machine | `./deploy.sh backup --download` |

### Restore Options

| Option | Description | Example |
|--------|-------------|---------|
| `--backup-file <path>` | Backup file to restore from | `./deploy.sh restore --backup-file /path/to/backup.tar.gz` |

### Log Options

| Option | Description | Example |
|--------|-------------|---------|
| `--service <name>` | Service to show logs from | `./deploy.sh logs --service mail-primary` |
| `--lines <n>` | Number of lines to show | `./deploy.sh logs --lines 100` |
| `--follow`, `-f` | Follow logs in real-time | `./deploy.sh logs -f` |

## Common Workflows

### Initial Deployment

```bash
# 1. Install Ansible and collections
./deploy.sh setup

# 2. Test connection
./deploy.sh ping

# 3. Review configuration
./deploy.sh config

# 4. Edit variables if needed
vim group_vars/emailservers.yml

# 5. Create vault for secrets (optional but recommended)
./deploy.sh vault create

# 6. Deploy!
./deploy.sh deploy --vault
```

### Update Workflow

```bash
# 1. Create backup first (always!)
./deploy.sh backup --download

# 2. Test update in dry-run mode
./deploy.sh update --check --diff

# 3. Update application
./deploy.sh update

# 4. Check status
./deploy.sh status

# 5. Monitor logs
./deploy.sh logs --follow
```

### Debugging Issues

```bash
# Check service status
./deploy.sh status

# View recent logs
./deploy.sh logs --lines 200

# Follow logs in real-time
./deploy.sh logs --follow

# Check specific service
./deploy.sh logs --service mail-primary --lines 100

# SSH to server for manual inspection
./deploy.sh shell
```

### Disaster Recovery

```bash
# Restore from backup
./deploy.sh restore --backup-file /path/to/backup.tar.gz

# If restore fails, check logs
./deploy.sh logs

# Or SSH to investigate
./deploy.sh shell
```

## Advanced Usage

### Using with CI/CD

```bash
# Non-interactive deployment for CI/CD
./deploy.sh deploy --force --vault

# With vault password file
echo "vault_password" > .vault_pass
chmod 600 .vault_pass
./deploy.sh deploy --force
```

### Selective Deployment

```bash
# Deploy only common setup
./deploy.sh deploy --tags common

# Deploy only Docker
./deploy.sh deploy --tags docker

# Deploy only email service
./deploy.sh deploy --tags emailservice

# Deploy common and Docker, skip email service
./deploy.sh deploy --tags "common,docker"
```

### Variable Overrides

```bash
# Deploy with custom domain
./deploy.sh deploy --extra-vars "email_domain=mail.example.com"

# Deploy without Let's Encrypt
./deploy.sh deploy --extra-vars "letsencrypt_enabled=false"

# Multiple overrides
./deploy.sh deploy --extra-vars "email_domain=mail.example.com letsencrypt_enabled=true admin_password=newpass123"
```

### Testing Changes

```bash
# Dry run - see what would change without making changes
./deploy.sh deploy --check

# Dry run with detailed diff
./deploy.sh deploy --check --diff

# Test with verbose output
./deploy.sh deploy --check --diff --vvv
```

## Environment Variables

Set these for non-interactive use:

```bash
# Skip all confirmations
export FORCE=yes
./deploy.sh deploy

# Use vault automatically
export USE_VAULT=yes
./deploy.sh deploy

# Verbose output
export VERBOSE=v
./deploy.sh deploy
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error (check error message) |

## Internal Workflow

When you run `./deploy.sh deploy`, it:

1. ✅ Checks if Ansible is installed
2. ✅ Checks if required collections are installed
3. ✅ Tests connection to server
4. ✅ Shows current configuration
5. ✅ Prompts for confirmation (unless --force)
6. ✅ Runs ansible-playbook with appropriate options
7. ✅ Shows deployment summary

## Configuration Files

The script uses these configuration files:

| File | Purpose |
|------|---------|
| `ansible.cfg` | Ansible configuration |
| `inventories/production.ini` | Server inventory |
| `group_vars/emailservers.yml` | Configuration variables |
| `group_vars/emailservers_vault.yml` | Encrypted secrets (optional) |
| `.vault_pass` | Vault password file (optional) |

## Comparison: deploy.sh vs ansible-playbook

### Without deploy.sh:
```bash
ansible-playbook -i inventories/production.ini \
  --vault-password-file .vault_pass \
  --tags emailservice \
  --extra-vars "email_domain=mail.example.com" \
  --check --diff -vvv \
  deploy.yml
```

### With deploy.sh:
```bash
./deploy.sh deploy --vault --tags emailservice \
  --extra-vars "email_domain=mail.example.com" \
  --check --diff --vvv
```

Much simpler! Plus deploy.sh adds:
- ✅ Pre-flight checks
- ✅ Connection testing
- ✅ Confirmation prompts
- ✅ Color output
- ✅ Error checking

## Troubleshooting

### "Ansible is not installed"
```bash
# Install Ansible
brew install ansible  # macOS
sudo apt install ansible  # Ubuntu
pip3 install ansible  # Python
```

### "Cannot connect to server"
```bash
# Test SSH manually
ssh root@apps.afterdarksys.com

# Check inventory
cat inventories/production.ini

# Update SSH config if needed
vim ~/.ssh/config
```

### "Collection not found"
```bash
# Install missing collections
./deploy.sh setup

# Or manually
ansible-galaxy collection install community.docker
ansible-galaxy collection install community.crypto
```

### "Vault password required"
```bash
# Create vault password file
echo "your-vault-password" > .vault_pass
chmod 600 .vault_pass

# Or use --vault to prompt
./deploy.sh deploy --vault
```

## Tips & Best Practices

1. **Always backup before updates:**
   ```bash
   ./deploy.sh backup --download && ./deploy.sh update
   ```

2. **Test in dry-run mode first:**
   ```bash
   ./deploy.sh deploy --check --diff
   ```

3. **Use vault for secrets:**
   ```bash
   ./deploy.sh vault create
   ./deploy.sh deploy --vault
   ```

4. **Monitor deployments:**
   ```bash
   # Terminal 1
   ./deploy.sh deploy

   # Terminal 2
   ./deploy.sh logs --follow
   ```

5. **Keep backups locally:**
   ```bash
   ./deploy.sh backup --download
   # Backups saved to: ./backups/apps.afterdarksys.com/
   ```

## See Also

- **Full Documentation**: `README.md`
- **Quick Reference**: `QUICK_REFERENCE.md`
- **Deployment Guide**: `DEPLOYMENT_GUIDE.md`
- **Ansible Playbooks**: `deploy.yml`, `update.yml`, `backup.yml`, `restore.yml`

## Summary

`deploy.sh` provides a simple, safe, and powerful interface to deploy and manage your email service. It wraps all Ansible operations with:

- ✅ Simple commands
- ✅ Safety checks
- ✅ Helpful output
- ✅ Flexible options
- ✅ Complete functionality

Just run `./deploy.sh help` to get started!
