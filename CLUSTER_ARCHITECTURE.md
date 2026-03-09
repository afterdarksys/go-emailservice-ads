# Multi-Site Cluster Architecture

## Overview
Distributed email cluster system spanning multiple regions/data centers with API key-based node authentication and intelligent load distribution.

## Architecture

### Node Types

1. **Master Nodes** (control plane)
   - Policy distribution
   - Configuration management
   - Health monitoring
   - Load balancing decisions
   - Cluster state management

2. **Worker Nodes** (data plane)
   - SMTP/IMAP/JMAP servers
   - Message processing
   - Queue management
   - Local storage

3. **Edge Nodes** (optional)
   - MX record endpoints
   - Initial message acceptance
   - Smart routing to workers
   - DDoS protection

### Cluster Configuration

```yaml
# cluster.yaml
cluster:
  # This node's identity
  node:
    id: "node-us-west-1"
    hostname: "mail1.us-west.company.com"
    region: "us-west"
    datacenter: "dc1"
    role: "worker"  # master, worker, edge

  # Node API endpoint
  api:
    listen_addr: "0.0.0.0:9443"
    tls_cert: "/etc/mail/cluster/node.crt"
    tls_key: "/etc/mail/cluster/node.key"
    api_key: "${CLUSTER_API_KEY}"  # Shared secret for node auth

  # Master nodes (for workers to connect to)
  masters:
    - hostname: "master1.company.com"
      api_endpoint: "https://master1.company.com:9443"
      region: "us-east"

    - hostname: "master2.company.com"
      api_endpoint: "https://master2.company.com:9443"
      region: "eu-west"

  # Peer nodes (other workers in cluster)
  peers:
    - node_id: "node-us-east-1"
      hostname: "mail1.us-east.company.com"
      api_endpoint: "https://mail1.us-east.company.com:9443"
      region: "us-east"
      weight: 100

    - node_id: "node-eu-west-1"
      hostname: "mail1.eu-west.company.com"
      api_endpoint: "https://mail1.eu-west.company.com:9443"
      region: "eu-west"
      weight: 100

  # Load distribution settings
  load_balancing:
    strategy: "weighted_round_robin"  # weighted_round_robin, least_connections, geographic
    health_check_interval: 30s
    failover_timeout: 60s
    max_retries: 3

  # Shared state backend
  state:
    type: "etcd"  # etcd, consul, redis_cluster
    endpoints:
      - "etcd1.company.com:2379"
      - "etcd2.company.com:2379"
      - "etcd3.company.com:2379"
    tls: true
    tls_cert: "/etc/mail/cluster/etcd-client.crt"
    tls_key: "/etc/mail/cluster/etcd-client.key"
    tls_ca: "/etc/mail/cluster/etcd-ca.crt"

  # Message distribution
  distribution:
    strategy: "sticky_recipient"  # sticky_recipient, round_robin, least_load

    # User affinity - keep user's messages on same node
    sticky_ttl: 24h

    # Cross-node message forwarding
    enable_forwarding: true
    forwarding_protocol: "grpc"  # grpc, http, smtp
```

### Node Authentication

#### API Key-Based Auth
```go
// Cluster API key format: cluster:node_id:base64(hmac-sha256(secret, node_id))
// Example: cluster:node-us-west-1:YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo=

// Each node has:
// 1. Node ID (unique identifier)
// 2. Shared cluster secret (environment variable)
// 3. Generated API key (derived from node_id + secret)
```

#### Mutual TLS
- Each node has a certificate signed by cluster CA
- Hostname verification
- Certificate pinning for masters
- Auto-rotation support

### Load Distribution Strategies

#### 1. Geographic Routing
```
User in US → Route to US nodes
User in EU → Route to EU nodes
Failover to nearest region if primary unavailable
```

#### 2. Sticky Recipient
```
alice@company.com → Always routes to node-us-west-1
bob@company.com → Always routes to node-us-east-1

Benefits:
- Mailbox locality
- Cache efficiency
- Reduced cross-node queries
```

#### 3. Least Connections
```
Track active connections per node
Route new sessions to node with fewest connections
```

#### 4. Weighted Round Robin
```
Nodes have weights (1-100) based on capacity
Higher weight = more traffic
Adjust weights dynamically based on load
```

### Message Flow

#### Inbound Mail (External → Cluster)
```
1. DNS MX records point to edge nodes (or all workers)
   MX 10 mail-edge-us.company.com
   MX 20 mail-edge-eu.company.com

2. Edge node receives SMTP connection

3. Edge determines target node:
   - Look up recipient in user routing table
   - Apply load balancing strategy
   - Select target worker node

4. Forward via cluster protocol:
   Option A: SMTP relay to target node
   Option B: gRPC message forwarding
   Option C: Enqueue to shared queue (Redis/NATS)

5. Target node processes and stores message
```

#### Outbound Mail (Cluster → External)
```
1. Message queued on local node

2. Local node attempts delivery

3. On failure or retry:
   - Can be picked up by any node in cluster
   - Shared queue visibility

4. Delivery state replicated to cluster state store
```

#### Inter-Node Mail (Internal)
```
alice@company.com (on node-1) → bob@company.com (on node-2)

1. Node-1 receives message
2. Looks up bob's home node (node-2) in routing table
3. Options:
   A. Direct SMTP to node-2
   B. gRPC forwarding to node-2
   C. Queue for node-2 pickup
4. Node-2 delivers to bob's mailbox
```

### Cluster State

#### Shared State (via etcd/Consul)
```
/cluster/nodes/
  node-us-west-1/
    status: "online"
    load: 45
    connections: 1250
    last_heartbeat: 1234567890

/cluster/routing/
  users/
    alice@company.com: "node-us-west-1"
    bob@company.com: "node-us-east-1"

  domains/
    company.com:
      strategy: "sticky_recipient"
      nodes: ["node-us-west-1", "node-us-east-1"]

/cluster/policies/
  global-antispam.star: "...script content..."
  last_update: 1234567890

/cluster/queue/
  pending/
    msg-12345: "node-us-west-1"
  processing/
    msg-67890: "node-us-east-1"
```

#### Local State (per-node)
```
- Active SMTP/IMAP connections
- Local mailbox cache
- Recent message cache
- Local queue
```

### API Endpoints (Cluster Management)

```
# Node Health & Status
GET    /cluster/api/v1/nodes                    # List all nodes
GET    /cluster/api/v1/nodes/:id                # Node details
POST   /cluster/api/v1/nodes/:id/drain          # Drain node for maintenance
POST   /cluster/api/v1/nodes/:id/undrain        # Re-enable node

# Load Balancing
GET    /cluster/api/v1/load                     # Cluster load statistics
POST   /cluster/api/v1/load/rebalance           # Trigger rebalance

# Message Forwarding
POST   /cluster/api/v1/messages/forward         # Forward message to node
GET    /cluster/api/v1/messages/:id/location    # Find message location

# Routing
GET    /cluster/api/v1/routing/users/:email     # Get user's home node
POST   /cluster/api/v1/routing/users            # Set user home node
GET    /cluster/api/v1/routing/domains/:domain  # Get domain routing

# Policy Sync
GET    /cluster/api/v1/policies                 # List policies
POST   /cluster/api/v1/policies/sync            # Sync policies to all nodes

# Health Check
GET    /cluster/api/v1/health                   # Cluster health
GET    /cluster/api/v1/metrics                  # Cluster metrics
```

### High Availability

#### Split-Brain Prevention
```
- Require quorum (majority of masters online)
- Use distributed locks (etcd)
- Leadership election for master role
- Fencing for failed nodes
```

#### Failover Scenarios

1. **Worker Node Failure**
   ```
   - Master detects missed heartbeats (3x interval)
   - Marks node as "offline"
   - Redirects traffic to healthy nodes
   - Messages in local queue picked up by others
   ```

2. **Master Node Failure**
   ```
   - Workers connect to backup master
   - Leadership election among masters
   - New master takes over coordination
   ```

3. **Region Failure**
   ```
   - All traffic fails over to healthy regions
   - Geographic DNS updates
   - Cross-region message sync
   ```

4. **Network Partition**
   ```
   - Partition detection via state store
   - Minority partition goes read-only
   - Majority partition continues operation
   - Auto-heal when partition resolves
   ```

### Message Queue Distribution

#### Option 1: Shared Queue (Redis/NATS)
```
Pros:
- Any node can pick up any message
- True load distribution
- Simple to implement

Cons:
- Central dependency
- Network overhead
- Queue bottleneck
```

#### Option 2: Per-Node Queues with Work Stealing
```
Pros:
- No central queue
- Node locality
- Better performance

Cons:
- More complex
- Requires coordination
- Possible imbalance
```

#### Option 3: Hybrid (Recommended)
```
- Hot path: Per-node queues
- Cold path: Shared retry queue
- Failed messages go to shared DLQ
- Load balancing handles new messages
```

### Security

#### Node Authentication
```
- API key per node
- Mutual TLS
- Certificate pinning
- Key rotation support
```

#### Message Security
```
- TLS for all inter-node traffic
- Optional: Encrypt message content in transit
- Audit log for all cluster operations
```

#### Network Segmentation
```
- Cluster API on private network
- Public SMTP/IMAP on public network
- Firewall rules between nodes
```

### Monitoring & Observability

#### Metrics to Track
```
- Messages processed per node
- Queue depth per node
- Cross-node forwarding rate
- Node health scores
- Inter-node latency
- Failed node count
- Split-brain events
- Policy sync lag
```

#### Distributed Tracing
```
- Trace message flow across nodes
- Track forwarding hops
- Measure end-to-end latency
- Identify bottlenecks
```

### Deployment Patterns

#### Pattern 1: Simple HA (2-3 nodes, same datacenter)
```
cluster:
  masters: []  # No dedicated masters
  peers:
    - node-1 (worker+master)
    - node-2 (worker+master)
    - node-3 (worker+master)

  state: etcd (3-node cluster)
  dns: Round-robin MX records
```

#### Pattern 2: Multi-Region (6+ nodes)
```
cluster:
  masters:
    - master-us (us-east)
    - master-eu (eu-west)

  workers:
    - node-us-east-1, node-us-east-2 (us-east)
    - node-us-west-1, node-us-west-2 (us-west)
    - node-eu-west-1, node-eu-west-2 (eu-west)

  state: etcd (5-node, distributed)
  dns: GeoDNS with region-specific MX
```

#### Pattern 3: Edge + Backend
```
cluster:
  edge:
    - edge-us-1, edge-us-2 (public, accept mail)
    - edge-eu-1, edge-eu-2 (public, accept mail)

  workers:
    - worker-1 through worker-10 (private, process mail)

  masters:
    - master-1, master-2, master-3 (control plane)

  dns: Edge nodes in MX records
  routing: Edge → Workers via load balancer
```

### Integration with Policy Engine

#### Policy Distribution
```
1. Policy created/updated on master
2. Master pushes to etcd
3. All nodes watch etcd for changes
4. Nodes reload policies on change
5. Verify hash across all nodes
```

#### Distributed Policy Execution
```
- Policies execute on node that receives message
- State shared via cluster state store
- Rate limits coordinated across nodes
- Reputation scores aggregated
```

### Implementation Structure

```
internal/cluster/
├── node.go               # Node registration & heartbeat
├── discovery.go          # Peer discovery
├── auth.go              # API key & mTLS auth
├── api_server.go        # Cluster API server
├── api_client.go        # Client for calling peer APIs
├── state/
│   ├── store.go         # State store interface
│   ├── etcd.go          # etcd implementation
│   ├── redis.go         # Redis implementation
│   └── consul.go        # Consul implementation
├── routing/
│   ├── router.go        # Message routing logic
│   ├── affinity.go      # Sticky routing
│   └── strategy.go      # Load balancing strategies
├── forwarding/
│   ├── smtp.go          # SMTP-based forwarding
│   ├── grpc.go          # gRPC-based forwarding
│   └── queue.go         # Queue-based forwarding
├── health/
│   ├── checker.go       # Health check logic
│   └── monitor.go       # Cluster monitoring
└── failover/
    ├── detector.go      # Failure detection
    └── recovery.go      # Failover & recovery

cluster.yaml             # Cluster configuration
```

### CLI Commands

```bash
# Cluster management
mailctl cluster status                    # Show cluster health
mailctl cluster nodes                     # List all nodes
mailctl cluster node add <hostname>       # Add node to cluster
mailctl cluster node remove <node-id>     # Remove node
mailctl cluster node drain <node-id>      # Drain for maintenance

# Load balancing
mailctl cluster load                      # Show load distribution
mailctl cluster rebalance                 # Trigger rebalance

# Message routing
mailctl cluster route <email>             # Show routing for user
mailctl cluster route-set <email> <node>  # Set user home node

# Diagnostics
mailctl cluster test-connectivity         # Test inter-node connectivity
mailctl cluster trace <message-id>        # Trace message across nodes
```

### Next Steps

1. Implement cluster state store abstraction
2. Add node registration & discovery
3. Implement cluster API server/client
4. Add message forwarding mechanisms
5. Integrate with SMTP/IMAP servers
6. Add monitoring & health checks
7. Implement failover logic
8. Create admin tooling
