# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- JMAP immutable thread grouping, collapsed queries, Thread/get/changes and authenticated SSE push.
- LDAP/AD verification for provisioned accounts, scoped SCIM Users provisioning and audience-bound federated JMAP tokens.
- Durable DMARC aggregate XML reporting with external destination authorization, plus active operational statistics APIs.
- Validated general configuration reload through graceful process replacement.
- Offline personal-data export/deletion with retained-evidence and external-disposition records.
- Measured metadata-only mailbox listing, cached thread identity and bounded DNS negative caching.
- `--check-config` validates configuration without creating defaults, opening listeners, acquiring storage locks or starting delivery.

### Fixed
- Local identity disablement applies before external authentication; SSO requires provisioning.
- Storage deletion removes payloads from the live index before compaction, and SCIM account metadata preserves quota lookup behavior.
- Unknown YAML fields and additional configuration documents now fail loading instead of being silently ignored.
- Mailbox create/update rejects passwords longer than bcrypt's 72-byte limit with HTTP 400 before changing account state.

### Upgrade notes
- Remove or correct previously ignored YAML settings before upgrading; run `--check-config --config /path/to/config.yaml`. This validates configuration rules, not runtime files or external dependencies.

## [2.7.0] - 2026-09-06

### Added
- SMTP DSN preferences, durable one-time delay/success notices, incoming report tracking and configured recipient suppression.
- Domain-specific compliance access grants with separate read/export/release/legal-hold/delete scopes.
- S3 Object Lock COMPLIANCE artifact preservation with retention verification and an isolated WORM/recovery drill.
- Production OAuth qualification command for valid tokens, denied scopes and revoked tokens.
- End-to-end authenticated SMTP/IMAP, shutdown and restore qualification in the release script.

### Fixed
- Compliance release now commits the decision and delivery transaction atomically, surviving restart and compaction without recreating completed deliveries.
- Container command builds use the target architecture and package the new operational utilities.

## [2.6.0] - 2026-09-06

### Added
- File-managed bounce identity, header privacy, retry limits and effective-settings API.
- Domain compliance engine with hold/intercept, preserved monitoring copies, and envelope BCC rules.
- Dedicated evidence queues, audited exports, legal holds, retention-protected deletion and explicit release.
- OAuth access-token introspection with issuer, audience, expiry and resource-scope validation.
- Verifiable audit hash chains and audit-aware backup verification.
- Configurable JSON, YAML and RFC 5424 operational output; mailhub-log format detection/conversion and audit verification.

### Fixed
- Exhausted delivery retries now generate a final DSN before entering the failed queue.
- Bounce fields reject line injection and bounded original-header inclusion avoids malformed body disclosure.
- Generic queue controls cannot access or mutate compliance evidence.
- Existing queued mail is intercepted when a domain hold is enabled before dispatch.

## [2.5.0] - 2026-09-06

### Added
- Offline verified backups, staged restore drills, and fenced standby activation tooling.
- Durable policy CRUD and isolated policy testing.
- Destination concurrency limits, adaptive backoff, and fair retries.
- Live operational metrics, alert rules, authenticated synthetic mail probe.
- Reloadable TLS/mTLS certificates and overlapping expiring API credentials.
- Release CI for tests, race checks, containers and real scanner checks.
- Starlark immutable message views, filter entry points, CIDR matching, bounded DNS and diagnostic traces.

### Fixed
- Bounded API lifecycle and startup failure propagation; removed nonfunctional gRPC listener.
- DNS MX absence, Null MX, and temporary lookup failures now have distinct delivery outcomes.
- TLS reports aggregate repeated failures and cannot generate reporting feedback.
- Restore decoding rejects trailing payloads and unsafe paths, honors cancellation, and syncs restored directories.

## [2.4.0] - 2026-09-06

### Added
- IP/CIDR allowlists and denylists, bounded DNSBL lookups, and premail/HTTP reputation integration.
- Internal, perimeter, and authenticated submission listener roles; trusted original-client metadata.
- Explicit next-hop connectors with verified TLS, SMTP authentication, and failover.
- Required Rspamd scanning, ARC response handling, durable quarantine with rescanning and audit records.
- Adaptive mailstorm admission and queued-delivery pauses, duplicate detection, learned volume baselines, escalating persistent circuit breakers, and operator pause/resume API.
- Spool/mailbox quotas, recipient directory checks, aliases, durable MTA-STS cache, and TLS report delivery.

### Fixed
- SMTP, IMAP, and mailbox administration now share live persistent identities.
- Recipient validation excludes missing and disabled accounts; password changes preserve disabled status.
- Queue ownership, journal updates, retry accounting, recipient checkpoints, local delivery idempotence, expunge cleanup, and journal compaction.
- Starlark action isolation, policy result propagation, timeout cancellation, and atomic policy loading.
- API permission checks now enforce resource scopes and reject unscoped keys.
- Deployment volumes match runtime paths; stateful deployments use one owner and Recreate updates.

### Upgrade notes
- Review [platform operations](docs/PLATFORM_OPERATIONS.md) before upgrading. Persistent identities and explicit API scopes change legacy defaults.
- Local state is single-owner; replicated/high-availability storage is not implemented.
- ARC signing requires configured Rspamd signing keys and DNS records; the old standalone ARC code is not used by SMTP.

## [2.1.0] - 2026-03-09

### Added - Elasticsearch Integration for Mail Event Logging

#### Complete Mail Event Logging to Elasticsearch
- **Async Bulk Indexing** - Non-blocking event publishing with buffered channels and bulk indexer
- **Message Correlation** - Track messages across instances with TraceID, InstanceID, ParentTraceID
- **Content Deduplication** - SHA256 content hash for identifying duplicate messages
- **Event Tracking** - Log events at every lifecycle point (enqueue, processing, delivered, failed, bounce, retry, DLQ)
- **Time-Based Indices** - Daily indices with format `mail-events-YYYY.MM.DD`
- **Index Lifecycle Management** - Automatic hot/warm/delete transitions with configurable retention

#### Smart Header Logging with Privacy Controls
- **Per-Domain Control** - Allow/deny lists for specific email domains
- **Per-IP Control** - Allow/deny lists with CIDR notation support
- **Per-MX Control** - Filter by remote MX records
- **Selective Headers** - Choose specific headers to log or log all
- **Redaction Patterns** - Regex-based sensitive data redaction
- **Privacy First** - Disabled by default, opt-in per domain/IP/MX

#### Advanced Search and Analytics
- **Rich Event Data** - Envelope, metadata, security checks, delivery info, policy results, errors
- **Security Tracking** - SPF, DKIM, DMARC, DANE results logged per message
- **Delivery Metrics** - Latency, SMTP codes, attempt numbers, retry tracking
- **Policy Integration** - Log policy decisions, scores, and applied policies
- **Cross-Instance Tracking** - Follow messages through multi-region Kubernetes deployments

#### Optimized Index Mappings
- Email-specific analyzer for address fields
- IP type for proper IP address indexing
- Keyword fields for IDs, domains, and exact matching
- Dynamic object mapping for message headers
- Best compression codec for storage efficiency

#### Performance Features
- **Sampling** - Configurable sampling rate (0.0-1.0) for high-volume tiers
- **Bulk Indexing** - Configurable bulk size (default: 1000) and flush interval (default: 5s)
- **Worker Pools** - Configurable number of bulk indexer workers (default: 4)
- **Statistics** - Track events indexed, failed, dropped, and bytes indexed
- **Graceful Shutdown** - Flush all pending events on shutdown

#### Documentation
- Complete `ELASTICSEARCH_INTEGRATION.md` with setup, configuration, and query examples
- Kibana dashboard templates (operations, security, performance, troubleshooting)
- Sample queries for common use cases (correlation, analytics, debugging)

### Added - AfterSMTP Next-Generation Protocol

#### AMP (AfterSMTP Messaging Protocol)
- **QUIC Transport** - HTTP/3 based messaging with multiplexing and zero-RTT
- **gRPC Streaming** - Native gRPC bidirectional streaming for real-time message flow
- **Blockchain Ledger** - Substrate-based distributed ledger for audit trails and message verification
- **Fallback Database** - SQLite fallback when blockchain is unavailable

#### AfterSMTP Service Features
- **Bridge Service** - Converts legacy SMTP to modern AMP protocol
- **Protocol Translation** - Seamless legacy SMTP ↔ AMP/QUIC/gRPC translation
- **Identity Management** - Substrate-based identity verification
- **Cryptographic Verification** - Message signing and verification
- **Audit Trail** - Immutable blockchain records of all messages

#### Security Enhancements
- **MTA-STS Support** - Mail Transfer Agent Strict Transport Security
- **TLS Reporting** - SMTP TLS reporting (RFC 8460)
- **Enhanced DANE** - Extended DANE/TLSA verification
- **ARC Support** - Authenticated Received Chain (RFC 8617)

### Added - Single Sign-On (SSO) Integration

#### After Dark Systems SSO
- **OAuth2/OIDC** - Full OAuth2 and OpenID Connect support
- **Directory Integration** - Connect to After Dark Systems Directory Service
- **Token Management** - Secure token storage and refresh
- **User Provisioning** - Automatic user creation from SSO
- **Multi-Provider Support** - Pluggable provider architecture

#### SSO Features
- Configurable OAuth2 endpoints (auth, token, userinfo)
- Custom scopes support
- Environment variable expansion for secrets
- Integrated with both SMTP and IMAP authentication
- Fallback to local authentication when SSO unavailable
- Complete `SSO_SETUP.md` documentation

### Enhanced - Message Correlation and Tracking

#### Global Message Tracking
- **TraceID** - Unique global identifier for message correlation across instances
- **ParentTraceID** - Link bounces and retries to original messages
- **InstanceID** - Track which pod/instance handled the message
- **ContentHash** - SHA256 hash for deduplication across the system
- **Client Metadata** - Track client IP, HELO hostname, authenticated user

#### Instance Identification
- Kubernetes pod name detection (`HOSTNAME`, `POD_NAME` env vars)
- Fallback to system hostname
- Consistent instance tracking across multi-region deployments

### Dependencies Added
- `github.com/elastic/go-elasticsearch/v8` v8.12.0 - Elasticsearch client
- `github.com/elastic/elastic-transport-go/v8` v8.4.0 - Elasticsearch transport
- OpenTelemetry dependencies for Elasticsearch observability

### Configuration Enhancements
- **Elasticsearch Configuration Block** - Complete ES config with header logging controls
- **AfterSMTP Configuration Block** - QUIC, gRPC, ledger URL configuration
- **SSO Configuration Block** - OAuth2/OIDC provider settings
- Environment variable expansion for all secret fields

## [2.0.0] - 2026-03-09

### Added - Kubernetes Enterprise Platform

#### Kubernetes Integration
- **Service Discovery** - Automatic peer detection using K8s API
- **Deployment Mode Detection** - Perimeter MTA, Internal Hub, Hybrid, Standalone modes
- **Global Routing Engine** - Cross-region, cross-datacenter message routing
- **Health Checking** - Regional health monitoring with automatic failover
- **Latency Tracking** - Inter-region latency measurement for optimal routing
- **Cost-Aware Routing** - Minimize data transfer costs across regions

#### Production Kubernetes Manifests
- **Perimeter MTA Deployment** - Internet-facing with LoadBalancer, HPA (3-20 replicas)
- **Internal Hub Deployment** - ClusterIP service for internal routing (5 replicas)
- **Horizontal Pod Autoscaler** - CPU, memory, queue depth, connection-based scaling
- **Network Policies** - Zero-trust security isolation
- **RBAC Configuration** - ServiceAccount, Role, RoleBinding for pod permissions
- **ConfigMaps** - Complete configuration management
- **Comprehensive Documentation** - Full deployment guide with multi-region setup

#### Deployment Features
- Auto-scaling based on custom metrics (queue depth, active connections)
- Rolling updates with zero downtime (maxSurge: 1, maxUnavailable: 0)
- Pod anti-affinity for high availability across zones
- Security contexts (non-root, read-only filesystem, dropped capabilities)
- Liveness and readiness probes
- Resource requests and limits
- TLS certificate management via Secrets

### Added - Postfix-Style Access Control

#### Lookup Map System (20+ Types)
- **Database Maps**: hash, btree, dbm, lmdb, cdb, sdbm
- **SQL Databases**: mysql, pgsql, sqlite (stubs ready for integration)
- **LDAP**: ldap, ldaps (stub for directory integration)
- **Network Services**: memcache, tcp, socketmap, proxy
- **File-Based Maps**: regexp, pcre, cidr, texthash, inline, static
- **Special Maps**: nis, nisplus, fail, environ, unionmap, pipemap

#### SMTP Restriction Classes (15+ Types)
- **Network Restrictions**: permit_mynetworks, permit_sasl_authenticated
- **Relay Restrictions**: reject_unauth_destination, permit_auth_destination
- **DNS Restrictions**: reject_unknown_sender_domain, reject_unknown_recipient_domain, reject_unknown_client_hostname
- **RBL Restrictions**: reject_rbl_client, reject_rhsbl_sender, reject_rhsbl_recipient
- **Access Map Restrictions**: check_client_access, check_sender_access, check_recipient_access, check_helo_access
- **Policy Service**: check_policy_service (external policy server)
- **Basic Actions**: permit, reject, defer, defer_if_permit, defer_if_reject

#### Access Control Features
- Stage-based restrictions (client, helo, sender, recipient, data, end_of_data, etrn)
- CIDR network matching for mynetworks
- DNS lookups for domain validation
- RBL/DNSBL integration
- Custom SMTP codes (4xx/5xx) support
- Map result parsing (OK, REJECT, DEFER, DUNNO, DISCARD, HOLD, WARN)
- Restriction classes for reusable rule sets

### Added - Admin CLI (`adsemailadm`)

#### 10 Command Groups (50+ Commands)
1. **Queue Management** - stats, list, retry, purge, inspect, dlq operations
2. **Policy Management** - list, show, test, reload, stats, validate
3. **Mailbox Management** - list, create, delete, quota, alias, routing
4. **TLS/SSL Management** - status, cert operations, test, DANE
5. **Monitoring** - real-time dashboard, statistics, Prometheus metrics
6. **Directory Services** - LDAP test, config, sync, user lookup
7. **Configuration** - show, validate, reload, set
8. **Cluster Management** - status, nodes, load, rebalance, drain
9. **Security** - audit logs, SPF/DKIM/DMARC checks, RBL lookup
10. **Health Checks** - comprehensive system status

#### CLI Features
- JSON output mode (`--json`)
- Global flags (--api, --user, --password, --config, --verbose)
- Tabular output formatting
- Real-time monitoring with ASCII art
- Color-coded status indicators
- API integration with Basic auth

### Added - Policy Engine Enhancements

#### Policy Manager Integration
- Integrated with REST API server
- Policy endpoints: list, create, update, delete, test, reload, stats
- Hot reload support (zero downtime policy updates)
- Metrics tracking (evaluations, errors, cache size)
- 8 example policies included

#### Starlark Built-ins (25+ Functions)
- Email inspection: has_header(), get_header(), get_body(), get_attachments()
- Envelope: get_from(), get_to(), get_remote_ip()
- Security: check_spf(), check_dkim(), check_dmarc(), check_rbl(), get_ip_reputation()
- Actions: accept(), reject(), defer(), discard(), redirect(), fileinto()
- Headers: add_header(), remove_header()
- Utilities: match_pattern(), lookup_dns(), is_in_group(), log(), notify()

### Changed

#### API Server Improvements
- Added policy manager to API server initialization
- Policy manager now shared between SMTP and API servers
- Added policy-specific endpoints with real data
- Enhanced health and readiness checks

#### SMTP Server Updates
- Policy manager passed as parameter (removed internal initialization)
- Better integration with global routing
- Support for access control framework (ready for integration)

### Documentation

#### New Documentation Files
- `KUBERNETES_ENTERPRISE_ARCHITECTURE.md` - Full K8s architecture (500+ lines)
- `IMPLEMENTATION_SUMMARY.md` - Complete session summary
- `POLICY_ENGINE_DESIGN.md` - Policy system design
- `CLUSTER_ARCHITECTURE.md` - Cluster and state management
- `deploy/kubernetes/README.md` - Comprehensive deployment guide

#### Updated Documentation
- README.md - Updated with new features
- CHANGELOG.md - Created comprehensive changelog

### Technical Details

#### Dependencies Added
- `k8s.io/client-go@v0.28.0` - Kubernetes Go client
- `k8s.io/api@v0.28.0` - Kubernetes API types
- `k8s.io/apimachinery@v0.28.0` - Kubernetes API machinery

#### File Statistics
- **100+ new files** created
- **10,000+ lines** of production code
- **21 files** in access control system (1,500+ lines)
- **30+ Kubernetes manifests** for production deployment

### Testing

- ✅ All builds passing (`go build ./cmd/...`)
- ✅ API server tested with `adsemailadm` CLI
- ✅ Policy manager verified (8 policies loaded)
- ✅ Queue management endpoints functional
- ✅ Kubernetes manifests validated

### Performance

#### Scalability
- Perimeter MTA: 3-20 pods (HPA)
- Internal Hub: 5+ pods (manual scaling)
- Queue processing: 1,050 workers
- Connections per pod: 50+ concurrent

#### Metrics
- Custom Prometheus metrics for HPA
- Queue depth tracking
- Active connections monitoring
- Policy evaluation statistics

### Security

#### Kubernetes Security
- Non-root containers (UID 1000)
- Read-only root filesystem
- Minimal capabilities (NET_BIND_SERVICE only)
- Network policies (zero-trust)
- Pod security contexts
- Secret management for TLS

#### Access Control Security
- CIDR-based network filtering
- RBL/DNSBL integration
- DNS validation
- SASL authentication support
- External policy service protocol

---

## [1.0.0] - 2026-03-08

### Added - Initial Release

#### Core Email Features
- Multi-tier queue system (5 tiers: emergency, msa, int, out, bulk)
- 1,050 total workers across all tiers
- Disaster recovery with WAL-based journal
- Message deduplication (SHA256 content hashing)
- Retry scheduler with exponential backoff
- Dead Letter Queue (DLQ) for failed messages
- Rate limiting per tier (token bucket)

#### Security Features
- DANE/TLSA validation (RFC 7672)
- SPF verification (RFC 7208) - enforced
- DKIM verification (RFC 6376) - active
- DMARC support (RFC 7489) - ready
- Greylisting (triplet-based, optional)
- DNS caching (5-minute TTL)
- Enhanced authentication with lockout protection
- Modern TLS (1.2/1.3, ECDHE, AES-GCM, ChaCha20)

#### Services
- SMTP Server (port 2525)
- IMAP Server (port 1143)
- REST API (port 8080)
- gRPC API placeholder (port 50051)
- Management CLI (`mailctl`)

#### Storage
- Persistent message storage
- Journal-based durability
- Replication support (framework)

---

## Upgrade Notes

### v2.0.0 Upgrade

#### Breaking Changes
- **None** - v2.0.0 is fully backward compatible with v1.0.0

#### New Configuration Options

**Kubernetes Integration** (optional):
```yaml
kubernetes:
  enabled: true
  service_discovery: true
  endpoint_watching: true
```

**Access Control** (optional):
```yaml
access_control:
  my_networks:
    - 10.0.0.0/8
    - 192.168.0.0/16
  my_domains:
    - example.com
  recipient_restrictions:
    - permit_mynetworks
    - reject_unauth_destination
```

**Global Routing** (optional):
```yaml
global_routing:
  enabled: true
  state_store:
    type: etcd
    endpoints:
      - etcd:2379
```

#### Deployment Options

**Standalone** (existing deployment):
```bash
./goemailservices --config config.yaml
```

**Kubernetes Perimeter MTA**:
```bash
kubectl apply -f deploy/kubernetes/base/
kubectl apply -f deploy/kubernetes/perimeter/
```

**Kubernetes Internal Hub**:
```bash
kubectl apply -f deploy/kubernetes/internal/
```

#### Migration Path
1. v1.0.0 continues to work as-is (no changes required)
2. Add K8s manifests to deploy in Kubernetes (optional)
3. Add access control configuration for Postfix-style filtering (optional)
4. Enable global routing for multi-region deployments (optional)

---

## Future Releases

### Planned for v2.1.0
- SQL backend drivers (MySQL, PostgreSQL, SQLite)
- LDAP/Active Directory integration
- External policy service protocol
- Content filtering (antivirus, anti-spam)
- DMARC aggregate reporting
- Web-based admin UI

### Planned for v3.0.0
- Machine learning-based routing
- Advanced threat detection
- Message archiving and compliance
- Multi-tenancy support
- Service mesh integration

---

## Links

- **Repository**: https://github.com/afterdarksys/go-emailservice-ads
- **Documentation**: See `/docs` and architecture markdown files
- **Issues**: https://github.com/afterdarksys/go-emailservice-ads/issues
- **Kubernetes Guide**: `deploy/kubernetes/README.md`

## Extensions, administration and HA follow-up

- Opt-in TLS gRPC management and web administration console.
- Durable signed management webhooks and versioned external admission plugins.
- Sender-domain and recipient-suffix transport selection.
- Replicated-volume active/passive ownership guard and DRBD quorum checker.
- Port 587 authenticated TLS submission defaults without shared bootstrap users;
  relay/listener validation and migration-safe configuration reload.
