# Management REST API — 2.8.0

This reference describes the `api.NewServer` mounted by `cmd/goemailservices`,
verified against `internal/api/server.go`, its handlers and permissions middleware
on 2026-09-06. The separate `AdminAPI` router is not mounted by this executable.
JMAP and optional AfterSMTP protocols have separate contracts.

## Connection and authorization

The origin is the configured `api.rest_addr`; `api.tls` enables HTTPS. Requests
below use `API_BASE=https://mail.example.test:8080`, a trusted CA and a securely
supplied `MAILHUB_API_KEY`. Paths beginning `/api/v1` require Bearer authentication
except `/api/v1/health` and `/api/v1/version`.

```sh
curl --fail-with-body --cacert /etc/mailhub/ca.pem   -H "Authorization: Bearer $MAILHUB_API_KEY"   "$API_BASE/api/v1/queue/stats"
```

See [authentication](../API_AUTHENTICATION.md) for configuration and OAuth.
Static key permissions are exact strings or global `*`. Neither `all`, bare
`read`/`write`, nor `queue:*` grants access. Account passwords/Basic Auth are not
administrative authentication. The optional source-IP restriction checks the
TCP peer against exact IPs. Public health/version/metrics handlers bypass it.

## Endpoint inventory

Use the methods below even where legacy handlers do not explicitly reject other
methods. Write operations have no general idempotency-key facility. Do not blindly
retry a timed-out release or other mutation; inspect state first.

| Method | Path | Permission | Result / behavior |
| --- | --- | --- | --- |
| GET | `/health`, `/api/v1/health` | Public | 200 `{status:"ok",uptime:string}` |
| GET | `/ready` | Public | 200 ready or 503 not_ready, with `checks.storage` and `checks.queue` booleans |
| GET | `/api/v1/version` | Public | 200 `{service:"go-emailservice-ads",version:string}` |
| GET | `/metrics` | Public | Prometheus text |
| GET | `/api/v1/queue/stats` | `queue:read` | 200 `{metrics,storage}` |
| GET | `/api/v1/queue/pending?tier=out` | `queue:read` | 200 journal-entry array; omit tier for all pending tiers |
| GET | `/api/v1/dlq/list` | `queue:read` | 200 failed-entry array |
| POST | `/api/v1/dlq/retry/{id}` | `queue:write` | 200 `{status:"ok",message_id}`; no body |
| GET | `/api/v1/message/{id}` | `queue:read` | 200 journal entry; 404 absent; compliance entries denied with 403 |
| DELETE | `/api/v1/message/{id}` | `queue:write` | 200 `{status:"deleted"}`; compliance entries denied |
| GET | `/api/v1/mailboxes` | `mailboxes:read` | 200 account array |
| POST | `/api/v1/mailboxes` | `mailboxes:write` | 201 account; 409 duplicate username |
| GET | `/api/v1/mailboxes/{username}` | `mailboxes:read` | 200 account; 404 absent |
| PUT | `/api/v1/mailboxes/{username}` | `mailboxes:write` | 200 updated account |
| DELETE | `/api/v1/mailboxes/{username}` | `mailboxes:write` | 200 `{status:"deleted",username}`; 404 absent |
| GET | `/api/v1/recipients/{address}` | `recipients:read` | 200 `{recipients:[...]}`; 404 unknown; 503 directory failure |
| GET | `/api/v1/policies` | `policies:read` | 200 `{policies:[...],count}` |
| POST | `/api/v1/policies` | `policies:write` | 201 `{status:"saved",name}` |
| GET | `/api/v1/policies/{name}` | `policies:read` | 200 policy object; 404 absent |
| PUT | `/api/v1/policies/{name}` | `policies:write` | 200 `{status:"saved",name}`; URL supplies name |
| DELETE | `/api/v1/policies/{name}` | `policies:write` | 204, empty body |
| POST | `/api/v1/policies/{name}/test` | `policies:write` | 200 `{policy,action}`; 422 evaluation failure |
| GET | `/api/v1/policies/stats` | `policies:read` | 200 manager statistics |
| POST | `/api/v1/policies/reload` | `policies:write` | 200 `{status:"reloaded",message}`; no body |
| GET | `/api/v1/mailstorm` | `mailstorm:read` | 200 guard status |
| POST | `/api/v1/mailstorm/pause` | `mailstorm:write` | 200 updated guard status |
| POST | `/api/v1/mailstorm/resume` | `mailstorm:write` | 200 updated guard status |
| GET | `/api/v1/quarantine` | `quarantine:read` | 200 held metadata array |
| POST | `/api/v1/quarantine/{id}/release` | `quarantine:write` | 200 `{status:"release",id}` after clean rescan; no body |
| POST | `/api/v1/quarantine/{id}/delete` | `quarantine:write` | 200 `{status:"delete",id}`; no body |
| GET | `/api/v1/bounce/config` | `bounce:read` | 200 effective bounce settings |
| GET | `/api/v1/bounce/reports` | `bounce:read` | 200 tracked incoming DSN entries |
| GET | `/api/v1/compliance/config` | `compliance:read` + domain read grant | 200 visible rule array |
| GET | `/api/v1/compliance?domain=example.test` | `compliance:read` + domain read grant | 200 visible case entries, message data omitted |
| GET | `/api/v1/compliance/{id}/export` | `compliance:export` + domain export grant | 200 RFC822 bytes after integrity/audit checks |
| POST | `/api/v1/compliance/{id}/legal-hold` | `compliance:legal-hold` + domain legal-hold grant | 200 `{id,action}` |
| POST | `/api/v1/compliance/{id}/release` | `compliance:release` + domain release grant | 200 `{id,action}`; creates delivery, retains evidence |
| POST | `/api/v1/compliance/{id}/delete` | `compliance:delete` + domain delete grant | 200 `{id,action}`; retention/hold enforced |
| GET | `/api/v1/replication/status` | `replication:read` | 501 in standard executable: replication not configured |
| POST | `/api/v1/replication/promote` | `replication:write` | 501 in standard executable; use fenced standby tooling |

Domain grants apply when `platform.compliance.enforce_domain_access` is enabled.
Use opaque IDs returned by each endpoint; the journal `message_id` is the message
operation ID, not the journal-record `id`. URL-encode usernames, addresses, policy
names and query values. Collections have no documented pagination or sorting
contract; clients should tolerate empty arrays or null from legacy collections.

## Request and response formats

Send JSON mutations with `Content-Type: application/json`. Success responses are
JSON except metrics, RFC822 export and empty 204 responses. Errors usually use
plain-text `http.Error`, not a shared JSON error envelope. Read status and
Content-Type before decoding.

| Status | Interpretation |
| --- | --- |
| 400 | Invalid JSON/fields, missing parameters or invalid values |
| 401 | Missing/invalid Bearer credentials, or failed required OAuth/TLS |
| 403 | IP denied, recognized key lacks scope, or generic queue access to evidence |
| 404 | Missing resource or domain/action-hidden compliance evidence |
| 405 | Unsupported method |
| 409 | Policy conflict/validation, invalid pause, release/retention/hold/rescan conflict |
| 422 | Policy test evaluation failure |
| 500 | Persistence/reload/retry failure; capture response and inspect logs |
| 501 | Feature unavailable in the running server |
| 503 | Readiness, required service, audit or storage unavailable |

Credential failures are not uniformly classified: a file-backed key lacking
scope or an OAuth validation failure may return 401 rather than 403. Treat both
as requiring credential/authorization review, not automatic retry.

### Mailboxes

POST accepts `username`, `password`, optional `email` (defaults to username).
Identifiers must be nonempty, at most 255 bytes, without whitespace/control
characters; email must contain `@`. Passwords must be 12–72 UTF-8 bytes, matching bcrypt’s input limit.
Oversized create/update requests return 400 before changing account state. Bodies are limited to 64 KiB and unknown fields are rejected.

```json
{"username":"alice@example.test","email":"alice@example.test","password":"REPLACE_WITH_UNIQUE_SECRET"}
```

A successful create returns:

```json
{"username":"alice@example.test","email":"alice@example.test","enabled":true}
```

PUT accepts `password`, `email`, and/or boolean `enabled`; at least one change
must be supplied. Password/hash values are never returned. Disable example:

```sh
curl --fail-with-body --cacert /etc/mailhub/ca.pem   -H "Authorization: Bearer $MAILHUB_API_KEY" -H 'Content-Type: application/json'   -X PUT --data '{"enabled":false}'   "$API_BASE/api/v1/mailboxes/alice%40example.test"
```

DELETE removes the user identity, not all retained mail/evidence.

### Queue data

Journal objects expose `id`, `message_id`, `from`, `to`, `data`, `tier`, `attempts`,
`last_attempt`, `created_at`, `status`, optional `error_message` and `metadata`.
Go byte-slice `data` is base64 in JSON; it can contain full mail. Timestamps use
RFC3339-compatible JSON. Queue metrics contain `enqueued`, `processed`, `failed`,
`duplicates`, `last_update`; counters are not a durable delivery receipt.

Quarantine listing uses `{id,from,to,created_at,bytes}` and omits content.
Compliance listing redacts `data`; export returns `Content-Type: message/rfc822`,
`Content-Disposition: attachment; filename=message.eml`, `Cache-Control: no-store`.

### Policies

Policy fields are `name`, `type`, `enabled`, `priority`, `scope`, `script_path`,
`script`, `max_execution_time`, `max_memory`. API saves require inline scripts.
Use `type: "starlark"` for the supported filter engine. Duration fields in JSON
are integer nanoseconds (for example 1000000000 for one second), not `"1s"`.
Avoid reserved policy names `stats` and `reload`.

A disabled rule can be stored and tested before activation:

```json
{"name":"review-example","type":"starlark","enabled":false,"priority":100,"scope":{"type":"global"},"script":"reject(\"Review example\")"}
```

POST `/api/v1/policies/review-example/test` accepts:

```json
{"From":"sender@example.test","To":["alice@example.test"],"Subject":"Synthetic test","Body":"Example content","Headers":{"X-Test":"true"}}
```

Save/test bodies are capped at 2 MiB. Test execution does not queue mail. Review
[Starlark filters](STARLARK_FILTERS.md) for action semantics. PUT replaces the
policy definition; supply the complete intended definition, including script and
enabled state. Policy changes persist atomically before activation. Reload reads
the managed policy file, not the server's YAML.

### Mailstorm and compliance actions

Pause: `{"key":"user:billing-app","reason":"Incident INC-123","duration":"30m"}`.
Resume: `{"key":"user:billing-app"}`. Keys also accept `ip:<address>` or `*` for
instance-wide pause. Bodies are capped at 4096 bytes. Check returned guard state;
active transactions can finish despite a pause.

Compliance mutation bodies are capped at 4096 bytes and reject unknown fields:

```json
{"reason":"Case closed; approved review record CASE-123","hold":false}
```

Use `hold` explicitly for legal-hold changes. Release/delete take `reason`.
A release is permitted only for eligible intercepted mail, without active legal
hold, and preserves evidence. Delete requires expired finite retention and no
legal hold. Scope alone does not bypass these conditions. A missing or unauthorized
case can return 404. See [compliance operations](COMPLIANCE_OPERATIONS.md).

### Effective configuration

Bounce and compliance configuration endpoints are read-only. Modify the deployed
YAML and restart to change rules, retries or suppression. Incoming DSN reports
are untrusted and do not automatically change suppression. General configuration,
mailbox alias mapping and credential issuance are not writable through this API.

## Routes not supplied by this server

There are no active management REST endpoints for `/listeners`, `/filters`,
`/filter-chains`, `/maps`, `/interfaces`, `/users`, `/tenants`, `/aliases`,
`/nextmailhop`, `/api-keys`, `/security/stats`, `/dns/stats`, or `/greylisting/stats`
under `/api/v1`. Older router code and CLI commands describing those paths do not
make them available. The placeholder management gRPC listener was removed.
Use file configuration, `/mailboxes`, policy APIs and supported operational tools.
Import [openapi.json](openapi.json) into your API tooling for the machine-readable
contract. Run `go test ./internal/api -run TestOpenAPI` to check document structure,
references, version, route coverage, permission mapping, serialized fields and
representative response schemas. These tests also run in release CI. Update the
contract with handler changes. Nested policy scope/action response fields use
Go field capitalization (for example `Type`, `Users`), unlike the outer policy
object. Password bounds are UTF-8 byte limits, recorded as `x-minBytes` and
`x-maxBytes`, rather than OpenAPI character counts.

Mailbox payloads cannot be deleted through the generic message DELETE endpoint
(409); remove mailbox membership through IMAP EXPUNGE. Read-only health, readiness
and queue-list routes accept GET/HEAD and reject other methods with 405. See
[JMAP API](JMAP_API.md) for the separate optional protocol and
[QA/UAT](QA_UAT.md) for executable REST acceptance checks.

## Additional operational APIs

| Method | Endpoint | Scope | Result |
| --- | --- | --- | --- |
| GET | `/api/v1/security/stats` | `security:read` | Active authentication/evaluation/listener statistics |
| GET | `/api/v1/dns/stats` | `dns:read` | Per-listener MX/TXT resolver counters/cache state |
| GET | `/api/v1/greylisting/stats` | `greylisting:read` | Per-listener enabled/triplet state |
| GET | `/api/v1/dmarc/reports` | `dmarc:read` | Aggregate report inventory |
| GET | `/api/v1/dmarc/reports/{id}` | `dmarc:read` | XML report |
| POST | `/api/v1/dmarc/reports/send` | `dmarc:write` | 204 after pending closed-day reports are queued |
| POST | `/api/v1/config/reload` | `config:write` | 202 accepted for validated graceful process replacement |
| GET/POST | `/api/v1/scim/v2/Users` | `scim:read` / `scim:write` | SCIM Users list/create |
| GET/PUT/PATCH/DELETE | `/api/v1/scim/v2/Users/{id}` | `scim:read` / `scim:write` | SCIM lifecycle |
| GET | `/api/v1/scim/v2/ServiceProviderConfig` | `scim:read` | Supported provisioning profile |

See [statistics](OPERATIONS_STATISTICS.md), [DMARC](DMARC_REPORTING.md),
[reload](CONFIGURATION_RELOAD.md), and [enterprise identity](ENTERPRISE_IDENTITY.md)
for validation/errors and supported subsets. Personal-data operations use the
[offline privacy tool](PERSONAL_DATA.md), not an unaudited HTTP delete-all endpoint.

## Optional administration and HA

`GET /api/v1/extensions` requires `extensions:read`;
`POST /api/v1/extensions/webhooks/retry` requires `extensions:write`;
`GET /api/v1/ha/status` requires `ha:read`. The optional `/admin/` assets are
public, but all console data operations require existing API permissions.
The TLS gRPC management transport uses `api/management.proto` and the same scopes.
See [extensions](EXTENSIONS_ADMINISTRATION.md) and [HA](HIGH_AVAILABILITY.md).
