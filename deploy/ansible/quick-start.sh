#!/bin/bash
# Quick Start Script for Email Service Deployment
# This script helps you quickly deploy the email service to apps.afterdarksys.com

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║   Go Email Service - Ansible Deployment Quick Start         ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Check if Ansible is installed
if ! command -v ansible &> /dev/null; then
    echo -e "${RED}✗ Ansible is not installed${NC}"
    echo "Install Ansible first:"
    echo "  macOS: brew install ansible"
    echo "  Ubuntu: sudo apt install ansible"
    echo "  pip: pip3 install ansible"
    exit 1
fi

echo -e "${GREEN}✓ Ansible is installed: $(ansible --version | head -n1)${NC}"

# Check Ansible collections
echo -e "${YELLOW}→ Checking Ansible collections...${NC}"
if ! ansible-galaxy collection list | grep -q "community.docker"; then
    echo -e "${YELLOW}→ Installing community.docker collection...${NC}"
    ansible-galaxy collection install community.docker
fi

if ! ansible-galaxy collection list | grep -q "community.crypto"; then
    echo -e "${YELLOW}→ Installing community.crypto collection...${NC}"
    ansible-galaxy collection install community.crypto
fi

echo -e "${GREEN}✓ Required collections installed${NC}"

# Test connection to server
echo ""
echo -e "${YELLOW}→ Testing connection to apps.afterdarksys.com...${NC}"
if ansible -i inventories/production.ini emailservers -m ping &> /dev/null; then
    echo -e "${GREEN}✓ Connection successful${NC}"
else
    echo -e "${RED}✗ Cannot connect to server${NC}"
    echo "Make sure you have SSH access:"
    echo "  ssh root@apps.afterdarksys.com"
    exit 1
fi

# Check variables
echo ""
echo -e "${YELLOW}→ Checking configuration...${NC}"
if [ ! -f "group_vars/emailservers.yml" ]; then
    echo -e "${RED}✗ Configuration file not found${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Configuration file found${NC}"

# Display configuration
echo ""
echo -e "${YELLOW}Current Configuration:${NC}"
grep -E "^email_domain:|^admin_password:|^letsencrypt_enabled:" group_vars/emailservers.yml | sed 's/^/  /'

echo ""
echo -e "${YELLOW}⚠️  Important:${NC}"
echo "  1. Review group_vars/emailservers.yml"
echo "  2. Change admin_password and postgres_password"
echo "  3. Set letsencrypt_enabled: true for production SSL"
echo "  4. Consider using Ansible Vault for secrets"
echo ""

# Prompt for deployment
read -p "Deploy to apps.afterdarksys.com? (yes/no): " CONFIRM

if [ "$CONFIRM" != "yes" ]; then
    echo -e "${YELLOW}Deployment cancelled${NC}"
    exit 0
fi

# Run deployment
echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║              Starting Deployment                             ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""

ansible-playbook -i inventories/production.ini deploy.yml

echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║              Deployment Complete!                            ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${GREEN}Services:${NC}"
echo "  SMTP:       apps.afterdarksys.com:25"
echo "  Submission: apps.afterdarksys.com:587"
echo "  API:        http://apps.afterdarksys.com:8080"
echo "  Grafana:    http://apps.afterdarksys.com:3000"
echo ""
echo -e "${YELLOW}Next Steps:${NC}"
echo "  1. Configure DNS records (see README.md)"
echo "  2. Test email sending: ssh root@apps.afterdarksys.com 'cd /opt/go-emailservice-ads/source/tests && python3 test_smtp.py'"
echo "  3. Access Grafana: http://apps.afterdarksys.com:3000 (admin/<your-password>)"
echo "  4. Review logs: ssh root@apps.afterdarksys.com 'docker compose -f /opt/go-emailservice-ads/docker-compose.yml logs -f'"
echo ""
