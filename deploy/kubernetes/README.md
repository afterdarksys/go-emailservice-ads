# Kubernetes Deployment Guide

## Overview

This directory contains Kubernetes manifests for deploying the enterprise SMTP platform in three modes:
- **Perimeter MTA**: Internet-facing mail servers with heavy security
- **Internal Hub**: Internal mail routing for trusted traffic
- **Hybrid**: Combined perimeter + internal in a single deployment

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Kubernetes Cluster                       │
│                                                              │
│  ┌──────────────┐         ┌──────────────┐                 │
│  │  Perimeter   │────────▶│  Internal    │                 │
│  │  MTA Pods    │         │  Hub Pods    │                 │
│  │ (LoadBalancer)│         │ (ClusterIP)  │                 │
│  └──────────────┘         └──────────────┘                 │
│         │                         │                          │
│         │                         │                          │
│         └─────────────┬───────────┘                          │
│                       │                                      │
│                  ┌────▼─────┐                               │
│                  │   etcd   │                               │
│                  │ StatefulSet│                             │
│                  └──────────┘                               │
└─────────────────────────────────────────────────────────────┘
```

## Prerequisites

1. **Kubernetes Cluster** (v1.28+)
   - AWS EKS, GKE, AKS, or self-managed
   - Multi-zone deployment recommended

2. **kubectl** configured and authenticated

3. **TLS Certificates**
   ```bash
   kubectl create secret tls smtp-tls \
     --cert=path/to/tls.crt \
     --key=path/to/tls.key \
     -n mail-system
   ```

4. **etcd Cluster** (for global state coordination)
   ```bash
   # Install etcd operator
   kubectl apply -f https://github.com/coreos/etcd-operator/releases/download/v0.10.3/etcd-operator.yaml

   # Create etcd cluster
   kubectl apply -f - <<EOF
   apiVersion: etcd.database.coreos.com/v1beta2
   kind: EtcdCluster
   metadata:
     name: etcd
     namespace: mail-system
   spec:
     size: 3
     version: "3.5.9"
   EOF
   ```

## Deployment Steps

### Option 1: Perimeter MTA Only

Deploy internet-facing mail servers:

```bash
# 1. Create namespace and base resources
kubectl apply -f base/namespace.yaml
kubectl apply -f base/serviceaccount.yaml
kubectl apply -f base/configmap.yaml
kubectl apply -f base/networkpolicy.yaml

# 2. Deploy perimeter MTA
kubectl apply -f perimeter/deployment.yaml
kubectl apply -f perimeter/service.yaml
kubectl apply -f perimeter/hpa.yaml

# 3. Verify deployment
kubectl get pods -n mail-system -l component=perimeter-mta
kubectl get svc -n mail-system smtp-perimeter

# 4. Get external IP
kubectl get svc smtp-perimeter -n mail-system -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
```

### Option 2: Internal Hub Only

Deploy internal mail routing:

```bash
# 1. Create namespace and base resources
kubectl apply -f base/

# 2. Deploy internal hub
kubectl apply -f internal/deployment.yaml
kubectl apply -f internal/service.yaml

# 3. Verify
kubectl get pods -n mail-system -l component=internal-hub
```

### Option 3: Hybrid Deployment

Deploy both perimeter and internal:

```bash
# 1. Base resources
kubectl apply -f base/

# 2. Deploy both modes
kubectl apply -f perimeter/
kubectl apply -f internal/

# 3. Verify both components
kubectl get pods -n mail-system
```

## Multi-Region Deployment

For global routing across regions:

### 1. Deploy to Each Region

```bash
# Set region-specific context
export REGION=us-east-1

# Update ConfigMap with region info
sed -i "s/REGION_PLACEHOLDER/$REGION/g" base/configmap.yaml

# Deploy
kubectl apply -f base/
kubectl apply -f perimeter/
kubectl apply -f internal/

# Repeat for each region (us-west-1, eu-west-1, ap-south-1, etc.)
```

### 2. Configure Global DNS

Use GeoDNS or latency-based routing:

**AWS Route53 Example**:
```json
{
  "Type": "A",
  "Name": "mail.example.com",
  "GeoLocation": {
    "ContinentCode": "NA"
  },
  "AliasTarget": {
    "DNSName": "<us-loadbalancer-dns>",
    "EvaluateTargetHealth": true
  }
}
```

### 3. Enable Global Routing

Set environment variables in deployment:
```yaml
- name: ENABLE_GLOBAL_ROUTING
  value: "true"
- name: STATE_STORE_TYPE
  value: "etcd"
- name: STATE_STORE_ENDPOINTS
  value: "global-etcd:2379"
```

## Monitoring

### Prometheus Integration

The service exposes metrics on port 8080:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: smtp-metrics
  namespace: mail-system
spec:
  selector:
    matchLabels:
      app: smtp
  endpoints:
  - port: metrics
    interval: 30s
```

### Key Metrics

- `smtp_connections_total` - Total connections
- `smtp_messages_received_total` - Messages received
- `smtp_messages_delivered_total` - Messages delivered
- `smtp_queue_depth` - Queue depth by tier
- `smtp_cross_region_transfers_total` - Cross-region transfers
- `smtp_policy_evaluations_total` - Policy evaluations

## Scaling

### Horizontal Pod Autoscaling

HPA is configured to scale based on:
- CPU utilization (70%)
- Memory utilization (80%)
- Queue depth (avg 100 messages)
- Active connections (avg 50)

```bash
# View HPA status
kubectl get hpa -n mail-system

# Manual scaling
kubectl scale deployment smtp-perimeter --replicas=10 -n mail-system
```

### Vertical Pod Autoscaling

```bash
# Install VPA
kubectl apply -f https://github.com/kubernetes/autoscaler/releases/download/vertical-pod-autoscaler-0.13.0/vertical-pod-autoscaler.yaml

# Create VPA
kubectl apply -f - <<EOF
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: smtp-perimeter-vpa
  namespace: mail-system
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: smtp-perimeter
  updatePolicy:
    updateMode: "Auto"
EOF
```

## Security

### Pod Security

Pods run with:
- Non-root user (UID 1000)
- Read-only root filesystem
- Dropped capabilities (except NET_BIND_SERVICE for port 25)
- Seccomp profile

### Network Policies

Network policies restrict traffic:
- Perimeter MTA: Internet → Pod → Internal Hub
- Internal Hub: Cluster → Pod
- Both: → etcd, → DNS

### TLS

All external connections require TLS:
- STARTTLS on port 25
- TLS on port 587 (submission)
- Implicit TLS on port 465 (SMTPS)

## Troubleshooting

### Check Pod Status

```bash
kubectl get pods -n mail-system
kubectl describe pod <pod-name> -n mail-system
kubectl logs <pod-name> -n mail-system
```

### Check Service Endpoints

```bash
kubectl get endpoints -n mail-system
```

### Test SMTP Connection

```bash
# Port forward for testing
kubectl port-forward svc/smtp-perimeter 2525:25 -n mail-system

# Test with telnet
telnet localhost 2525
> EHLO test.local
> QUIT
```

### Check Metrics

```bash
kubectl port-forward svc/smtp-perimeter-metrics 8080:8080 -n mail-system
curl http://localhost:8080/metrics
```

## Disaster Recovery

### Backup

```bash
# Backup ConfigMaps
kubectl get configmap smtp-config -n mail-system -o yaml > backup-configmap.yaml

# Backup Secrets
kubectl get secret smtp-tls -n mail-system -o yaml > backup-secret.yaml

# Backup etcd (if self-hosted)
kubectl exec etcd-0 -n mail-system -- etcdctl snapshot save /tmp/snapshot.db
```

### Multi-Region Failover

Automatic failover is handled by:
1. DNS health checks (Route53, CloudFlare)
2. Regional health monitoring
3. Global routing engine

Manual failover:
```bash
# Scale down unhealthy region
kubectl scale deployment smtp-perimeter --replicas=0 -n mail-system --context=us-east-1

# Scale up backup region
kubectl scale deployment smtp-perimeter --replicas=10 -n mail-system --context=eu-west-1
```

## Cost Optimization

### Resource Requests

Tune based on actual usage:
```yaml
resources:
  requests:
    cpu: 250m      # Start small
    memory: 256Mi
  limits:
    cpu: 1000m     # Prevent runaway
    memory: 1Gi
```

### Cluster Autoscaler

Enable node autoscaling:
```bash
# AWS EKS example
eksctl create cluster --asg-access --enable-autoscaling --min-nodes=3 --max-nodes=10
```

### Spot Instances

Use spot instances for non-critical workloads:
```yaml
nodeSelector:
  eks.amazonaws.com/capacityType: SPOT
tolerations:
- key: "eks.amazonaws.com/capacityType"
  operator: "Equal"
  value: "SPOT"
  effect: "NoSchedule"
```

## Advanced Configuration

### Custom Policies

Update ConfigMap with custom Starlark policies:
```yaml
data:
  policies.yaml: |
    policies:
      - name: "My Custom Policy"
        type: starlark
        enabled: true
        priority: 50
        scope:
          type: domain
          domains: ["example.com"]
        script: |
          def evaluate():
              if get_from().endswith("@example.com"):
                  accept("Local domain")
              reject("Unknown domain")
```

### LDAP Integration

Enable LDAP for authentication:
```yaml
env:
- name: ENABLE_LDAP
  value: "true"
- name: LDAP_URL
  value: "ldaps://ldap.example.com:636"
- name: LDAP_BIND_DN
  value: "cn=readonly,dc=example,dc=com"
- name: LDAP_SEARCH_BASE
  value: "ou=users,dc=example,dc=com"
```

## Support

For issues or questions:
- GitHub Issues: https://github.com/afterdarksys/go-emailservice-ads/issues
- Documentation: https://docs.example.com/smtp
