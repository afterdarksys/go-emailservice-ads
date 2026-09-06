# SMTP IP filtering and reputation

The main SMTP listener supports IP allowlists (whitelists), denylists, and DNS-based real-time blocklists (RBL/DNSBL). Configure them under `server.ip_filter`:

```yaml
server:
  ip_filter:
    allowlist: []       # Individual IPv4/IPv6 addresses or CIDRs
    denylist: []        # Individual IPv4/IPv6 addresses or CIDRs
    rbl_zones: []       # DNSBL zones you are authorized to query
    timeout: "5s"      # Shared deadline for all zones per SMTP session
    defer_on_error: false
```

For example, `denylist: ["192.0.2.0/24", "2001:db8::1"]` rejects those example addresses. An empty configuration leaves filtering disabled. Invalid addresses, CIDRs, zones, and timeouts are rejected during configuration loading. Restart the service after changing configuration.

Checks run when the SMTP session is created after HELO/EHLO, using the TCP peer address. Denylist matches take precedence and return 554. Allowlist matches skip RBL checks. Other clients are queried against the configured zones; a positive listing returns 554. Allowlisting does not bypass authentication, relay authorization, rate limits, or other mail checks. An allowlist is an exemption list, not an exclusive list of permitted clients.

NXDOMAIN means unlisted. DNS failures are ignored by default; `defer_on_error: true` returns 451 so clients can retry. Unexpected answers and provider error responses in 127.255.255.* are treated as lookup failures, not listings. Select zones whose positive answers use standard 127.* addresses. Both reversed IPv4 octets and reversed IPv6 nibbles are supported. Lookups use the system resolver and are performed per session; no application-level cache is provided.

Behind an SMTP proxy, these checks apply to the proxy's TCP address. Apply original-sender IP policy at the perimeter proxy; SMTP headers cannot override the peer address.

## Existing IP reputation

Historical IP reputation already exists in the optional `adspremail` perimeter service: `internal/premail/scoring` combines connection behavior and historical PostgreSQL records into scores and actions, while `internal/premail/reputation` provides the optional DNSScience integration. See [premail setup](../ADS_PREMAIL_README.md) and [operations](ADS_PREMAIL_OPERATIONS.md) for deployment, scoring weights, and reputation management.

That reputation engine is separate from the main SMTP listener. Enabling `server.ip_filter` does not enable historical scoring or the external reputation feed. The older Postfix-style `internal/access` restriction engine also remains separate; use the configuration above for active SMTP IP/RBL enforcement.
