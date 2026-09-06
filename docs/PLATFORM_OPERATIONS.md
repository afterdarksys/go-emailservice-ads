# Mail hub operations — 2.4.0

The supported topology is one persistent internal hub, optionally preceded by
one perimeter MTA. Each process owns its own spool. The internal hub provides
local mailboxes and authenticated submission; the perimeter validates recipients
against the hub and forwards through an explicit TLS connector.

## Choose a role

| Listener role | Purpose | Admission |
| --- | --- | --- |
| `internal` | Applications and perimeter-to-hub traffic | Explicit trusted CIDRs and STARTTLS |
| `submission` | User/application submission | STARTTLS plus SMTP authentication |
| `perimeter` | Internet MX | Local-domain recipients; opportunistic inbound STARTTLS |

`platform.listeners` creates multiple listeners sharing identities, policies,
queues, and limits. Without that list, the legacy `server.addr` listener remains
available and uses `server.require_auth` / `server.require_tls`.

```yaml
platform:
  listeners:
    - addr: ':2525'
      role: internal
      trusted_networks: ['192.0.2.10/32'] # replace with actual connector addresses
      tls: {cert: /etc/tls/tls.crt, key: /etc/tls/tls.key}
    - addr: ':587'
      role: submission
      tls: {cert: /etc/tls/tls.crt, key: /etc/tls/tls.key}
```

CIDR trust grants relay authority. Use precise connector networks. An IP
allowlist for reputation filtering does not grant relay or administrative rights.
PROXY protocol supports v1, is mandatory when enabled, and is accepted only from
its configured TCP peers. The physical peer controls connector trust; the
forwarded address controls IP checks. Trusted mail connectors may supply
`X-Mailhub-Original-IP`; untrusted copies are stripped. Authentication-Results is
replaced at the trust boundary, while incoming ARC headers are preserved for
Rspamd verification. See [RFC 8601](https://www.rfc-editor.org/rfc/rfc8601.html).

## Storage and upgrade

`platform.data_dir` defaults to `./data`; `MAILHUB_DATA_DIR` overrides it. Mount the
entire directory. It contains `mail-storage/`, `mailbox.db`, `users.db`, the
MTA-STS cache, TLS reports, and mailstorm circuit state. The queue acquires an
exclusive filesystem lock. Kubernetes deployments use one replica and Recreate
updates. Multiple writers and active-active mailbox service are unsupported.

Before changing the path, stop the old process, back up the whole data directory
and any separately configured identity database, then copy them to the mounted
destination with the service user's ownership. Keep SQLite WAL/SHM files with
their databases when present. Do not mix journals from different instances.
For rollback, restore the complete pre-upgrade backup while stopped; older
versions do not understand the new compaction checkpoints.

An empty `auth.user_database_url` now selects `data_dir/users.db`. Existing
default users are bootstrap-only and are not overwritten on restart. SMTP, IMAP,
and `/api/v1/mailboxes` use the same live identity store. `PUT` a mailbox with
`{"enabled":false}` disables authentication and local recipient acceptance.
Password updates preserve that state. Configure `platform.aliases` for alias
expansion; loops and unknown/disabled local targets are rejected.

Set `max_spool_bytes`, `max_spool_messages`, `min_free_bytes`, and
`mailbox_quota_bytes` under `platform`. Admission defers when capacity is exhausted.
Emergency DSNs bypass ordinary spool quotas but still respect free-space reserve.
Mailbox expunge tombstones stored payloads; hourly compaction reclaims old journal
data. Held messages expire after `quarantine_retention_days`; zero retains them.
Allow additional disk space for compaction snapshots and audit-log retention.

SMTP acceptance follows durable queue storage. Local delivery is idempotent by
queue ID and recipient. Remote delivery remains at-least-once: a crash after a
remote server's DATA acknowledgement but before the local checkpoint can duplicate
mail. This is an SMTP ambiguity, not an exactly-once delivery guarantee.

## Routing and recipient directory

```yaml
platform:
  validate_recipients: true
  aliases:
    operations@example.com: [alice@example.com, bob@example.com]
  transports:
    - domain: example.com
      next_hops:
        - address: smtp-internal:2525
          server_name: smtp-internal
          require_tls: true
          ca_file: /etc/tls/internal-ca.pem # omit for system-trusted certificates
        - address: backup-hub.example.com:2525
          require_tls: true
```

Next hops are tried in order on transient failure. An explicit transport never
falls back to public MX delivery. `domain: '*'` is an outbound default; local
mailboxes still receive locally unless their domain has an explicit route.
Optional `username` and `password_env` require verified TLS. `max_hops` bounds
Received-header loops.

On a perimeter instance, configure `recipient_directory_url` to the hub's HTTPS
`/api/v1/recipients` endpoint and `recipient_directory_token_env` to the environment
variable holding a `recipients:read` key. Unknown recipients get 550; a directory
outage gets 451. Install the private CA in the system trust store when needed.

## Layered filtering

See [IP filtering](IP_FILTERING.md) for deny/allow CIDRs, DNSBL zones, lookup
deadlines, and provider failure handling. Choose DNSBL providers and licensed
resolvers appropriate to your deployment; no third-party list is enabled by default.

For the existing premail database, set `reputation_database_env` to an environment
variable containing a PostgreSQL DSN with read access to `ip_characteristics`.
Premail's high-is-bad score is inverted to the policy engine's high-is-good score.
Records older than seven days are unknown; active blocklist entries score zero.
Alternatively, `reputation_url` accepts GET `/{ip}` returning
`{"score":80,"known":true,"source":"provider","expires_at":"...RFC3339..."}`.
Its `/health` must return 200 when `reputation_required` is true. Unknown reputation
is explicitly marked unknown, rather than presented as a measured score.
`reputation_reject_below` rejects known scores below its threshold; zero disables
that direct threshold. Policies can also consume the score.

```yaml
platform:
  policy_required: true
  policy_path: /etc/smtp/policies.yaml
  scanner_url: http://rspamd:11333
  scanner_required: true
  scanner_timeout: 15s
  clamav_address: clamav:3310
  malware_required: true
  reputation_database_env: PREMAIL_READONLY_DSN
  reputation_required: true
  reputation_reject_below: 20
```

Required policy initialization fails startup; runtime evaluation errors defer
mail. Policy reload validates all enabled scripts before replacing the active set.
An empty policy list is valid and supplies no custom rules. The bootstrap policy file seeds `data_dir/policies.yaml` once; this writable file
then becomes authoritative. GET/POST `/api/v1/policies`, GET/PUT/DELETE
`/api/v1/policies/{name}`, and POST `/api/v1/policies/{name}/test` manage and test
policies. Changes persist atomically before activation; inline scripts are
required for API edits. POST `/api/v1/policies/reload` reloads the managed file.
Use policies:read/write API scopes; test evaluation never queues mail.

The direct ClamAV INSTREAM check requires an explicit clean result. Malware gets
550; required scanner outages, incomplete scans, and size-limit errors get 451.
Rspamd supplies spam/content decisions and signature integration. A required
Rspamd outage also gets 451. Its `greylist`/`soft reject` actions defer; quarantine
and discard actions are held for review. Header removals and insertions follow the
[Rspamd protocol](https://docs.rspamd.com/developers/protocol/). A returned complete
message is used without reapplying its header changes.

The direct malware check is intentional: a live outage test found that the tested
Rspamd image can time out its ClamAV rule without returning a failure symbol.
Do not rely solely on a force_actions rule to require completed malware scans.

`platform.arc: true` delegates ARC to Rspamd. Configure its signing networks,
private keys, selectors, and DNS records using `deploy/rspamd/arc.conf.example` and
the [Rspamd ARC documentation](https://docs.rspamd.com/modules/arc/). Configure DKIM
signing there too; simultaneous local DKIM signing is rejected by configuration
validation. The previous homemade ARC implementation is not used. ARC response
handling has regression coverage; end-to-end cryptographic interoperability with
your published keys must be checked in staging before enabling ARC in production.

## Mailstorm prevention and control

The sample configuration enables `platform.mailstorm`. Configure it per instance
for the traffic it receives. The limits apply to authenticated users or original
source IPs, so changing envelope sender addresses does not evade the identity
limit. NAT-shared applications should use separate SMTP accounts when independent
limits are needed.

| Setting | Sample value | Behavior |
| --- | --- | --- |
| `window` | `1m` | Baseline and duplicate observation window |
| `messages_per_window` | 120 | Per-identity token-bucket burst/refill |
| `recipients_per_window` | 1000 | Fan-out budget per identity |
| `global_messages_per_window` | 2000 | Instance-wide admission budget |
| `duplicate_limit` | 20 | Repeated subject/body/envelope fingerprint threshold |
| `trip_after` | 3 | Burst violations before opening a circuit |
| `cooldown` / `max_cooldown` | `5m` / `1h` | Exponential cooldown for recurring storms |
| `baseline_windows` | 5 | Observation windows before anomaly detection |
| `anomaly_factor` / `anomaly_floor` | 4 / 20 | Spike threshold relative to learned volume |
| `max_identities` | 10000 | Bound on tracked identities and circuits |

Duplicate fingerprints ignore transport headers such as Date and Message-ID, and
retain hashes rather than message bodies. Baselines use an exponentially weighted
moving average. This is deterministic adaptive control, not a trained ML model.
Burst counters and learned baselines reset on restart; active circuit breakers
persist. Limits are per instance, not a distributed cluster-wide quota.

New storm traffic receives SMTP 451 before queue acceptance. Queued messages from
paused identities remain pending without spending retries. Already-running remote
SMTP transactions can finish. Queue capacity limits provide further backpressure.
Expiry permits traffic again; recurring storms extend the pause. A `*` operator
pause stops all new admissions and queued dispatch on that instance.

Use a Bearer key with `mailstorm:read` to inspect `GET /api/v1/mailstorm` and
`mailstorm:write` for the following requests:

```http
POST /api/v1/mailstorm/pause
Content-Type: application/json

{"key":"user:billing-app","reason":"Runaway job","duration":"30m"}
```

Use `ip:192.0.2.10` for an unauthenticated application or `*` for an instance-wide
stop. `POST /api/v1/mailstorm/resume` with `{"key":"user:billing-app"}` clears that
circuit and its in-memory sender counters. Pausing an already-paused identity extends
its expiry; use resume to shorten a pause. Legacy queued messages use their stored
IP identity, or the key `unknown` when no original identity exists. Operator intent
is durably audited.
Existing `user_recipients_per_hour`, `domain_recipients_per_hour`, per-IP message,
and connection limits remain available alongside adaptive admission.

## Administration, quarantine, and readiness

Mail user credentials no longer grant administrative API access. API keys require
explicit `resource:read` / `resource:write` scopes or `*`; empty permissions deny.
Use `key_env` for secrets. HTTPS is available through `api.tls`. Restrict network
access and source IPs separately from key scopes.

`GET /api/v1/quarantine` lists held message metadata. `POST
/api/v1/quarantine/{id}/release` rescans before returning a message to the pending
queue; only a clean verdict releases it. `/delete` removes it. These require
`quarantine:read` / `quarantine:write`. Operator requests are recorded in
`mail-storage/audit.jsonl`; release actor/time are also journaled. Ship and retain
audit logs externally according to your retention policy.

`/health` is liveness. `/ready` checks storage capacity and required runtime
dependencies. A required dependency outage makes readiness fail and SMTP defer.
Monitor pending/failed/held queues, storage, mailstorm deferred totals and active
circuits, scanner availability, and TLS report delivery errors.

`mta_sts` persists enforced policies to survive restarts and temporary discovery
failures. `tls_reporting` persists per-domain/day reports and submits completed
days via published HTTPS or mailto destinations; failed delivery retains reports.
No-policy destinations are retried and need retention monitoring. Configure
external DNS, certificate trust, and signing for your actual domains.

## Deployment and verification

The Kubernetes role examples are under `deploy/kubernetes/internal` and
`deploy/kubernetes/perimeter`; shared ConfigMaps and private scanner dependencies
are in `deploy/kubernetes/base`. Apply namespace/service-account prerequisites,
create `smtp-admin` (admin-key, directory-key) and `smtp-tls` secrets, edit domains,
CIDRs and certificate names, then apply ConfigMaps, filters, network policies, and
role deployments/services. The hub certificate must match the connector hostname
and HTTPS recipient-directory hostname. Do not deploy the legacy generic
deployment at the same time as the role deployments.

`deploy/docker-compose.filters.yml` supplies private Rspamd, Redis, and ClamAV
services for a containerized hub. Join the hub to its `mail-filters` network.
Keep scanner ports private; reserve memory for ClamAV and wait for signature
database initialization. Scanner dependency images are pinned to the digests used in local validation.

Validation includes real SMTP authentication/recipient rejection/local mailbox
delivery, explicit TLS connector failover, durable queue recovery, quota and
compaction tests, scanner protocol cases, and deterministic mailstorm tests.
The live scanner test can be repeated with `MAILHUB_CLAMAV_TEST_ADDR=host:port go
test ./internal/filtering -run TestLiveClamAV`. It sends a harmless EICAR fixture.
This release is locally validated, not deployed to your live mail environment.
