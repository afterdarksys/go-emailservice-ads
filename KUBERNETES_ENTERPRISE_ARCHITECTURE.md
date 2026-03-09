# Kubernetes-Aware Enterprise SMTP Architecture

## Executive Summary

A cloud-native, Kubernetes-aware enterprise SMTP platform supporting:
- **Perimeter MTA** (edge mail servers facing the internet)
- **Internal Mail Hub** (internal routing between services/users)
- **Hybrid Mode** (both perimeter and internal simultaneously)
- **Global Routing** (cross-region, cross-datacenter, cross-continent)
- **Auto-Discovery** (Kubernetes-native service discovery)
- **Auto-Scaling** (HPA based on queue depth, connection count)
- **High Availability** (multi-region active-active)
- **Zero-Downtime Updates** (rolling deployments, canary releases)

---

## Architecture Overview

```
                                    ╔═══════════════════════════════════╗
                                    ║   Global DNS Load Balancer        ║
                                    ║   (GeoDNS, Latency-based)         ║
                                    ╚═══════════════════════════════════╝
                                               │
                    ┌──────────────────────────┴────────────────────────────┐
                    │                          │                             │
            ┌───────▼─────────┐       ┌───────▼─────────┐         ┌────────▼────────┐
            │   REGION: US    │       │   REGION: EU    │         │  REGION: APAC   │
            │  (us-east-1)    │       │  (eu-west-1)    │         │  (ap-south-1)   │
            └─────────────────┘       └─────────────────┘         └─────────────────┘
                    │                          │                             │
        ┌───────────┴───────────┐  ┌──────────┴──────────┐      ┌──────────┴──────────┐
        │  Kubernetes Cluster   │  │  Kubernetes Cluster │      │  Kubernetes Cluster │
        │  ┌─────────────────┐  │  │  ┌─────────────────┐│      │  ┌─────────────────┐│
        │  │ Perimeter MTAs  │  │  │  │ Perimeter MTAs  ││      │  │ Perimeter MTAs  ││
        │  │ (Public IPs)    │  │  │  │ (Public IPs)    ││      │  │ (Public IPs)    ││
        │  └────────┬────────┘  │  │  └────────┬────────┘│      │  └────────┬────────┘│
        │           │            │  │           │          │      │           │          │
        │  ┌────────▼────────┐  │  │  ┌────────▼────────┐│      │  ┌────────▼────────┐│
        │  │ Internal Hubs   │  │  │  │ Internal Hubs   ││      │  │ Internal Hubs   ││
        │  │ (ClusterIP)     │  │  │  │ (ClusterIP)     ││      │  │ (ClusterIP)     ││
        │  └────────┬────────┘  │  │  └────────┬────────┘│      │  └────────┬────────┘│
        │           │            │  │           │          │      │           │          │
        │  ┌────────▼────────┐  │  │  ┌────────▼────────┐│      │  ┌────────▼────────┐│
        │  │  Storage/Queue  │  │  │  │  Storage/Queue  ││      │  │  Storage/Queue  ││
        │  │  (StatefulSet)  │  │  │  │  (StatefulSet)  ││      │  │  (StatefulSet)  ││
        │  └─────────────────┘  │  │  └─────────────────┘│      │  └─────────────────┘│
        └───────────────────────┘  └─────────────────────┘      └─────────────────────┘
                    │                          │                             │
                    └──────────────────────────┴─────────────────────────────┘
                                               │
                                    ╔══════════▼══════════╗
                                    ║  Global State Store ║
                                    ║  (etcd/Consul)      ║
                                    ╚═════════════════════╝
```

---

## Deployment Modes

### 1. Perimeter MTA Mode

**Purpose**: Internet-facing mail servers that receive/send external mail

**Characteristics**:
- Public IP addresses (LoadBalancer service type)
- Heavy security filtering (SPF, DKIM, DMARC, RBL, reputation)
- Rate limiting per IP/domain
- Connection tracking and abuse prevention
- TLS enforcement (DANE, MTA-STS)
- Greylisting for unknown senders
- DDoS protection integration

**Kubernetes Resources**:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: smtp-perimeter
  labels:
    component: perimeter-mta
    tier: edge
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
  selector:
    matchLabels:
      component: perimeter-mta
  template:
    spec:
      containers:
      - name: smtp-perimeter
        image: goemailservices:latest
        env:
        - name: DEPLOYMENT_MODE
          value: "perimeter"
        - name: REQUIRE_TLS
          value: "true"
        - name: ENABLE_GREYLISTING
          value: "true"
        ports:
        - containerPort: 25    # SMTP
        - containerPort: 587   # Submission
        - containerPort: 465   # SMTPS
```

**Traffic Flow**:
```
Internet → LoadBalancer → Perimeter MTA → Policy Engine → Internal Hub → Delivery
```

### 2. Internal Mail Hub Mode

**Purpose**: Internal mail routing between services, users, and applications

**Characteristics**:
- ClusterIP service (internal only)
- Authenticated submission (SASL)
- Trusted network access
- Fast routing (no heavy filtering)
- Integration with directory services (LDAP/AD)
- Internal-only domains
- Service mesh integration

**Kubernetes Resources**:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: smtp-internal
  labels:
    component: internal-hub
    tier: backend
spec:
  replicas: 5
  template:
    spec:
      containers:
      - name: smtp-internal
        image: goemailservices:latest
        env:
        - name: DEPLOYMENT_MODE
          value: "internal"
        - name: REQUIRE_AUTH
          value: "true"
        - name: TRUSTED_NETWORKS
          value: "10.0.0.0/8,172.16.0.0/12"
        ports:
        - containerPort: 2525  # Internal SMTP
        - containerPort: 587   # Submission
```

**Traffic Flow**:
```
Application → Service Discovery → Internal Hub → Routing → Delivery/Relay
```

### 3. Hybrid Mode

**Purpose**: Single deployment supporting both perimeter and internal roles

**Implementation**: Pod with multiple services/ports
```yaml
apiVersion: v1
kind: Service
metadata:
  name: smtp-perimeter
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
spec:
  type: LoadBalancer
  selector:
    component: smtp-hybrid
  ports:
  - name: smtp
    port: 25
    targetPort: 25
  - name: submission
    port: 587
    targetPort: 587
---
apiVersion: v1
kind: Service
metadata:
  name: smtp-internal
spec:
  type: ClusterIP
  selector:
    component: smtp-hybrid
  ports:
  - name: internal-smtp
    port: 2525
    targetPort: 2525
```

---

## Global Routing Architecture

### Routing Decision Engine

**Factors**:
1. **Geographic Location** - Route to nearest region
2. **Latency** - Use region with lowest latency
3. **Load** - Balance across regions based on queue depth
4. **Health** - Avoid unhealthy regions
5. **Policy** - Domain-specific routing rules
6. **Cost** - Consider data transfer costs

### Cross-Region Routing

```go
// internal/routing/global.go
type GlobalRouter struct {
    regions       map[string]*RegionInfo
    stateStore    cluster.StateStore
    healthChecker *HealthChecker
    latencyMap    *LatencyMap
}

type RegionInfo struct {
    Name          string
    Endpoints     []string
    Load          float64
    HealthStatus  HealthStatus
    Latency       time.Duration
    Capacity      int
    CurrentConns  int
}

func (r *GlobalRouter) SelectRegion(ctx context.Context, emailCtx *EmailContext) (*RegionInfo, error) {
    // 1. Filter healthy regions
    healthy := r.healthChecker.HealthyRegions()

    // 2. Check sender/recipient location
    senderRegion := r.geolocate(emailCtx.RemoteIP)
    recipientRegion := r.lookupDomain(emailCtx.To[0])

    // 3. Apply routing policies
    if region := r.applyRoutingPolicies(emailCtx, healthy); region != nil {
        return region, nil
    }

    // 4. Select by latency + load
    return r.selectByLatencyAndLoad(senderRegion, recipientRegion, healthy), nil
}
```

### Cross-Datacenter Message Forwarding

**Scenario**: Message arrives in US-EAST but recipient's primary mailbox is in EU-WEST

```
┌────────────┐         ┌────────────┐         ┌────────────┐
│  US-EAST   │         │   RELAY    │         │  EU-WEST   │
│            │  ────▶  │   QUEUE    │  ────▶  │            │
│ Perimeter  │         │  (async)   │         │  Delivery  │
└────────────┘         └────────────┘         └────────────┘
```

**Implementation**:
```go
// Detect cross-region delivery
if recipientRegion := routing.GetMailboxRegion(recipient); recipientRegion != currentRegion {
    // Queue for cross-region transfer
    queue.Enqueue(QueueConfig{
        Tier:     "inter-region",
        Priority: 50,
        Target:   recipientRegion,
        Message:  msg,
    })
    return nil
}
```

---

## Kubernetes Service Discovery

### Automatic Peer Discovery

```go
// internal/k8s/discovery.go
package k8s

import (
    "context"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

type ServiceDiscovery struct {
    clientset *kubernetes.Clientset
    namespace string
    selector  string
}

func (sd *ServiceDiscovery) DiscoverPeers(ctx context.Context) ([]*PeerInfo, error) {
    pods, err := sd.clientset.CoreV1().Pods(sd.namespace).List(ctx, metav1.ListOptions{
        LabelSelector: sd.selector,
    })
    if err != nil {
        return nil, err
    }

    var peers []*PeerInfo
    for _, pod := range pods.Items {
        if pod.Status.Phase == corev1.PodRunning {
            peers = append(peers, &PeerInfo{
                PodName:   pod.Name,
                PodIP:     pod.Status.PodIP,
                NodeName:  pod.Spec.NodeName,
                Region:    pod.Labels["topology.kubernetes.io/region"],
                Zone:      pod.Labels["topology.kubernetes.io/zone"],
            })
        }
    }

    return peers, nil
}
```

### Endpoint Watching

```go
func (sd *ServiceDiscovery) WatchEndpoints(ctx context.Context) (<-chan EndpointEvent, error) {
    events := make(chan EndpointEvent)

    watcher, err := sd.clientset.CoreV1().Endpoints(sd.namespace).Watch(ctx, metav1.ListOptions{})
    if err != nil {
        return nil, err
    }

    go func() {
        defer close(events)
        for event := range watcher.ResultChan() {
            events <- EndpointEvent{
                Type:     event.Type,
                Endpoint: event.Object.(*corev1.Endpoints),
            }
        }
    }()

    return events, nil
}
```

---

## Auto-Scaling Configuration

### Horizontal Pod Autoscaler (HPA)

#### Queue-Based Scaling
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: smtp-perimeter-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: smtp-perimeter
  minReplicas: 3
  maxReplicas: 20
  metrics:
  - type: Pods
    pods:
      metric:
        name: queue_depth
      target:
        type: AverageValue
        averageValue: "100"
  - type: Pods
    pods:
      metric:
        name: active_connections
      target:
        type: AverageValue
        averageValue: "50"
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
      - type: Percent
        value: 50
        periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
      - type: Percent
        value: 10
        periodSeconds: 60
```

### Custom Metrics

```go
// internal/metrics/k8s_metrics.go
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
)

var (
    queueDepthMetric = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "queue_depth",
            Help: "Number of messages in queue",
        },
        []string{"tier"},
    )

    activeConnectionsMetric = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "active_connections",
            Help: "Number of active SMTP connections",
        },
    )
)

func init() {
    prometheus.MustRegister(queueDepthMetric)
    prometheus.MustRegister(activeConnectionsMetric)
}
```

---

## State Store Integration

### Global State Architecture

```go
// internal/cluster/global_state.go
package cluster

type GlobalState struct {
    store cluster.StateStore
}

// Region registry
func (gs *GlobalState) RegisterRegion(ctx context.Context, region *RegionInfo) error {
    key := fmt.Sprintf("/regions/%s", region.Name)
    data, _ := json.Marshal(region)
    return gs.store.PutWithTTL(ctx, key, data, 30*time.Second)
}

// Health heartbeat
func (gs *GlobalState) Heartbeat(ctx context.Context, regionName string) error {
    key := fmt.Sprintf("/regions/%s/heartbeat", regionName)
    timestamp := time.Now().Unix()
    return gs.store.Put(ctx, key, []byte(fmt.Sprintf("%d", timestamp)))
}

// Global routing table
func (gs *GlobalState) UpdateRoutingTable(ctx context.Context, table *RoutingTable) error {
    key := "/routing/global"
    data, _ := json.Marshal(table)
    return gs.store.Put(ctx, key, data)
}

// Watch for routing changes
func (gs *GlobalState) WatchRouting(ctx context.Context) (<-chan *RoutingTable, error) {
    events, err := gs.store.Watch(ctx, "/routing")
    if err != nil {
        return nil, err
    }

    updates := make(chan *RoutingTable)
    go func() {
        for event := range events {
            if event.Type == cluster.WatchEventPut {
                var table RoutingTable
                json.Unmarshal(event.Value, &table)
                updates <- &table
            }
        }
    }()

    return updates, nil
}
```

---

## Configuration Management

### ConfigMap Structure

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: smtp-config
data:
  config.yaml: |
    server:
      mode: perimeter  # perimeter, internal, hybrid
      addr: ":25"
      domain: mail.example.com

      # Regional configuration
      region: us-east-1
      zone: us-east-1a

      # Global routing
      enable_global_routing: true
      regions:
        - name: us-east-1
          endpoint: smtp-us-east.svc.cluster.local
          weight: 100
        - name: eu-west-1
          endpoint: smtp-eu-west.svc.cluster.local
          weight: 100

      # Perimeter settings
      perimeter:
        require_tls: true
        enable_greylisting: true
        rate_limit_per_ip: 100
        max_connections_per_ip: 10

      # Internal hub settings
      internal:
        require_auth: true
        trusted_networks:
          - 10.0.0.0/8
          - 172.16.0.0/12
        ldap_integration: true

    # Kubernetes integration
    kubernetes:
      enabled: true
      namespace: mail-system
      service_discovery: true
      endpoint_watching: true

      # Auto-scaling triggers
      autoscale:
        enable: true
        queue_depth_threshold: 100
        connection_threshold: 50

    # State store (for global coordination)
    state_store:
      type: etcd  # etcd, consul, redis
      endpoints:
        - etcd-0.etcd.mail-system.svc.cluster.local:2379
        - etcd-1.etcd.mail-system.svc.cluster.local:2379
        - etcd-2.etcd.mail-system.svc.cluster.local:2379
      prefix: /mail-system
```

---

## Security Considerations

### Network Policies

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: smtp-perimeter-netpol
spec:
  podSelector:
    matchLabels:
      component: perimeter-mta
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - ipBlock:
        cidr: 0.0.0.0/0  # Internet traffic
    ports:
    - protocol: TCP
      port: 25
    - protocol: TCP
      port: 587
  egress:
  - to:
    - podSelector:
        matchLabels:
          component: internal-hub
    ports:
    - protocol: TCP
      port: 2525
  - to:
    - namespaceSelector:
        matchLabels:
          name: mail-system
    podSelector:
      matchLabels:
        component: etcd
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: smtp-internal-netpol
spec:
  podSelector:
    matchLabels:
      component: internal-hub
  policyTypes:
  - Ingress
  ingress:
  - from:
    - podSelector:
        matchLabels:
          component: perimeter-mta
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 2525
```

### Pod Security

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: smtp-perimeter
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: smtp
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
        add:
        - NET_BIND_SERVICE  # For port 25
      readOnlyRootFilesystem: true
    volumeMounts:
    - name: tmp
      mountPath: /tmp
    - name: config
      mountPath: /etc/smtp
      readOnly: true
  volumes:
  - name: tmp
    emptyDir: {}
  - name: config
    configMap:
      name: smtp-config
```

---

## Observability

### Metrics

**Prometheus Metrics**:
- `smtp_connections_total` - Total connections by region/type
- `smtp_messages_received_total` - Messages received by region
- `smtp_messages_delivered_total` - Messages delivered by region
- `smtp_queue_depth` - Current queue depth by tier
- `smtp_cross_region_transfers_total` - Cross-region message transfers
- `smtp_policy_evaluations_total` - Policy evaluations by result
- `smtp_latency_seconds` - Processing latency by stage

### Logging

**Structured Logging**:
```json
{
  "timestamp": "2026-03-08T21:30:00Z",
  "level": "info",
  "region": "us-east-1",
  "zone": "us-east-1a",
  "pod": "smtp-perimeter-7d8f9-abc123",
  "deployment_mode": "perimeter",
  "message": "Message accepted",
  "message_id": "20260308213000.abc123@mail.example.com",
  "from": "sender@example.com",
  "to": ["recipient@example.org"],
  "remote_ip": "203.0.113.45",
  "policy_result": "accept",
  "destination_region": "eu-west-1"
}
```

### Tracing

**OpenTelemetry Integration**:
```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
)

func (s *Session) Data(r io.Reader) error {
    ctx, span := otel.Tracer("smtp").Start(s.ctx, "smtp.data")
    defer span.End()

    span.SetAttributes(
        attribute.String("smtp.from", s.From),
        attribute.StringSlice("smtp.to", s.To),
        attribute.String("region", s.region),
    )

    // ... processing
}
```

---

## Deployment Strategies

### Rolling Update

**Zero-Downtime Deployment**:
```yaml
strategy:
  type: RollingUpdate
  rollingUpdate:
    maxSurge: 25%
    maxUnavailable: 0
```

### Canary Deployment

```yaml
# Stable deployment (90% traffic)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: smtp-perimeter-stable
spec:
  replicas: 9
---
# Canary deployment (10% traffic)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: smtp-perimeter-canary
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: smtp
        image: goemailservices:v2.0.0-canary
```

### Blue-Green Deployment

```yaml
# Blue environment (current)
apiVersion: v1
kind: Service
metadata:
  name: smtp-perimeter
spec:
  selector:
    version: blue
---
# Green environment (new version)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: smtp-perimeter-green
spec:
  replicas: 3
  selector:
    matchLabels:
      version: green
```

---

## Disaster Recovery

### Multi-Region Failover

**Automatic Failover**:
```go
type FailoverController struct {
    healthChecker *HealthChecker
    dnsManager    *DNSManager
}

func (fc *FailoverController) MonitorHealth(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-time.After(30 * time.Second):
            for _, region := range fc.regions {
                if !fc.healthChecker.IsHealthy(region) {
                    fc.logger.Warn("Region unhealthy, failing over",
                        zap.String("region", region.Name))
                    fc.dnsManager.RemoveRegion(region.Name)
                    fc.redistributeTraffic(region)
                }
            }
        }
    }
}
```

### Data Replication

**Cross-Region Queue Replication**:
- Primary region writes to local queue
- Async replication to backup regions
- Read from nearest healthy region
- Conflict resolution with vector clocks

---

## Cost Optimization

### Resource Requests/Limits

```yaml
resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: 2000m
    memory: 2Gi
```

### Cluster Autoscaler Integration

```yaml
apiVersion: v1
kind: Node
metadata:
  labels:
    workload: mail-processing
spec:
  taints:
  - key: workload
    value: mail-processing
    effect: NoSchedule
```

### Cost-Aware Routing

```go
func (r *GlobalRouter) SelectRegionWithCost(ctx context.Context, emailCtx *EmailContext) (*RegionInfo, error) {
    // Prefer same-region delivery (no data transfer cost)
    if region := r.sameRegionRoute(emailCtx); region != nil {
        return region, nil
    }

    // Calculate cross-region transfer costs
    costs := make(map[string]float64)
    for _, region := range r.regions {
        costs[region.Name] = r.calculateTransferCost(r.currentRegion, region.Name)
    }

    // Select cheapest route that meets SLA
    return r.selectByLatencyAndCost(costs), nil
}
```

---

## Summary

This architecture provides:
✅ **Kubernetes-Native** - Full K8s integration with service discovery, auto-scaling, health checks
✅ **Global Scale** - Cross-region, cross-datacenter, cross-continent routing
✅ **Flexible Deployment** - Perimeter MTA, Internal Hub, or Hybrid modes
✅ **High Availability** - Multi-region active-active with automatic failover
✅ **Zero Downtime** - Rolling updates, canary deployments, blue-green
✅ **Enterprise Security** - Network policies, pod security, TLS enforcement
✅ **Observability** - Prometheus metrics, structured logging, OpenTelemetry tracing
✅ **Cost Optimized** - Smart routing, auto-scaling, resource management

**Next Steps**:
1. Implement Kubernetes service discovery (`internal/k8s/`)
2. Build deployment mode detection (`perimeter`, `internal`, `hybrid`)
3. Create global routing engine (`internal/routing/global.go`)
4. Add state store backends (etcd, Consul)
5. Generate Kubernetes manifests (`deploy/kubernetes/`)
6. Implement access control and lookup maps (Postfix-compatible)
