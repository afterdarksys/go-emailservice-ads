# API authentication — 2.8.0

The active management REST server accepts `Authorization: Bearer <token>`.
Use a configured scoped API key or a configured OAuth access token over TLS.
Basic Auth and SMTP/IMAP account passwords do not grant management access.
See [the endpoint reference](docs/API_REFERENCE.md) for exact methods and scopes.

## Configure a static key

```yaml
api:
  rest_addr: ':8080'
  tls:
    cert: /etc/mailhub/tls/tls.crt
    key: /etc/mailhub/tls/tls.key
  require_ip_auth: true
  allowed_ips: ['127.0.0.1', '::1']
  api_keys:
    - name: queue-observer
      key_env: MAILHUB_QUEUE_KEY
      permissions: [queue:read]
```

Provision the environment variable through your supervisor/secret manager and
restart. `key_env` resolves at startup and missing referenced secrets fail config
loading. Literal `key` does not interpolate `${VAR}`. Alternatively use
`key_files: [/run/secrets/queue-key]`; file contents are trimmed, bounded to 4096
bytes and reread per request. `expires_at` accepts an RFC3339 timestamp. Use
[overlapping credentials](docs/CREDENTIAL_ROTATION.md) for rotation. There is no
active key-issuance API; configure key entries and permissions in YAML.

Permissions match exact `resource:read` / `resource:write` strings or global `*`.
Empty permissions deny access; `all`, bare `read` and `resource:*` do not match.
Queue/message/DLQ use `queue`; users use `mailboxes`. Test/reload actions require
`policies:write`. Compliance export, release, delete and legal-hold use their
own scopes. Assign a distinct key name per actor because the name is the audit
principal and is used for compliance domain grants.

`allowed_ips` matches exact TCP peer IPs when `require_ip_auth` is enabled; an
empty allowlist supplies no source restriction. CIDRs and forwarded HTTP headers
are not supported here. Health, readiness, version and metrics bypass the
protected-route middleware; restrict the management port at the network layer.

## Make a request

Set API_BASE to your HTTPS origin and supply MAILHUB_API_KEY securely:

```sh
curl --fail-with-body --cacert /etc/mailhub/ca.pem   -H "Authorization: Bearer $MAILHUB_API_KEY"   "$API_BASE/api/v1/queue/stats"
```

Omit `--cacert` when the certificate chains to the system trust store. Do not
embed administrative keys in browser-delivered JavaScript. Proxy administrative
operations through an appropriately authorized server-side integration.

## OAuth access tokens

`api.oauth` uses token introspection with configured endpoint/client credentials,
issuer, audience, expiry and requested resource scope. This is independent of
legacy `sso` settings and JMAP JWT verification. Enabling it requires `api.tls`;
TLS termination only at an upstream proxy does not satisfy the handler's TLS check.

Use [examples/compliance-config.yaml](examples/compliance-config.yaml).
`compliance_only` limits OAuth validation to compliance paths.
`require_for_compliance` requires OAuth on those paths; otherwise a configured
static key can still authenticate. OAuth principals are `oauth:<subject>`.
With domain enforcement enabled, configure matching
`platform.compliance.access` principal, domains and actions as well as scopes.
Lists filter inaccessible cases/rules; individual inaccessible cases return 404.

Build and run `mailhub-authcheck` as described in
[deployment qualification](docs/DEPLOYMENT_QUALIFICATION.md) with the actual
provider and valid/revoked tokens. API introspection support does not imply SMTP
OAuth or a token-issuing endpoint. SCIM Users provisioning and SAML-broker
integration are documented in [enterprise identity](docs/ENTERPRISE_IDENTITY.md).

## Error handling

Missing/invalid credentials return 401. IP restriction and recognized literal
keys lacking scope can return 403. File-backed key or OAuth authorization failures
may also return 401. Read the plain-text response before diagnosing. Expired
credentials, unavailable introspection, wrong scopes and TLS errors need operator
review. There is no uniform JSON error envelope. See
[troubleshooting](docs/TROUBLESHOOTING.md) for concrete checks.

Dedicated operational scopes: `security:read`, `dns:read`, `greylisting:read`,
`dmarc:read`, `dmarc:write`, `scim:read`, `scim:write`, and `config:write`.
