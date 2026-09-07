# Extensions and administration

All new listeners, webhooks and plugins are opt-in. Existing REST permissions
remain authoritative; enabling a new transport does not grant broader access.

## Web console

Set `api.admin_enabled: true` and configure `api.tls` (certificate/key, optional
client CA). Open `https://your-api-host/admin/`. Enter a scoped API credential;
it stays in tab memory, not cookies or browser storage. Disconnect clears access
and displayed results. The console provides operational views and account
create/update/delete forms. Newly created accounts are enabled; the enabled
checkbox applies to updates. Identity deletion retains mail; personal-data
requests use the offline privacy workflow.

The assets contain no secrets and are public when enabled; every data request is
authenticated by the existing API. Browser rendering uses text content, a strict
same-origin CSP and no third-party scripts. TLS is mandatory. UI assets and API
contracts are tested automatically; actual desktop/mobile browser acceptance
still belongs in PRELIMINARY_UAT.md.

## gRPC management

```yaml
api:
  grpc_enabled: true
  grpc_addr: ':50051'
  admin_enabled: true
  tls:
    cert: /etc/mailhub/tls/server.crt
    key: /etc/mailhub/tls/server.key
```

The wire contract is `api/management.proto`, service
`mailhub.management.v1.Management`, unary method `Call`. It uses protobuf Struct:

```json
{"method":"GET","path":"/api/v1/queue/stats","body":""}
```

Send `authorization: Bearer <credential>` metadata. The server passes the real
peer IP and TLS identity through existing REST authentication, IP restrictions,
OAuth introspection and resource scopes. No reflection service is exposed;
load the provided proto into grpcurl or generate a client. Calls support GET,
POST, PUT, PATCH and DELETE with relative `/api/v1/` paths. `body` is JSON text,
not a nested object. Response fields are numeric HTTP `status`, textual `body`,
`content_type` and `location`. Application failures retain HTTP status; invalid
RPC envelopes use gRPC errors. TLS uses the same API certificate/client-CA policy.
See [gRPC authentication documentation](https://grpc.io/docs/guides/auth/).

Limits: 1 MiB request body, 2 MiB protobuf request, 3 MiB response body, 32 streams
per connection and a 30-second call context. Use REST for larger exports. A
connection loss after mutation may have an uncertain outcome; inspect the
resource before retrying. Shutdown stops active RPCs; operations are not made
idempotent merely by using gRPC.

## Durable management webhooks

```yaml
platform:
  webhooks:
    - name: audit-collector
      url: https://audit.example.net/mailhub/events
      secret_env: MAILHUB_WEBHOOK_SECRET
```

Use a secret of at least 32 bytes. Up to eight operator-configured HTTPS
destinations are allowed; redirects are not followed. Callers cannot supply a
webhook destination through the API. Place credentials in the service environment,
not in source-controlled configuration. A destination name identifies its queue;
keep its meaning stable. Removing it leaves undelivered items for disposition.

Authenticated management mutations persist an event intent in
`platform.data_dir/webhooks.db` before the handler runs and its outcome afterward.
Requests are refused if intent cannot be persisted or the 10,000-event bound is
reached. Events contain ID, time, principal, HTTP method/path, status and outcome;
request bodies, response bodies and credentials are omitted. This is a
management-event feed, not a message delivery notification feed.

On restart unfinished intents become `interrupted_or_unknown` events with status
zero. They do not assert that the mutation succeeded or failed. Completed events
record the handler's HTTP status (202 still means accepted, not finished).
The outbox is durable, but the resource mutation and outbox are separate
transactions; consumers must handle uncertain outcomes explicitly.

POST delivery headers:

- `X-Mailhub-Event-ID`: stable UUID for deduplication.
- `X-Mailhub-Timestamp`: Unix seconds at this delivery attempt.
- `X-Mailhub-Signature`: `sha256=` plus hex HMAC-SHA256 of timestamp, a literal
  period and the exact raw JSON body, using the configured secret.

Consumers must compare signatures in constant time, enforce a short timestamp
window and deduplicate event IDs. A 2xx response acknowledges delivery. Failed
attempts back off exponentially and stop after ten attempts as `dead`. Lost
acknowledgements can produce duplicates. Acknowledged events expire after seven
days when later events are enqueued; dead/pending events are never auto-evicted.
Each HTTP attempt has a ten-second timeout; only one bounded batch worker runs.

`GET /api/v1/extensions` (`extensions:read`) returns delivery counts and enabled
features. `POST /api/v1/extensions/webhooks/retry` (`extensions:write`) resets dead
attempts. Retry is excluded from event recording so it can recover a full outbox.
For investigation, stop the service and inspect the SQLite events/deliveries
tables read-only. Back up before manual disposition. Outbox metadata may identify
accounts; include retained webhook history and external collectors in privacy
request disposition.

## Admission plugins

```yaml
platform:
  admission_plugins:
    - name: organization-policy
      url: https://policy.example.net/mailhub/admission
      token_env: MAILHUB_PLUGIN_TOKEN
      required: true
      include_body: false
```

Up to four HTTPS services run sequentially during DATA admission, after built-in
security checks and before mail acceptance. Each uses `Authorization: Bearer`
with an environment secret of at least 32 bytes. The version-1 JSON request has
`version`, envelope `from`, `to`, `client_ip`, `username`, `authenticated`, and an
optional base64 RFC 5322 `data` field. Bodies are omitted unless explicitly
selected. Envelope/account metadata is still personal data: qualify the plugin's
access and retention. The existing message-size bound applies to input.

Return HTTP 200 and exactly `{"action":"allow","reason":""}`. Actions are
`allow`, `reject` (SMTP 550), `defer` (451), or `quarantine` (Junk). Unknown fields,
unknown actions, extra JSON, response bodies over 4 KiB and control characters in
reasons are rejected. Reasons are at most 256 bytes. Required plugin errors defer
mail; optional plugin errors continue to the next plugin. A later allow cannot
undo a preceding quarantine. Each call has a five-second timeout and the group
has a twenty-second deadline; redirects are refused. Plugins cannot rewrite
recipients, bypass relay rules, or execute server commands. SMTP and JMAP
submission share admission. These are external protocol plugins, not in-process
Go shared libraries or arbitrary executable uploads.

## Transport routing

`platform.transports` now supports `domain: '*.example.net'`, `sender_domain`
and nonnegative `priority`, alongside existing exact domains and `'*'` fallback.
An exact recipient domain outranks a suffix; the longest suffix wins. At equal
domain specificity a matching sender rule outranks a general rule, then lower
priority wins. A suffix does not match its apex. Sender matching uses the envelope
sender domain, not the untrusted From header; a null sender matches general rules.
Duplicate domain/sender selectors are rejected. These rules select outbound
transports; they do not grant relay permission or replace mailbox ownership.

Existing next-hop ordering, verified TLS, optional mTLS and credential-env
settings still apply. If all configured next hops fail temporarily, the message
retries; it does not bypass the configured transport by falling back to public MX.
