# Starlark filtering extensions (2.5)

Policies may retain top-level action calls or define `filter(message)`.
The runtime executes top-level initialization once per evaluation and then
calls `filter`, if present. Return None and use `accept`, `reject`, `quarantine`,
or other existing action builtins. Return immediately after a terminal decision
to avoid replacing it with a later action. Unknown global names fail validation.

The immutable `message` global and function argument expose sender, recipients,
remote_ip, subject, size, headers (lowercase keys with tuple values), attachment
metadata, authenticated, username, internal/inbound/outbound, SPF/DKIM/DMARC/ARC
results, IP reputation and virus status. Attachment content is excluded. Fields
reflect available platform results; empty results mean no result was supplied.
Legacy inspection functions remain available.

`cidr_contains(network, address)` supports IPv4 and IPv6, normalizing IPv4-mapped
addresses. Invalid input fails evaluation. It does not authorize relay access.
See `examples/policies/internal-layer.star` for an additional defense policy.

Each evaluation allows at most ten live DNS helper calls across all helpers,
100,000 interpreter steps, and the configured execution deadline. Exhausting a
budget fails evaluation. Cached RBL results do not spend DNS calls. Scripts are
limited to 1 MiB. `print`, `log`, and `log_entry` produce at most 8 KiB of trace
in the returned Action, visible through the policy test API, instead of printing
unbounded message content to process stdout. Traces are isolated per evaluation.

These are execution controls, not an operating-system memory sandbox.
`max_memory` remains a legacy setting without a hard allocator limit. Only
trusted policy administrators should install scripts. Loading external modules
is unavailable. Existing action capabilities still depend on transport support;
this release does not introduce external notification delivery.
