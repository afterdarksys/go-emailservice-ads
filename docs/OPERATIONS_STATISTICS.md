# Operational statistics

The active REST server provides authenticated GET endpoints:

| Endpoint | Permission | Contents |
| --- | --- | --- |
| `/api/v1/security/stats` | `security:read` | SPF/DMARC evaluation counters, greylist deferrals, per-listener security settings, account/IP lockout counts |
| `/api/v1/dns/stats` | `dns:read` | Actual SMTP listener resolver MX/TXT cache entries, hits, misses, failures |
| `/api/v1/greylisting/stats` | `greylisting:read` | Enabled state and active/whitelisted triplet counts for each listener |

Counters describe the current process lifetime and reset after restart/reload.
They are snapshots, not an audit log or a cross-node total. Listener addresses
identify the source; no sender/recipient names, passwords or message content are
returned. Greylisting disabled is represented explicitly, not by invented zero
traffic. DNS counters cover MX/TXT queries on these resolvers, not every external
library's resolver. Prometheus queue status now includes `scheduled` messages.

Use deltas between samples for rates. Rising DMARC failures may reflect monitor
traffic rather than rejection: inspect listener mode. Rising DNS failures and
cache misses suggest resolver/connectivity issues; greylist deferrals with growing
triplets may indicate novel sender traffic. Preserve monitoring externally across
process restarts. Protect these endpoints with dedicated read-only scopes and the
same API TLS/IP controls as other administrative endpoints.
