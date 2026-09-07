# JMAP API: reads, keyword writes and email changes

The optional JMAP listener uses its configured address. Put it behind a trusted
TLS terminator as described in CONFIGURATION.md. Authentication accepts shared
mail-account Basic credentials or a signed Bearer JWT configured for this
listener; disabled accounts are rejected. Management REST API keys are not JMAP
credentials. Invalid configured JWT keys or listener bind failures abort startup.
Method calls and responses use standard three-element arrays
`[methodName, arguments, callId]`; the former numeric-key object format is rejected.

Discover URLs with `GET /.well-known/jmap`. Use the discovered `primary` account
ID for `POST /jmap/api`. With the durable mailbox store, the account allows keyword
writes and mailbox rights advertise `maySetSeen`/`maySetKeywords`. Other mutation
rights remain false. A custom backend without the synchronization interface stays
read-only. Request bodies, method counts and get/set/query results are bounded.

| Operation | Current behavior |
| --- | --- |
| Mailbox/get | Durable folders, parent relationships, counts, unread counts and supported rights. |
| Email/get | Owned messages across folders, keywords, internal receipt date, decoded MIME body values and attachment descriptors. |
| Email/query | Filters inMailbox, subject, from, to, text, hasKeyword and notKeyword; bounded position/limit pagination. |
| Email/set | Update keywords using replacement or patch syntax; conditional writes with ifInState; per-object notFound/invalidProperties errors. Creation/destruction return per-object forbidden errors. Other email properties cannot be changed. |
| Email/changes | Durable, account-scoped created/updated/destroyed IDs, bounded pagination and restart/restore continuity. |
| GET download URL | Owner-scoped raw message or decoded MIME part; another account gets 404. |
| Mailbox/set | forbidden with the durable store; accountReadOnly with a read-only custom backend. |
| Mailbox/changes | cannotCalculateChanges; retrieve current mailbox state. |
| Upload | HTTP 501. |

## Keyword updates

Capture `state` from Email/get and submit it as `ifInState` to avoid overwriting
concurrent changes. The comparison and updates run in the same storage transaction.
A stale token returns `stateMismatch` without changing any email. Omit ifInState
only when conditional update protection is unnecessary.

```json
{
  "using": ["urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"],
  "methodCalls": [["Email/set", {
    "accountId": "primary",
    "ifInState": "STATE_FROM_EMAIL_GET",
    "update": {
      "EMAIL_ID": {"keywords/$seen": true, "keywords/$flagged": null}
    }
  }, "edit"]]
}
```

Use true to add a keyword and null to remove one. A complete `keywords` object
replaces visible keywords. Replacement and nested patches cannot be combined on
the same email. JSON Pointer escaping uses `~0` for tilde and `~1` for slash.
$seen, $flagged, $answered and $draft map to their IMAP system flags. Custom
keywords use printable ASCII IMAP-compatible names of up to 255 bytes. IMAP
Deleted/Recent flags are not exposed as JMAP keywords or cleared by replacement.
These writes generate IMAP flag notifications.

## Incremental email synchronization

Call Email/changes with `sinceState` from Email/get or the previous changes
response. Optionally set positive `maxChanges` (the server caps a page at 500 IDs).
Apply created/updated/destroyed IDs, then continue with `newState` while
`hasMoreChanges` is true. Fetch created/updated objects with Email/get. An object
may disappear between changes and get; handle notFound and continue syncing.

The SQLite change journal records mutations from SMTP delivery, IMAP flags,
MOVE, folder rename/deletion, expunge and JMAP keyword writes. It retains the
latest 10,000 events per account. Multiple events for one email are combined
within each page, including changes subsequently reversed. State tokens are
opaque and scoped to the account and database; they persist across restart and
backup restore. Tokens from the former hash-based implementation, another
account/database, or expired history return cannotCalculateChanges. In that
case, retrieve a complete current ID set and resume from the new state. A backup
restored to an older point cannot honor tokens issued after that point.

The change log is stored in mailbox.db and is automatically migrated on startup;
existing live messages initialize its history once. Back up before upgrading.
Retained journal entries contain account names, message IDs and change types,
not message bodies. Include this metadata in the deployment's retention policy.

## Limits and qualification

Queries use newest internal date first with deterministic ID tie-breaking.
Explicit sort requests return unsupportedSort; unsupported filters return
unsupportedFilter. Email/query still reports canCalculateChanges=false:
Email/queryChanges is not implemented. Email/changes is an object change feed,
not incremental query membership or ordering.

Mailbox writes, email creation/import/destruction, JMAP moves, upload, thread
grouping, submission and push remain unimplemented. Use SMTP/IMAP for these
supported mail workflows. This is not a claim of full RFC 8620/8621 or named
client interoperability. See the implementation regression tests and
scripts/jmap-sync.py (invoked by verify-release.sh) for automated coverage.
