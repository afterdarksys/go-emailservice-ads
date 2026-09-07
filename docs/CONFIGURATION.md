# Configuration — 2.7.0

Configuration is loaded from the file passed to `goemailservices --config`.
Omitted fields receive defaults from `internal/config/config.go` and subsystem
validation. There is no generic environment-variable interpolation. In
particular, `${VAR}` inside a literal password/key is not a secret reference.
Unknown YAML keys at any configuration level and additional YAML documents are
rejected. Existing files containing ignored legacy settings must be corrected
before upgrade. Run `goemailservices --check-config --config /etc/mailhub/config.yaml`
to validate decoding, defaults, environment references and configuration rules.
It does not test certificate files, database access, DNS, scanner health or port
availability; follow with isolated startup and deployment qualification.

## Precedence and change application

| Setting | Source / update behavior |
| --- | --- |
| Data root | `MAILHUB_DATA_DIR` overrides `platform.data_dir`; default `./data` |
| Identity store | Empty `auth.user_database_url` becomes `data_dir/users.db`; a PostgreSQL URL selects external identities |
| API key | `key_env` resolves a named environment variable at startup; `key_files` are reread per request |
| Other named secrets | `recipient_directory_token_env`, `reputation_database_env`, transport `password_env`, OAuth `client_secret_env` refer to environment variables |
| Mail users | `auth.default_users` seeds missing users; persistent/API changes survive restart and are not overwritten by bootstrap values |
| Policies | `platform.policy_path` seeds `data_dir/policies.yaml`; API changes or policy reload update the managed policy file/set |
| TLS certificate/key content | Reloaded on new handshakes at configured paths; use atomic paired replacements |
| General YAML settings | Controlled restart required, including aliases, transports, bounce/compliance rules, key scope entries and OAuth configuration |

Use [credential rotation](CREDENTIAL_ROTATION.md) for key/certificate overlap.
Policies reload separately; POST `/api/v1/policies/reload` does not reload the
server YAML. The management API has no generic configuration write endpoint.

## Starting configuration

This example defines an authenticated submission hub. Replace the example domain,
TLS paths and operator IPs, provision the key file, and create mail users through
the API before testing delivery. Ports above 1024 permit an unprivileged process.
Merge optional sections into the same YAML mapping; do not duplicate top-level
`platform`, `server` or `api` keys.

```yaml
server:
  domain: mail.example.test
  local_domains: [example.test]
  max_message_bytes: 10485760
  max_recipients: 50
  max_connections: 1000
  max_per_ip: 10
  rate_limit_per_ip: 100
  allow_insecure_auth: false
  auth_mechanisms: [PLAIN]
  timeouts: {command: 300s}
  spf: {enabled: true, mode: monitor}
  dmarc: {enabled: true, mode: monitor, quarantine_folder: Junk}
  tls: &mail_tls
    cert: /etc/mailhub/tls/tls.crt
    key: /etc/mailhub/tls/tls.key
imap:
  addr: ':1143'
  tls_mode: starttls
  tls: *mail_tls
jmap:
  enabled: false
auth:
  default_users: []
api:
  rest_addr: ':8080'
  tls: *mail_tls
  require_ip_auth: true
  allowed_ips: ['127.0.0.1', '::1']
  api_keys:
    - name: operations
      key_files: [/run/secrets/mailhub-operations]
      permissions: [queue:read, queue:write, mailboxes:read, mailboxes:write,
                    recipients:read, policies:read, policies:write,
                    quarantine:read, quarantine:write, mailstorm:read,
                    mailstorm:write, bounce:read]
platform:
  data_dir: /var/lib/mailhub
  listeners:
    - addr: ':2525'
      role: submission
      tls: *mail_tls
  validate_recipients: true
  max_spool_bytes: 10737418240
  max_spool_messages: 100000
  min_free_bytes: 1073741824
  mailbox_quota_bytes: 1073741824
  max_hops: 30
  quarantine_retention_days: 30
logging:
  level: info
  format: json
aftersmtp:
  enabled: false
```

This baseline has no required content scanners or custom policies. Add the
filtering settings below for the deployment's admission policy. Quotas in this
example are capacity choices, not measured sizing recommendations.

## Listeners, relay and TLS

`platform.listeners` replaces the single `server.addr` SMTP listener when
nonempty. `submission` requires authentication and STARTTLS; `internal` requires
TLS and explicit `trusted_networks` CIDRs; `perimeter` accepts inbound local-domain
mail with opportunistic STARTTLS. Configure Internet MX ingress separately from
submission. See [role and connector examples](PLATFORM_OPERATIONS.md).

For the legacy single listener use `server.require_auth`, `server.require_tls`
and `server.relay.allowed_networks`. Relay CIDRs grant permission to send to
nonlocal destinations. Reputation allowlists do not grant relay rights.
`server.auth_mechanisms` currently supports PLAIN only, protected by TLS.

IMAP `tls_mode` is `starttls` (default), `implicit`, or `disabled`; do not infer
TLS mode from the port or the legacy `require_tls` field. Typical assignments are
1143/143 for STARTTLS and 993 for implicit TLS. Set `imap.disabled: true` on a
perimeter without mailboxes. Optional JMAP is a separate HTTP listener and needs
HTTPS termination; it does not inherit the REST API's TLS or bearer-key contract.

API `allowed_ips` matches exact TCP peer IPs, not CIDRs. Forwarded HTTP headers
do not change this identity. The allowlist applies to authenticated routes;
health/version/metrics bypass that middleware. Restrict the management listener
at the network layer too. OAuth requires TLS on the API itself.

PROXY protocol v1 is a separate SMTP setting. When enabled it is mandatory and
requires `trusted_networks`; do not enable it for direct SMTP clients. mTLS uses
`require_client_cert` and `client_ca_file` in the listener TLS configuration.

## Delivery, identity and resource settings

| Configuration | Purpose |
| --- | --- |
| `server.local_domains` | Domains eligible for local recipient handling |
| `platform.aliases` | Address-to-target-list mapping, with disabled/unknown target rejection |
| `platform.transports` | Explicit per-domain next hops; `'*'` supplies default remote routing |
| `platform.recipient_directory_url` / `recipient_directory_token_env` | Perimeter lookup against hub HTTPS `/api/v1/recipients`, using `recipients:read` |
| `platform.destination_throttle` | Bound per-destination concurrency and transient-failure backoff |
| `platform.user_recipients_per_hour` / `domain_recipients_per_hour` | Outbound recipient budgets |
| `platform.mailstorm` | Per-identity/global burst controls, duplicates, adaptive baselines and persistent pauses |
| `platform.max_spool_bytes` / `max_spool_messages` | Admission capacity; zero disables those bounds |
| `platform.min_free_bytes` | Free-space reserve; leave headroom for compaction and backups |
| `platform.mailbox_quota_bytes` | Mailbox byte limit; zero disables this bound |
| `platform.quarantine_retention_days` | Held-message expiration; zero retains indefinitely; compliance evidence is separate |
| `platform.fencing_lease_file` | Cross-host ownership lease only on storage with reliable shared POSIX locks |

`destination_throttle` accepts `concurrency` (default 4), `initial_backoff`
(default `30s`), `max_backoff` (default `1h`) and `max_destinations` (default
10000). See [mailstorm settings](PLATFORM_OPERATIONS.md#mailstorm-prevention-and-control)
for its full setting table and counter persistence behavior.

An explicit transport never falls back to public MX delivery. Next-hop credentials
require verified TLS. Specify `server_name` and a trusted CA when connecting to a
private hostname. Full [transport examples](PLATFORM_OPERATIONS.md) include ordered
fallback and connector trust. All data-directory files belong on the persistent
mount; separately configured databases and secrets need separate backups.

## Filtering and mail authentication

```yaml
platform:
  policy_required: true
  policy_path: /etc/mailhub/policies.yaml
  scanner_url: http://rspamd:11333
  scanner_required: true
  scanner_timeout: 15s
  clamav_address: clamav:3310
  malware_required: true
```

Supply a valid bootstrap policy file and reachable private scanner services before
starting. Required initialization failures stop startup; runtime required-provider
failures defer SMTP and can fail readiness. Direct ClamAV requires an explicit
clean result independently of Rspamd. Configure scanner time and message-size
limits together. See [platform filtering](PLATFORM_OPERATIONS.md) and
[IP/DNSBL filtering](IP_FILTERING.md).

SPF and DMARC default to enabled `monitor` mode. Set `server.dmarc.mode: enforce`
to apply published reject/quarantine policies after validating traffic. SPF
standalone enforcement is a separate `server.spf.mode` decision; forwarding can
fail SPF. Greylisting is optional via `server.enable_greylist` and causes retries.

Local outbound signing uses one `server.dkim` entry per envelope sending domain:

```yaml
server:
  dkim:
    - enabled: true
      domain: example.test
      selector: mail
      private_key_path: /etc/mailhub/dkim/example.test.pem
```

Provision an RSA or Ed25519 PEM key and publish its matching selector public key
in DNS. No key or DNS record is generated by enabling the flag. If `platform.arc`
is enabled, use required Rspamd and configure DKIM/ARC signing there; simultaneous
enabled local DKIM signing is rejected. Test signatures in the target environment.
`platform.mta_sts` and `tls_reporting` enable their persistent delivery controls;
DANE behavior also depends on the configured validating resolver and live DNS.

## Bounce, compliance, OAuth and logs

Use [examples/compliance-config.yaml](../examples/compliance-config.yaml) together
with [compliance operations](COMPLIANCE_OPERATIONS.md). Bounce duration values
accept YAML units; JSON effective settings expose nanoseconds. Zero delay-warning
age disables warning notices. Tracked incoming DSNs are untrusted and never
implicitly suppress recipients; configure suppression explicitly.

Compliance requires both API action scopes and configured principal/domain/action
grants when `enforce_domain_access` is enabled. API key principals use the key
entry's `name`; OAuth principals use `oauth:<subject>`. `require_for_compliance`
requires OAuth rather than permitting a static-key fallback. Run the
[provider qualification command](DEPLOYMENT_QUALIFICATION.md) before rollout.

`logging.format` supports `json`, `yaml`, `syslog`, `console`; choose retention in
the collector. Audit JSONL lives under `mail-storage/audit.jsonl` independently of
operational output formatting. Keep header logging opt-in. Configure Elasticsearch
only when its destination and credentials have been validated; enabling event
logging does not provide immutable evidence retention.

## Unsupported configuration assumptions

`api.grpc_addr` does not start a management gRPC service. Replication peer settings
are not wired into the standard executable. General `directory.base_url`, dynamic
listener/map/tenant REST administration, and SMTP OAuth cannot be inferred from
older guides or similarly named source packages. Use the [API reference](API_REFERENCE.md)
and [backlog](../TODO) for supported operations and explicit remaining work.

### JMAP creation, upload and submission capacity

JMAP upload limits are currently fixed: 10 MiB per file, four combined uploads/API
requests, and 20 temporary blobs or 100 MiB per account. Uploads expire after
24 hours; capacity pressure evicts the oldest first. This temporary SQLite storage
is separate from `platform.mailbox_quota_bytes`, which applies when Email/import
creates mail. Include temporary blob space in disk/backup sizing. See
[JMAP API](JMAP_API.md#upload-and-import) for workflow and troubleshooting.

Structured Email/set creation also enforces a 10 MiB encoded-email and aggregate
prepared-MIME limit per method. The main executable enables immediate JMAP
submission through the first submission-role SMTP backend, or the first SMTP
backend when no such role is configured. Existing server message/recipient limits,
sender quotas, policies, scanners and queue configuration apply. Keep JMAP behind
trusted TLS termination; per-IP message limits see the proxy address. Submission
receipts are durable journal metadata with no automatic expiry and do not consume
pending-message quota. Plan disk/backup capacity for their growth. See
[submission semantics and troubleshooting](JMAP_API.md#submission-and-acceptance-receipts).
