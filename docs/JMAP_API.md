# JMAP API: current read-only subset

The optional JMAP listener uses its own configured address. Put it behind a trusted
TLS terminator as described in CONFIGURATION.md. Authentication accepts the shared
mail account's Basic credentials or a signed Bearer JWT configured for this
listener; disabled accounts are rejected. Management REST API keys are not JMAP
credentials. An invalid configured JWT public key or listener bind failure aborts
startup.

Discover URLs through `GET /.well-known/jmap`. The session advertises the primary
account as read-only. Send method calls to `POST /jmap/api` using the discovered
account ID, `primary`. Request bodies and get/query results are bounded; unsupported
methods and invalid account IDs produce method-level errors.

| Operation | Current behavior |
| --- | --- |
| Mailbox/get | Durable folders, parent relationships, counts, unread counts and read-only rights. |
| Email/get | Owned messages across folders, flags as keywords, internal receipt date, decoded MIME body values and attachment descriptors. |
| Email/query | Filters inMailbox, subject, from, to, text, hasKeyword and notKeyword; bounded position/limit pagination. |
| GET download URL | Owner-scoped raw message or decoded MIME part; another account gets 404. |
| Email/set, Mailbox/set | accountReadOnly error; no fabricated write success. |
| Email/changes, Mailbox/changes | cannotCalculateChanges; clients must retrieve current state. |
| Upload | HTTP 501. |

Query results use newest internal date first with deterministic ID tie-breaking.
Explicit sort requests return unsupportedSort; unsupported filters return
unsupportedFilter. This subset does not provide full RFC 8621 interoperability,
thread grouping, submission, push events or writable mail synchronization. Use
SMTP and IMAP for the supported writable mail workflow. State tokens reflect
current durable metadata; do not interpret them as numeric counters.
