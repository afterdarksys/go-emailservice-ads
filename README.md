# go-emailservice-ads

**Mailhub with persistent SMTP delivery, IMAP mailboxes, scoped REST administration, filtering and recovery tooling.**

Current release: **2.7.0**. Start with the [documentation index](docs/README.md),
[administration](docs/ADMINISTRATION.md), [configuration](docs/CONFIGURATION.md),
[troubleshooting](docs/TROUBLESHOOTING.md) and [REST API reference](docs/API_REFERENCE.md).
The supported topology uses one owner per spool, with an optional perimeter.
Production throughput and failover must be qualified in the target environment.

Older feature descriptions and CLI examples below are historical; the 2.7 guides
take precedence. See [release notes](CHANGELOG.md) and [current backlog](TODO).

## Historical version 2.1.0 overview

This release adds **Elasticsearch integration** for comprehensive mail event logging and search, **AfterSMTP next-generation protocol** with QUIC/gRPC/blockchain support, and **SSO integration** for After Dark Systems authentication.

---

## Historical feature overview (deployment not asserted)

### 🆕 Elasticsearch Integration (v2.1) ✅

- **Mail Event Logging** - Track every message lifecycle event (enqueue, process, deliver, fail, bounce, retry)
- **Message Correlation** - Global TraceID follows messages across instances and queue ID changes
- **Smart Header Logging** - Per-domain/IP/MX control with privacy-first design and regex redaction
- **Async Bulk Indexing** - Non-blocking event publishing with configurable sampling
- **Time-Based Indices** - Daily indices (mail-events-YYYY.MM.DD) with ILM policies
- **Rich Search** - Query by sender, recipient, domain, IP, SPF/DKIM results, latency, errors
- **Kibana Ready** - Pre-configured index templates for operations, security, performance dashboards
- **Documentation** - Complete setup guide in `ELASTICSEARCH_INTEGRATION.md`

### 🆕 AfterSMTP Next-Gen Protocol (v2.1) ✅

- **AMP Protocol** - AfterSMTP Messaging Protocol with QUIC transport (HTTP/3)
- **gRPC Streaming** - Native bidirectional gRPC for real-time message flow
- **Blockchain Ledger** - Substrate-based distributed ledger for audit trails
- **Legacy Bridge** - Seamless SMTP ↔ AMP/QUIC/gRPC protocol translation
- **MTA-STS** - Mail Transfer Agent Strict Transport Security
- **TLS Reporting** - SMTP TLS reporting (RFC 8460)
- **Enhanced DANE** - Extended DANE/TLSA verification
- **ARC Support** - Authenticated Received Chain (RFC 8617)

### 🆕 Single Sign-On (v2.1) ✅

- **OAuth2/OIDC** - Full OAuth2 and OpenID Connect support
- **After Dark Systems SSO** - Direct integration with ADS Directory Service
- **Multi-Provider** - Pluggable provider architecture (OIDC, OAuth2)
- **Auto-Provisioning** - Automatic user creation from SSO claims
- **Token Management** - Secure token storage and refresh
- **Fallback Auth** - Local authentication when SSO unavailable
- **Documentation** - Complete setup guide in `SSO_SETUP.md`

### 🆕 Kubernetes Enterprise Platform (v2.0) ✅

- **Service Discovery** - Automatic peer detection using Kubernetes API
- **Deployment Modes** - Perimeter MTA, Internal Hub, Hybrid, or Standalone operation
- **Global Routing** - Cross-region, cross-datacenter, cross-continent message routing
- **Health Monitoring** - Regional health checks with automatic failover
- **Latency Tracking** - Inter-region latency measurement for optimal routing
- **Cost-Aware Routing** - Minimize data transfer costs across regions
- **Production Manifests** - Complete Kubernetes deployment configurations
- **Auto-Scaling** - HPA with CPU, memory, queue depth, and connection-based scaling
- **Zero-Trust Security** - Network policies with pod isolation
- **RBAC** - Complete role-based access control for Kubernetes

### 🆕 Postfix-Style Access Control (v2.0) ✅

- **20+ Lookup Map Types** - hash, btree, regexp, pcre, cidr, mysql, pgsql, sqlite, ldap, memcache, tcp, socketmap, and more
- **15+ SMTP Restrictions** - permit_mynetworks, reject_rbl_client, reject_unauth_destination, check_client_access, check_policy_service, etc.
- **Stage-Based Filtering** - Client, HELO, sender, recipient, data, end-of-data stages
- **CIDR Matching** - Network-based access control
- **RBL/DNSBL and IP allow/deny lists** - Active SMTP peer filtering via `server.ip_filter`; see [configuration and reputation support](docs/IP_FILTERING.md)
- **DNS Validation** - Domain verification for senders and recipients
- **Policy Service Protocol** - External policy server support
- **Restriction Classes** - Reusable rule sets

### 🆕 Admin CLI (adsemailadm) (v2.2) ✅

- **Queue Management** - stats, list, retry, purge, inspect, DLQ operations
- **Policy Management** - list, show, test, reload, stats, validate (8 example policies)
- **Mailbox Management** - list, create, delete, quota, alias, routing
- **TLS/SSL Management** - status, cert operations, test, DANE
- **Monitoring** - Real-time dashboard, statistics, Prometheus metrics
- **Directory Services** - LDAP test, config, sync, user lookup
- **Configuration** - show, validate, reload, set
- **Cluster Management** - status, nodes, load, rebalance, drain
- **Security** - audit logs, SPF/DKIM/DMARC checks, RBL lookup
- **Health Checks** - Comprehensive system status
- **API Key Management** - create, list, revoke keys in config (Bearer token auth)
- **Sieve Script Management** - per-user Sieve filter scripts (list, show, upload, edit, delete)

### Core Features ✅

- **Multi-tier Queue System** - 1,050 workers across 5 priority tiers (emergency/msa/int/out/bulk)
- **Disaster Recovery** - WAL-based journal, persistent storage and fenced cold-standby recovery
- **Deduplication** - SHA256 content hashing prevents duplicate processing
- **Retry Scheduler** - Exponential backoff (1m, 2m, 4m, 8m intervals)
- **Dead Letter Queue** - Failed messages quarantined for manual review
- **Rate Limiting** - Per-tier token bucket rate limiting (500-5000 msg/s per tier)

### Security Features ✅

- **DANE/TLSA Validation** ⭐ **NEW!** - RFC 7672 compliant, DNS-based certificate authentication with DNSSEC
- **SPF Verification** - RFC 7208 compliant, ENFORCED (rejects spoofed mail)
- **DKIM Verification** - RFC 6376 compliant, ACTIVE (logging results)
- **DMARC Support** - RFC 7489 compliant (code exists, ready to enable)
- **Greylisting** - Triplet-based anti-spam (50-90% spam reduction, disabled by default)
- **DNS Caching** - 5-minute TTL cache for MX/TXT/A/AAAA/TLSA records (200-500x speedup)
- **Enhanced Auth** - Account lockout protection, IP tracking, rate limiting
- **Modern TLS** - TLS 1.2/1.3, ECDHE, AES-GCM, ChaCha20-Poly1305

### Services ✅

- **SMTP Server** - Port 2525, STARTTLS, SASL authentication, access control, policy engine, SSO
- **IMAP Server** - Port 1143, full IMAP4rev1 implementation, SSO support
- **REST API** - Port 8080, queue management, policy operations, metrics, DLQ operations
- **gRPC API** - Port 50051 (placeholder)
- **AfterSMTP QUIC** - Port 4434, HTTP/3 AMP protocol (if enabled)
- **AfterSMTP gRPC** - Port 4433, native gRPC streaming (if enabled)
- **Admin CLI** - `adsemailadm` with 12 command groups (60+ commands)
- **Legacy CLI** - `mailctl` with SASL authentication (v1.0 compatibility)
- **Elasticsearch** - Mail event logging and search (if enabled)

---

## Architecture Overview

### Kubernetes Deployment Architecture (v2.0)

```
Global Multi-Region Deployment:

┌─────────────────────────────────────────────────────────────────┐
│                    Global Routing Layer                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │ Health Check │  │Latency Track │  │ Cost Routing │         │
│  │   (etcd)     │  │   (Redis)    │  │   (Config)   │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
└─────────────────────────────────────────────────────────────────┘
         │                    │                    │
    ┌────┴────┐          ┌────┴────┐          ┌────┴────┐
    │ Region  │          │ Region  │          │ Region  │
    │  US-W   │          │  US-E   │          │   EU    │
    └─────────┘          └─────────┘          └─────────┘

Region Deployment (Kubernetes):

  Internet Traffic
         ↓
  ┌─────────────────────────────────────────┐
  │     Perimeter MTA (LoadBalancer)        │
  │   ┌──────┐ ┌──────┐ ┌──────┐          │
  │   │ Pod1 │ │ Pod2 │ │ Pod3 │ (HPA 3-20)│
  │   └──────┘ └──────┘ └──────┘          │
  │   :25 :587 :465 (SMTP/Submission)      │
  │   • Access Control (Postfix-style)      │
  │   • RBL/DNSBL Checking                 │
  │   • Greylisting                        │
  │   • TLS Required                       │
  └─────────────────────────────────────────┘
         ↓
  ┌─────────────────────────────────────────┐
  │     Internal Hub (ClusterIP)            │
  │   ┌──────┐ ┌──────┐ ┌──────┐          │
  │   │ Pod1 │ │ Pod2 │ │ Pod3 │ (5 pods)  │
  │   └──────┘ └──────┘ └──────┘          │
  │   :2525 (Internal SMTP)                │
  │   • Policy Engine (Starlark)           │
  │   • Multi-tier Queues                  │
  │   • Global Routing Logic               │
  └─────────────────────────────────────────┘
         ↓
  Message Store (WAL + Journal)
         ↓
  Multi-tier Queue (1,050 workers per pod)
     ├─ Emergency  (50 workers, unlimited rate)
     ├─ MSA        (200 workers, 1000/s)
     ├─ Internal   (500 workers, 5000/s) ← HIGHEST VOLUME
     ├─ Outbound   (200 workers, 500/s)
     └─ Bulk       (100 workers, 100/s)
```

### Standalone Deployment (v1.0 compatible)

```
  Internet
     ↓
  :2525 SMTP Server
     ├─ Access Control (Postfix-style) ← NEW v2.0
     ├─ SPF Verification (enforced) ← DNS Resolver (cached)
     ├─ DKIM Verification (logging) ← DNS Resolver (cached)
     ├─ Policy Engine (Starlark) ← NEW v2.0
     ├─ Greylisting (optional)
     ├─ SASL Authentication ← Account Lockout Protection
     └─ TLS (STARTTLS)
        ↓
  Message Store (WAL + Journal)
     ↓
  Multi-tier Queue (1,050 workers)
     ├─ Emergency  (50 workers, unlimited rate)
     ├─ MSA        (200 workers, 1000/s)
     ├─ Internal   (500 workers, 5000/s) ← HIGHEST VOLUME
     ├─ Outbound   (200 workers, 500/s)
     └─ Bulk       (100 workers, 100/s)
        ↓
     Real SMTP delivery (MX lookup, DANE-aware TLS, RFC 5321 client)
        ↓
     "delivered" status (recipient outcomes tracked per domain)

Parallel Services:
  ├─ :1143  IMAP Server (mail retrieval)
  ├─ :8080  REST API (management + metrics + policies)
  ├─ :50051 gRPC API (placeholder)
  └─ adsemailadm CLI (10 command groups)
```

---

## Quick Start

### Build
```bash
go build -o bin/goemailservices ./cmd/goemailservices
go build -o bin/adsemailadm ./cmd/adsemailadm
go build -o bin/mailctl ./cmd/mailctl  # legacy CLI
```

### Run (Standalone)
```bash
./bin/goemailservices --config config.yaml > service.log 2>&1 &
```

### Run (Kubernetes - Perimeter MTA)
```bash
# Apply base resources (namespace, RBAC, ConfigMap)
kubectl apply -f deploy/kubernetes/base/

# Deploy perimeter MTA (internet-facing)
kubectl apply -f deploy/kubernetes/perimeter/

# Verify deployment
kubectl get pods -n email-system
kubectl get svc -n email-system
```

### Run (Kubernetes - Internal Hub)
```bash
# Apply base resources
kubectl apply -f deploy/kubernetes/base/

# Deploy internal hub (internal routing)
kubectl apply -f deploy/kubernetes/internal/

# Verify deployment
kubectl get pods -n email-system
```

### Test
```bash
# Send test email
python3 tests/test_smtp.py

# Check queue (new admin CLI)
./bin/adsemailadm queue stats

# Check policy engine
./bin/adsemailadm policy list

# Legacy CLI still works
./bin/mailctl --username admin --password changeme queue stats

# View metrics
curl http://localhost:8080/metrics
```

### Docker
```bash
# Build and run
./deploy.sh build
./deploy.sh up

# Or manually:
docker build -t afterdarksys/go-emailservice-ads:latest .
docker run -p 2525:2525 -p 8080:8080 afterdarksys/go-emailservice-ads:latest
```

---

## Configuration

Use the [2.7 configuration guide](docs/CONFIGURATION.md) for a complete starting
example, setting precedence, secret references, listener roles and update behavior.
The root `config.yaml` is a development example with placeholders and optional
features. Do not deploy it unchanged. See [compliance configuration](examples/compliance-config.yaml)
for bounce, evidence and OAuth settings.

## Admin CLI (adsemailadm) - v2.2

The `adsemailadm` binary remains in the repository, but its command surface includes
routes not mounted by the standard 2.7 server. Use the [REST API reference](docs/API_REFERENCE.md)
and [administration guide](docs/ADMINISTRATION.md) for supported management actions.
Verify individual legacy CLI commands before automation; their presence is not a
runtime capability guarantee.

## Legacy CLI (mailctl) - v1.0

Legacy `mailctl` commands are not the authoritative management contract. In
particular, Basic Auth is not accepted and replication is not configured by the
standard executable. Use scoped Bearer requests and [fenced failover tooling](docs/FAILOVER.md).

## API Endpoints

Use the [2.7 REST API reference](docs/API_REFERENCE.md) for the complete active
route inventory, scopes, payloads, errors and examples. [API authentication](API_AUTHENTICATION.md)
uses scoped Bearer credentials; Basic Auth is not accepted by the management API.
The separate admin router and some legacy CLI commands are not wired into the
standard executable. Management gRPC and replication promotion are unavailable.

## Performance Characteristics

### Queue Processing
- **Capacity:** 1,050 workers × 100 msg/s = **8.6 million messages/day**
- **Current:** Real SMTP delivery (DNS/MX lookup, DANE-aware TLS, ~200-500ms per message)
- **Effective throughput:** ~1,000-5,000 msg/s = **86-432 million/day**

### Disaster Recovery
- **WAL:** Write-ahead logging for crash recovery
- **Tested:** ✅ Recovered 12 messages after process crash (PID 79253)
- **Replication:** Code exists for primary/secondary/standby modes

### Anti-Spam Performance
- **SPF:** Enforced, rejects unauthorized senders
- **DKIM:** Verifies signatures, logs results
- **Greylisting:** 50-90% spam reduction (5-minute delay for unknowns)
- **DNS Cache:** 200-500x speedup on repeated lookups

### Rate Limiting (per tier)
- Emergency: Unlimited
- MSA: 1,000 msg/s (burst 2,000)
- Internal: 5,000 msg/s (burst 10,000) ← Highest
- Outbound: 500 msg/s (burst 1,000)
- Bulk: 100 msg/s (burst 500)

---

## Security Features

### Active and Enforced ✅
1. **DANE/TLSA Validation** ⭐ **NEW!** - Certificate authentication via DNS with DNSSEC (RFC 7672)
2. **SPF Verification** - Rejects mail from unauthorized IPs (RFC 7208)
3. **Enhanced Authentication** - Account lockout after failed attempts
4. **DNS Caching** - All lookups cached (5min TTL)
5. **Modern TLS** - TLS 1.2/1.3, ECDHE, PFS
6. **IMAP Auth** - Shared authentication with SMTP

### Active but Not Enforced ✅
1. **DKIM Verification** - Verifies signatures, logs results (RFC 6376)
2. **DMARC Verification** - Reject/quarantine enforcement wired in; monitor mode by default (RFC 7489)
3. **DKIM Signing** - Signs outbound mail when a key is configured; disabled by default until one is provisioned (RFC 6376)
4. **Greylisting** - Available, disabled by default (enable in config)

### Deployed but Not Integrated ⏸️
1. **Directory Service** - Client exists (needs endpoint config)

**See:** `SECURITY_FEATURES.md` for detailed documentation
**NEW:** `docs/DANE_IMPLEMENTATION.md` for complete DANE guide

---

## Outbound SMTP Delivery

**Implementation:** `internal/smtpd/queue.go`'s `deliverRemote` calls `internal/delivery.MailDelivery.Deliver`, which performs:
1. MX record lookup for the recipient domain (`internal/dns`)
2. DANE/TLSA-aware TLS negotiation, failing closed on bogus DNSSEC (RFC 7672)
3. SMTP handshake (EHLO, MAIL FROM, RCPT TO, DATA) with connection pooling
4. Message transmission
5. Per-recipient response code handling (2xx success, 4xx retry, 5xx permanent failure), tracked individually so one failed domain can't mask a successful one

---

## Documentation

### Core Documentation
- **`README.md`** - This file (overview)
- **`CHANGELOG.md`** - Complete version history and release notes
- **`WORKER_ARCHITECTURE.md`** - Deep dive on 1,050-worker system
- **`SECURITY_FEATURES.md`** - Complete security features documentation
- **`SECURITY_QUICK_START.md`** - Testing and enabling security features
- **`DEPLOYED_FEATURES.md`** - What's active vs what exists
- **`POSTFIX_FEATURES.md`** - Missing Postfix features analysis
- **`README_TESTING.md`** - Testing instructions

### v2.0 Documentation
- **`KUBERNETES_ENTERPRISE_ARCHITECTURE.md`** - Complete Kubernetes architecture (500+ lines)
- **`IMPLEMENTATION_SUMMARY.md`** - v2.0 implementation summary
- **`POLICY_ENGINE_DESIGN.md`** - Policy system design and Starlark scripting
- **`CLUSTER_ARCHITECTURE.md`** - Cluster management and state coordination
- **`deploy/kubernetes/README.md`** - Comprehensive Kubernetes deployment guide

### Deployment
- **`Dockerfile`** - Multi-stage optimized build (31.9 MB)
- **`docker-compose.yml`** - Full HA stack deployment
- **`deploy.sh`** - Deployment automation
- **`deploy/kubernetes/`** - Production Kubernetes manifests

---

## Directory Structure

```
go-emailservice-ads/
├── cmd/
│   ├── goemailservices/     # Main SMTP/IMAP service
│   ├── adsemailadm/         # NEW v2.0: Admin CLI (10 command groups)
│   └── mailctl/             # Legacy v1.0 CLI
├── internal/
│   ├── access/              # NEW v2.0: Postfix-style access control
│   │   ├── maps/            # Lookup map implementations (20+ types)
│   │   ├── restrictions.go  # Restriction manager
│   │   └── types.go         # Core access control types
│   ├── ai/                  # NEW v2.0: AI/ML integration
│   ├── api/                 # REST/gRPC API (enhanced with policy endpoints)
│   ├── auth/                # Authentication + account lockout
│   ├── config/              # Configuration loading
│   ├── delivery/            # Message delivery engine
│   ├── directory/           # Directory service client
│   ├── dns/                 # DNS resolver with caching
│   ├── greylisting/         # Anti-spam greylisting
│   ├── imap/                # IMAP server (full implementation)
│   ├── jmap/                # JMAP reads, composition/submission, upload/import, mutations and change feeds
│   ├── k8s/                 # NEW v2.0: Kubernetes integration
│   │   ├── discovery.go     # Service discovery
│   │   └── deployment_mode.go # Mode detection
│   ├── master/              # Master control system
│   ├── metrics/             # Prometheus metrics
│   ├── netutil/             # Network utilities
│   ├── policy/              # Policy engine (Starlark scripting)
│   ├── replication/         # Disaster recovery replication
│   ├── routing/             # NEW v2.0: Global routing engine
│   │   ├── global.go        # Cross-region routing
│   │   └── health.go        # Regional health checks
│   ├── security/            # SPF, DKIM, DMARC, DANE, ARC
│   │   └── dane/            # DANE/TLSA implementation
│   ├── smtpd/               # SMTP server + queue
│   └── storage/             # Persistent storage + WAL
├── msgfmt/                  # NEW v2.0: ADS Mail Format (AMF)
│   ├── types.go             # Message format types
│   ├── reader.go            # AMF reader
│   ├── writer.go            # AMF writer
│   ├── converter.go         # EML/mbox conversion
│   ├── utils.go             # Utilities (encryption, signing)
│   └── examples/            # Usage examples
├── policies/                # NEW v2.0: Starlark policy scripts
│   ├── 10_ratelimit.star
│   ├── 20_spamcheck.star
│   └── ... (8 policies)
├── tests/                   # Python test suite
├── deploy/
│   ├── kubernetes/          # NEW v2.0: Production K8s manifests
│   │   ├── base/            # Namespace, RBAC, ConfigMap, NetworkPolicy
│   │   ├── perimeter/       # Perimeter MTA deployment
│   │   └── internal/        # Internal hub deployment
│   ├── nginx/               # NEW v2.0: NGINX configs
│   ├── scripts/             # NEW v2.0: Deployment scripts
│   └── systemd/             # NEW v2.0: Systemd service files
├── docs/                    # NEW v2.0: Additional documentation
├── data/
│   ├── certs/               # TLS certificates
│   └── mail-storage/        # Message storage + journal
├── Dockerfile               # Optimized container (31.9 MB)
├── docker-compose.yml       # HA deployment
└── deploy.sh                # Deployment automation
```

---

## Dependencies

```go
require (
    github.com/emersion/go-imap v1.2.1       // IMAP server
    github.com/emersion/go-msgauth v0.7.0    // DKIM verification
    github.com/emersion/go-smtp v0.24.0      // SMTP server
    github.com/google/uuid v1.6.0            // Message IDs
    github.com/spf13/cobra v1.8.0            // CLI framework
    go.starlark.net v0.0.0-...               // Policy scripting
    go.uber.org/zap v1.27.1                  // Structured logging
    golang.org/x/crypto v0.31.0              // Cryptography
    golang.org/x/time v0.5.0                 // Rate limiting
    gopkg.in/yaml.v3 v3.0.1                  // Config parsing

    // v2.0 Kubernetes Integration
    k8s.io/api v0.28.0                       // Kubernetes API types
    k8s.io/apimachinery v0.28.0              // Kubernetes API machinery
    k8s.io/client-go v0.28.0                 // Kubernetes Go client

    // v2.1 Elasticsearch Integration
    github.com/elastic/go-elasticsearch/v8 v8.12.0   // Elasticsearch client
    github.com/elastic/elastic-transport-go/v8 v8.4.0 // Elasticsearch transport

    // v2.1 OAuth2/SSO Integration
    golang.org/x/oauth2 v0.8.0               // OAuth2 client

    // v2.1 AfterSMTP - Additional dependencies loaded dynamically
    // Substrate, QUIC, gRPC dependencies in internal/aftersmtplib/
)
```

---

## Deployment Options

### 1. Standalone Binary
```bash
./bin/goemailservices --config config.yaml
```

### 2. Docker
```bash
docker build -t afterdarksys/go-emailservice-ads:latest .
docker run -p 2525:2525 -p 8080:8080 afterdarksys/go-emailservice-ads:latest
```

### 3. Docker Compose (HA)
```bash
./deploy.sh up
# Starts: primary, secondary, prometheus, grafana
```

### 4. Kubernetes - Perimeter MTA (Internet-facing)

Deploy as an internet-facing MTA with LoadBalancer, ports 25/587/465:

```bash
# Apply base resources
kubectl apply -f deploy/kubernetes/base/namespace.yaml
kubectl apply -f deploy/kubernetes/base/rbac.yaml
kubectl apply -f deploy/kubernetes/base/configmap.yaml
kubectl apply -f deploy/kubernetes/base/network-policy.yaml

# Deploy perimeter MTA
kubectl apply -f deploy/kubernetes/perimeter/deployment.yaml
kubectl apply -f deploy/kubernetes/perimeter/service.yaml
kubectl apply -f deploy/kubernetes/perimeter/hpa.yaml

# Verify
kubectl get pods -n email-system
kubectl get svc -n email-system

# Expected:
# - 3-20 pods (HPA scaling)
# - LoadBalancer service on ports 25, 587, 465
# - Automatic scaling based on CPU, memory, queue depth, connections
```

**Features:**
- LoadBalancer service (public IP)
- HPA: 3-20 replicas
- Ports: 25 (SMTP), 587 (Submission), 465 (SMTPS)
- Access control enabled
- RBL checking enabled
- Greylisting enabled
- TLS required

### 5. Kubernetes - Internal Hub (Internal routing)

Deploy as an internal mail hub for cross-region routing:

```bash
# Apply base resources (if not already applied)
kubectl apply -f deploy/kubernetes/base/

# Deploy internal hub
kubectl apply -f deploy/kubernetes/internal/deployment.yaml
kubectl apply -f deploy/kubernetes/internal/service.yaml

# Verify
kubectl get pods -n email-system
kubectl get svc -n email-system

# Expected:
# - 5 replicas (manual scaling)
# - ClusterIP service (internal only)
# - Global routing enabled
```

**Features:**
- ClusterIP service (internal only)
- 5 replicas (manual scaling recommended)
- Port: 2525 (internal SMTP)
- Policy engine enabled
- Global routing enabled
- Multi-tier queues
- Service discovery

### 6. Kubernetes - Hybrid Mode

Run both perimeter and internal hub in the same cluster:

```bash
# Apply base resources
kubectl apply -f deploy/kubernetes/base/

# Deploy both perimeter and internal
kubectl apply -f deploy/kubernetes/perimeter/
kubectl apply -f deploy/kubernetes/internal/

# Verify
kubectl get pods -n email-system

# Expected:
# - email-service-perimeter-* pods (3-20)
# - email-service-internal-* pods (5)
# - 2 services (LoadBalancer + ClusterIP)
```

### 7. Multi-Region Kubernetes

Deploy across multiple regions with global routing:

```bash
# Region 1 (us-west-2)
kubectl --context=us-west-2 apply -f deploy/kubernetes/base/
kubectl --context=us-west-2 apply -f deploy/kubernetes/perimeter/

# Region 2 (us-east-1)
kubectl --context=us-east-1 apply -f deploy/kubernetes/base/
kubectl --context=us-east-1 apply -f deploy/kubernetes/perimeter/

# Region 3 (eu-west-1)
kubectl --context=eu-west-1 apply -f deploy/kubernetes/base/
kubectl --context=eu-west-1 apply -f deploy/kubernetes/perimeter/

# Deploy global coordination (etcd cluster)
kubectl --context=global apply -f deploy/kubernetes/global/etcd-cluster.yaml

# Configure global routing in each region's ConfigMap
# Update state_store endpoints to point to etcd cluster
```

**Features:**
- Cross-region message routing
- Latency-based routing
- Cost-optimized routing
- Health-based failover
- Regional load balancing

---

## Monitoring

### Metrics (Prometheus)
```bash
curl http://localhost:8080/metrics

# mail_queue_enqueued{tier="int"} 12
# mail_queue_processed{tier="int"} 12
# mail_queue_failed{tier="int"} 0
# mail_storage_total 12
# mail_storage_pending 0
# mail_storage_dlq 0
```

### Health Checks
```bash
# Liveness
curl http://localhost:8080/health
# {"status":"ok","uptime":"1h2m3s"}

# Readiness
curl http://localhost:8080/ready
# {"status":"ready","checks":{"storage":true,"queue":true}}
```

### Logs
```bash
tail -f service.log

# Security events
tail -f service.log | grep -E 'SPF|DKIM|Auth'

# Queue activity
tail -f service.log | grep -E 'Enqueued|Processed'

# Errors
tail -f service.log | grep ERROR
```

---

## Testing

### Unit Tests
```bash
go test ./...
```

### Integration Tests
```bash
cd tests
python3 test_smtp.py      # SMTP tests
python3 test_imap.py       # IMAP tests (if exists)
python3 test_security.py   # Security features (if exists)
```

### Load Testing
```bash
# TODO: Add load testing scripts
# Target: Verify 1,000-5,000 msg/s sustained throughput
```

---

## Troubleshooting

Use the [troubleshooting runbook](docs/TROUBLESHOOTING.md) for SMTP admission,
queue/delivery, TLS, API, scanner and storage failures. Start with version,
`/health`, `/ready`, queue age and the exact SMTP/HTTP error; preserve evidence
before recovery operations.

## Production Checklist

The maintained production checklist is in [TODO](TODO#production-qualification-and-operations).
Follow [deployment qualification](docs/DEPLOYMENT_QUALIFICATION.md),
[backup/recovery](docs/BACKUP_RECOVERY.md), [monitoring](docs/MONITORING.md) and
[configuration](docs/CONFIGURATION.md). Local tests do not qualify production
identity providers, Object Lock destinations or fencing APIs.

## Known Issues

See [TODO](TODO#engineering-gaps-and-deferred-enhancements) for current engineering
gaps. Real SMTP delivery, DKIM signing and DMARC enforcement are implemented;
signing keys/DNS and enforcement rollout require configuration. The standard
executable does not enable replication, some IMAP mutations remain unsupported,
and general configuration reload requires restart. See the current operational
guides for supported behavior.

## Contributing

### Code Style
- Follow standard Go conventions
- Use `gofmt` for formatting
- Add godoc comments for public APIs
- Write tests for new features

### Testing
- Unit tests for business logic
- Integration tests for protocols
- Security tests for auth/crypto

### Documentation
- Update README for new features
- Add godoc comments
- Create examples for complex features

---

## License

Internal use only - msgs.global infrastructure

---

## Support

- **Issues:** Report at msgs.global internal tracker
- **Documentation:** See docs/ directory
- **Contact:** Internal email infrastructure team

---

## Roadmap

[TODO](TODO) is the authoritative current backlog. [The planning roadmap](.planning/ROADMAP.md)
maps historical milestone proposals to delivered work and remaining requirements.
Do not use the old release-era feature checklists to assess current completion.

## Summary

Current release information is in [CHANGELOG.md](CHANGELOG.md). Follow the
[documentation index](docs/README.md) for operation and integration, and
[deployment qualification](docs/DEPLOYMENT_QUALIFICATION.md) for remaining gates.
