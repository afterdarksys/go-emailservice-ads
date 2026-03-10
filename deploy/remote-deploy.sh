#!/bin/bash
# Simple remote deployment script for apps.afterdarksys.com
# Bypasses Ansible Python compatibility issues

set -e

SERVER="apps.afterdarksys.com"
USER="root"
DOMAIN="apps.afterdarksys.com"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║   Deploying Email Service to apps.afterdarksys.com          ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Confirm deployment
read -p "Deploy to production? (yes/no): " confirm
if [ "$confirm" != "yes" ]; then
    echo -e "${YELLOW}Deployment cancelled${NC}"
    exit 0
fi

echo -e "${YELLOW}→ Installing Docker...${NC}"
ssh $USER@$SERVER 'bash -s' << 'ENDSSH'
# Install Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh
systemctl start docker
systemctl enable docker
ENDSSH

echo -e "${GREEN}✓ Docker installed${NC}"

echo -e "${YELLOW}→ Creating directories...${NC}"
ssh $USER@$SERVER "mkdir -p /opt/go-emailservice-ads/{data/certs,logs,source}"

echo -e "${YELLOW}→ Uploading files...${NC}"
cd /Users/ryan/development/go-emailservice-ads
rsync -av --exclude='.git' --exclude='data' --exclude='bin' --exclude='*.log' --exclude='deploy/ansible' . $USER@$SERVER:/opt/go-emailservice-ads/source/

echo -e "${YELLOW}→ Building Docker image...${NC}"
ssh $USER@$SERVER 'cd /opt/go-emailservice-ads/source && docker build -t afterdarksys/go-emailservice-ads:latest .'

echo -e "${YELLOW}→ Creating docker-compose.yml for production...${NC}"
ssh $USER@$SERVER 'cat > /opt/go-emailservice-ads/docker-compose.yml' << 'ENDCOMPOSE'
version: '3.8'

services:
  mail-primary:
    image: afterdarksys/go-emailservice-ads:latest
    container_name: mail-primary
    hostname: apps.afterdarksys.com
    ports:
      - "25:2525"          # SMTP
      - "587:2525"         # Submission
      - "465:2525"         # SMTPS
      - "8080:8080"        # REST API
      - "50051:50051"      # gRPC
      - "4434:4434/udp"    # AfterSMTP QUIC
      - "4433:4433"        # AfterSMTP gRPC
    volumes:
      - /opt/go-emailservice-ads/data:/var/lib/mail-storage
      - /opt/go-emailservice-ads/logs:/var/log/mail
      - /opt/go-emailservice-ads/config.yaml:/opt/goemailservices/config.yaml:ro
    environment:
      - LOG_LEVEL=info
    restart: unless-stopped
    networks:
      - mailnet

  postgres:
    image: postgres:16-alpine
    container_name: mail-postgres
    ports:
      - "5432:5432"
    volumes:
      - postgres-data:/var/lib/postgresql/data
    environment:
      - POSTGRES_DB=maildb
      - POSTGRES_USER=mailuser
      - POSTGRES_PASSWORD=P0stgr3s!Secure2026
    restart: unless-stopped
    networks:
      - mailnet

  redis:
    image: redis:7-alpine
    container_name: mail-redis
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    command: redis-server --appendonly yes
    restart: unless-stopped
    networks:
      - mailnet

  prometheus:
    image: prom/prometheus:latest
    container_name: mail-prometheus
    ports:
      - "9091:9090"
    volumes:
      - prometheus-data:/prometheus
    restart: unless-stopped
    networks:
      - mailnet

  grafana:
    image: grafana/grafana:latest
    container_name: mail-grafana
    ports:
      - "3000:3000"
    volumes:
      - grafana-data:/var/lib/grafana
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=Admin!Secure2026
      - GF_USERS_ALLOW_SIGN_UP=false
    restart: unless-stopped
    networks:
      - mailnet

volumes:
  postgres-data:
  redis-data:
  prometheus-data:
  grafana-data:

networks:
  mailnet:
    driver: bridge
ENDCOMPOSE

echo -e "${YELLOW}→ Starting services...${NC}"
ssh $USER@$SERVER 'cd /opt/go-emailservice-ads && docker compose up -d'

echo -e "${YELLOW}→ Waiting for services to start...${NC}"
sleep 10

echo -e "${YELLOW}→ Checking health...${NC}"
if ssh $USER@$SERVER 'curl -f http://localhost:8080/health'; then
    echo -e "${GREEN}✓ Services are healthy!${NC}"
else
    echo -e "${RED}✗ Health check failed${NC}"
    ssh $USER@$SERVER 'docker compose logs mail-primary'
    exit 1
fi

echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║              Deployment Complete!                            ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${GREEN}Services:${NC}"
echo "  SMTP:       apps.afterdarksys.com:25"
echo "  Submission: apps.afterdarksys.com:587"
echo "  API:        http://apps.afterdarksys.com:8080"
echo "  Grafana:    http://apps.afterdarksys.com:3000 (admin/Admin!Secure2026)"
echo ""
echo -e "${YELLOW}Next Steps:${NC}"
echo "  1. Configure DNS MX record"
echo "  2. Test email sending"
echo "  3. Access Grafana dashboard"
echo ""
ENDSSH
chmod +x /Users/ryan/development/go-emailservice-ads/deploy/remote-deploy.sh
