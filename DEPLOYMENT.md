# Deployment Guide

## Quick Deploy with Docker

### Option 1: Simple Single Instance

```bash
# Build image
./deploy.sh build

# Test locally
./deploy.sh test

# Start with Docker Compose
./deploy.sh up docker-compose.simple.yml

# Check status
./deploy.sh status

# View logs
./deploy.sh logs

# Stop
./deploy.sh down docker-compose.simple.yml
```

### Option 2: High Availability Setup

```bash
# Start primary + secondary + monitoring
./deploy.sh up docker-compose.yml

# Services started:
# - mail-primary (port 2525, 8080, 50051, 9090)
# - mail-secondary (port 2526, 8081, 50052, 9091)
# - prometheus (port 9091)
# - grafana (port 3000)
# - postgres (port 5432)
# - redis (port 6379)

# Access Grafana: http://localhost:3000 (admin/admin)
```

## Docker Commands

### Build
```bash
docker build -t afterdarksys/go-emailservice-ads:latest .
```

### Run Single Container
```bash
docker run -d \
  --name mail-server \
  -p 2525:2525 \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/opt/goemailservices/config.yaml:ro \
  -v mail-data:/var/lib/mail-storage \
  afterdarksys/go-emailservice-ads:latest
```

### Push to Registry
```bash
# Login to Docker Hub
docker login

# Push
docker push afterdarksys/go-emailservice-ads:latest
```

## Kubernetes Deployment

### Prerequisites
- Kubernetes cluster (1.20+)
- kubectl configured
- Persistent volume provider

### Deploy to Kubernetes

```bash
# Using deployment script
./deploy.sh k8s-deploy

# Or manually
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/pvc.yaml
kubectl apply -f deploy/k8s/deployment.yaml
kubectl apply -f deploy/k8s/service.yaml
```

### Verify Deployment

```bash
# Check pods
kubectl get pods -n mail-system

# Check services
kubectl get svc -n mail-system

# Check logs
kubectl logs -n mail-system -l app=go-emailservice-ads -f

# Port forward for testing
kubectl port-forward -n mail-system svc/mail-smtp 2525:25
kubectl port-forward -n mail-system svc/mail-api 8080:8080
```

### Update Deployment

```bash
# Update image
kubectl set image deployment/mail-primary \
  goemailservices=afterdarksys/go-emailservice-ads:20260308 \
  -n mail-system

# Watch rollout
kubectl rollout status deployment/mail-primary -n mail-system

# Rollback if needed
kubectl rollout undo deployment/mail-primary -n mail-system
```

## Oracle Cloud Infrastructure (OCI)

### Push to OCI Container Registry

```bash
# Login to OCI Registry
docker login <region>.ocir.io
# Username: <tenancy-namespace>/<oci-username>
# Password: <auth-token>

# Tag for OCI
docker tag afterdarksys/go-emailservice-ads:latest \
  <region>.ocir.io/<tenancy-namespace>/go-emailservice-ads:latest

# Push
docker push <region>.ocir.io/<tenancy-namespace>/go-emailservice-ads:latest
```

### Deploy to OCI Container Instances

```bash
# Create container instance
oci container-instances container-instance create \
  --compartment-id <compartment-ocid> \
  --display-name mail-server \
  --containers file://oci-container-config.json \
  --shape CI.Standard.E4.Flex \
  --shape-config '{"ocpus":2,"memoryInGBs":8}'
```

### OCI Container Config (oci-container-config.json)

```json
[
  {
    "imageUrl": "<region>.ocir.io/<namespace>/go-emailservice-ads:latest",
    "displayName": "mail-server",
    "environmentVariables": {
      "LOG_LEVEL": "info"
    },
    "volumeMounts": [
      {
        "mountPath": "/var/lib/mail-storage",
        "volumeName": "mail-data"
      }
    ]
  }
]
```

## Environment Variables

```bash
# Server
SMTP_ADDR=:2525
API_REST_ADDR=:8080
API_GRPC_ADDR=:50051

# Storage
STORAGE_PATH=/var/lib/mail-storage

# Logging
LOG_LEVEL=info  # debug, info, warn, error

# Replication
REPLICATION_MODE=primary  # primary, secondary, standby
REPLICATION_PEERS=replica1:9090,replica2:9090
```

## Monitoring

### Prometheus Metrics
```bash
# Metrics endpoint
curl http://localhost:8080/metrics

# Prometheus UI
http://localhost:9091
```

### Grafana Dashboards
```bash
# Grafana UI
http://localhost:3000

# Default credentials
Username: admin
Password: admin
```

### Health Checks
```bash
# Docker health check
docker inspect mail-server | jq '.[0].State.Health'

# Kubernetes health
kubectl get pods -n mail-system -o json | jq '.items[].status.containerStatuses[].ready'

# Manual health check
curl http://localhost:8080/health
```

## Production Checklist

### Before Production

- [ ] Change default credentials (admin/changeme)
- [ ] Configure TLS certificates
- [ ] Set up proper DNS records (MX, SPF, DKIM, DMARC)
- [ ] Configure firewall rules
- [ ] Set up log aggregation
- [ ] Configure monitoring and alerting
- [ ] Test disaster recovery procedures
- [ ] Set up automated backups
- [ ] Document runbook procedures
- [ ] Load test the deployment

### Security

```yaml
# config.yaml (production)
server:
  addr: ":25"
  domain: "mail.yourdomain.com"
  allow_insecure_auth: false  # Require TLS
  tls:
    cert: "/etc/mail/tls/fullchain.pem"
    key: "/etc/mail/tls/privkey.pem"

api:
  rest_addr: ":8080"
  grpc_addr: ":50051"
  # Add TLS for API endpoints too
```

### Resource Limits

**Docker Compose:**
```yaml
services:
  mail-primary:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G
        reservations:
          cpus: '0.5'
          memory: 512M
```

**Kubernetes:**
```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "500m"
  limits:
    memory: "2Gi"
    cpu: "2000m"
```

## Scaling

### Horizontal Scaling (Kubernetes)

```bash
# Scale deployment
kubectl scale deployment/mail-primary --replicas=5 -n mail-system

# Auto-scaling
kubectl autoscale deployment/mail-primary \
  --min=2 --max=10 \
  --cpu-percent=70 \
  -n mail-system
```

### Vertical Scaling (Docker)

```bash
# Update resources
docker update \
  --cpus=4 \
  --memory=4g \
  mail-server
```

## Backup and Recovery

### Backup Storage

```bash
# Docker volume backup
docker run --rm \
  -v mail-data:/data \
  -v $(pwd)/backups:/backup \
  alpine tar czf /backup/mail-data-$(date +%Y%m%d).tar.gz /data

# Kubernetes PVC backup
kubectl exec -n mail-system mail-primary-xxx -- \
  tar czf - /var/lib/mail-storage | \
  gzip > backup-$(date +%Y%m%d).tar.gz
```

### Restore

```bash
# Docker volume restore
docker run --rm \
  -v mail-data:/data \
  -v $(pwd)/backups:/backup \
  alpine tar xzf /backup/mail-data-20260308.tar.gz -C /

# Kubernetes restore
cat backup-20260308.tar.gz | \
  kubectl exec -i -n mail-system mail-primary-xxx -- \
  tar xzf - -C /
```

## Troubleshooting

### Container Won't Start

```bash
# Check logs
docker logs mail-server

# Check container details
docker inspect mail-server

# Exec into container
docker exec -it mail-server /bin/bash

# Check file permissions
docker exec mail-server ls -la /var/lib/mail-storage
```

### Pod CrashLoopBackOff

```bash
# Get pod logs
kubectl logs -n mail-system <pod-name>

# Get previous logs
kubectl logs -n mail-system <pod-name> --previous

# Describe pod
kubectl describe pod -n mail-system <pod-name>

# Debug with ephemeral container
kubectl debug -n mail-system <pod-name> -it --image=busybox
```

### Performance Issues

```bash
# Check resource usage (Docker)
docker stats mail-server

# Check resource usage (Kubernetes)
kubectl top pods -n mail-system

# Check queue depths
./bin/mailctl --api http://localhost:8080 \
  --username admin --password changeme \
  queue stats
```

## Migration

### From Postfix

1. Export Postfix queue:
```bash
postqueue -p > queue-backup.txt
```

2. Stop Postfix:
```bash
systemctl stop postfix
```

3. Start go-emailservice-ads
4. Import messages (manual process - TBD)

### Zero-Downtime Migration

1. Deploy go-emailservice-ads alongside Postfix
2. Update MX records to point to new server (low priority)
3. Monitor both servers
4. Increase priority of new MX record
5. After 24-48 hours, decommission Postfix

## Useful Commands

```bash
# Deploy script commands
./deploy.sh build          # Build image
./deploy.sh push           # Push to registry
./deploy.sh test           # Test locally
./deploy.sh up             # Start services
./deploy.sh down           # Stop services
./deploy.sh logs           # View logs
./deploy.sh clean          # Clean up
./deploy.sh full-deploy    # Build + test + push
./deploy.sh k8s-deploy     # Deploy to Kubernetes
./deploy.sh status         # Show status

# Docker Compose
docker-compose -f docker-compose.yml ps
docker-compose -f docker-compose.yml logs -f
docker-compose -f docker-compose.yml restart mail-primary

# Kubernetes
kubectl get all -n mail-system
kubectl logs -n mail-system -l app=go-emailservice-ads -f
kubectl exec -n mail-system -it <pod> -- /bin/bash
```

## Support

For issues or questions:
- Check logs first
- Review DISASTER_RECOVERY.md for architecture
- Check QUICKSTART.md for configuration
- Review TEST_RESULTS.md for expected behavior
