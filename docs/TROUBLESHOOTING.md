# Troubleshooting — 2.8.0

Start by recording binary version, deployment/config revision, timestamp,
listener role, queue `message_id`, affected envelope recipient, exact SMTP/HTTP
status and recent changes. Redact credentials and message content. Queue IDs and
recipient outcomes are more useful than subject alone. Preserve the original
spool and audit records before attempting recovery.

## First checks

With API_BASE set to the HTTPS origin and MAILHUB_API_KEY supplied securely:

```sh
curl --fail-with-body --cacert /etc/mailhub/ca.pem "$API_BASE/health"
curl --fail-with-body --cacert /etc/mailhub/ca.pem "$API_BASE/ready"
curl --fail-with-body --cacert /etc/mailhub/ca.pem   -H "Authorization: Bearer $MAILHUB_API_KEY" "$API_BASE/api/v1/queue/stats"
```

Use the system trust store instead of `--cacert` for publicly trusted certificates.
A live process is not proof of dependency readiness or successful mail delivery.
Inspect [metrics and the probe](MONITORING.md), then follow the matching symptom.

| Symptom | Inspect | Resolution / verification |
| --- | --- | --- |
| Startup fails with spool/lease ownership error | Existing process, volume mount, shared lease path | Confirm the old owner is stopped or fenced; keep one writer. Do not remove locks to bypass ownership. |
| Startup cannot load TLS/policy/config | File paths, service UID permissions, secret mounts, enabled required dependencies | Correct the underlying file/config; test isolated startup and a new TLS connection. |
| `/health` 200 but `/ready` 503 | `checks.storage`, `checks.queue`, disk space/quotas, required scanner/reputation dependencies | Restore storage/dependency health; confirm readiness and synthetic delivery. |
| SMTP 451 before acceptance | Scanner timeout/outage, directory outage, spool quota, mailstorm pause, policy error | Read logs at that timestamp and `/mailstorm`; fix dependency/capacity or sender behavior. Sending peer should retry. |
| SMTP 550 recipient rejection | Local domains, account enabled state, aliases, suppression list, perimeter directory | GET recipient lookup; fix intended mapping or account. Do not accept unknown recipients just to defer the problem. |
| Relay denied | Listener role, SMTP authentication, actual connector peer and trusted CIDRs | Authenticate on submission or configure the precise intended connector trust. |
| AUTH unavailable/fails | STARTTLS, supported PLAIN mechanism, persistent user state, password and lockout logs | Verify TLS first, then enabled account credentials; bootstrap YAML does not overwrite a changed password. |
| Pending queue grows | Stored error/attempts, per-recipient outcomes, DNS/MX, outbound TCP reachability, remote SMTP status, destination backoff | Resolve temporary cause and observe queued dispatch; retries may be intentionally delayed. |
| DLQ message remains failed | Original failure and current recipient/provider state | After correction, POST the specific DLQ retry; monitor the resulting transaction. |
| TLS/DANE/MTA-STS delivery failure | Certificate name/chain/expiry, TLSA/DNSSEC result, resolver health, enforced cached policy | Correct DNS/certificates/connector CA. Indeterminate validation can defer delivery; repeated retries do not repair trust. |
| IMAP connection fails | `tls_mode`, port, TLS trust, disabled server and username | Use STARTTLS on the configured STARTTLS listener or implicit TLS on its own port. |
| IMAP folder mutation rejected | Destination existence, quota, mailbox name and selected read-only state | Create the COPY/APPEND destination first; delete child folders before their parent. Check storage errors and available quota. |
| Quarantine release 409 | Rescan verdict, required scanner health, current held state | Resolve scanner/cause and review again; a release request is not proof of release. |
| Compliance list empty / evidence 404 | Principal name, domain grants, action grants and requested domain | Authorize the intended principal/domain/action. Hidden evidence can appear absent. |
| Compliance action 409 | Legal hold, finite retention expiry, evidence integrity, case mode and reason | Resolve the specific workflow condition; generic queue deletion cannot override it. |
| REST 401 / 403 | Bearer syntax, expiry/key file, exact scope, TCP peer allowlist, OAuth TLS and introspection | See authentication details below; scope wildcards like `queue:*` are not supported. |
| REST 404 for listener/tenant/security route | Actual route table | Older router/CLI endpoints are not part of the active API; use API_REFERENCE.md. |
| REST replication 501 | Standard executable wiring | Expected: use qualified offline restore and fenced standby activation. |
| Disk does not shrink immediately after deletion | Compaction cycle, held evidence, audit files, backups, stored mail | Allow/monitor compaction headroom; retained backups and evidence have separate disposal workflows. |

API paths abbreviated in the table are under `/api/v1`. Read-only queue responses
can include base64 message bodies; avoid collecting them unless necessary.

## Authentication and TLS diagnosis

API Basic Auth and mailbox passwords do not grant management access. A static key
needs an exact `resource:read` or `resource:write` permission (or global `*`).
Policy test/reload and queue retry are writes. Compliance export/release/delete/
legal-hold each have separate scopes and may also require domain grants.

A recognized literal key with insufficient permission can return 403; file-backed
or OAuth failures may return 401. Do not assume every permission failure uses one
status. Check the response text. `allowed_ips` accepts exact peer addresses;
X-Forwarded-For does not grant access. If a proxy is used, evaluate the actual
TCP peer and keep the API TLS requirement for OAuth.

To inspect SMTP STARTTLS without sending mail:

```sh
openssl s_client -starttls smtp -connect mail.example.test:2525   -servername mail.example.test -verify_hostname mail.example.test   -verify_return_error -CAfile /etc/mailhub/ca.pem
```

For IMAP STARTTLS replace `smtp` with `imap` and use the IMAP port. For implicit
TLS omit `-starttls`. Renew certificate/key as an atomic pair, then open a fresh
connection; existing sessions retain established TLS state.

## Delivery, duplicates and missing mail

First distinguish SMTP rejection before acceptance from failure after a 250
acceptance. Rejected/deferred transactions may have no durable queue ID. For
accepted mail, inspect the exact ID, remaining recipients, status and stored
error. Successful and permanently failed recipients are removed from retry work;
only temporary failures remain. DSN preferences or configured suppression can
explain absent notifications; incoming DSNs are untrusted reports.

Local delivery is idempotent by transaction and recipient. Remote SMTP is at least
once: a crash after remote DATA acknowledgment but before checkpoint can duplicate
mail. Repeated manual retries can also duplicate delivery. Preserve evidence of
the remote response before deciding to resubmit.

For missing local mail, verify recipient alias resolution and persistent mailbox
identity, then check Junk/quarantine and compliance holds. A compliance hold
suspends the whole envelope; a released case keeps evidence and creates a new
transaction. IMAP mailbox entries are distinct from outbound pending entries.

## Storage recovery and escalation

Stop the owner before backup/restore. Use `mailhub-backup verify` on the archive
and restore into a new destination. An incomplete final journal record is handled
by recovery; other corruption can fail startup. Do not hand-edit journals to
force startup. Preserve originals, logs and the failing archive for analysis and
recover from a verified snapshot using [BACKUP_RECOVERY.md](BACKUP_RECOVERY.md).

An audit-chain error needs the original JSONL plus an independent checkpoint;
a local replacement checkpoint does not prove continuity. For retention-protected
exports, retain the exact Object Lock version receipt. Follow
[deployment qualification](DEPLOYMENT_QUALIFICATION.md) for preservation checks.

After any incident fix, verify readiness, an authenticated local SMTP/IMAP round
trip, the relevant external connector, queue age and alerts. Record which checks
used production dependencies versus isolated fixtures.
