# TLS and credential rotation

SMTP, IMAP and REST load certificate/key material at startup and reload it for
each new TLS handshake. Existing sessions finish with their established keys.
Invalid or mismatched replacement material fails new handshakes. Deploy pairs
using atomic versioned-secret directory switches; monitor certificate-expiry
metrics and test a new connection after renewal.

For internal listeners set `tls.require_client_cert: true` and
`tls.client_ca_file: /etc/tls/client-ca.pem`. Client authentication supplements
trusted connector CIDRs and relay restrictions. Configure the sending next hop
with `require_tls: true`, `client_cert` and `client_key`. Both sides verify their
peer; never use insecure certificate verification to accommodate private PKI.
Bundle old and new CA certificates during CA rollover, rotate leaf certificates,
then remove the old CA after the overlap period.

API key configurations support `key_files: [/run/secrets/api-current]` and
`expires_at: 2026-12-01T00:00:00Z`. Values are reread on each authenticated request.
Use two separately scoped key entries with different expiry dates during a
rotation. Remove the old file/entry after client migration. Environment-backed
keys retain their startup semantics; use files for live rotation.

Certificate issuance remains the responsibility of the organization's CA or
certificate controller. The sample cert-manager resource targets a preconfigured
private CA ClusterIssuer; install and secure that issuer before applying it.
Public ACME cannot issue certificates for Kubernetes-internal hostnames.
