# JMAP API: mailbox management and email synchronization

The optional JMAP listener uses its configured address. Put it behind a trusted
TLS terminator as described in CONFIGURATION.md. Authentication accepts shared
mail-account Basic credentials or a signed Bearer JWT configured for this
listener; disabled accounts are rejected. Management REST API keys are not JMAP
credentials. Invalid configured JWT keys or listener bind failures abort startup.
Method calls and responses use standard three-element arrays
`[methodName, arguments, callId]`; the former numeric-key object format is rejected.

Discover URLs with `GET /.well-known/jmap`. Use the discovered `primary` account
ID for `POST /jmap/api`. With the durable mailbox store, the account allows keyword
writes and mailbox management. Mailbox rights advertise keyword updates and
child creation; rename/delete are enabled except for INBOX. Message add/remove
and submission rights remain false. A custom backend without the synchronization interface stays
read-only. Request bodies, method counts and get/set/query results are bounded.

| Operation | Current behavior |
| --- | --- |
| Mailbox/get | Durable folders, parent relationships, counts, unread counts and supported rights. |
| Email/get | Owned messages across folders, keywords, internal receipt date, decoded MIME body values and attachment descriptors. |
| Email/query | Filters inMailbox, subject, from, to, text, hasKeyword and notKeyword; bounded position/limit pagination. |
| Email/set | Update keywords using replacement or patch syntax; conditional writes with ifInState; per-object notFound/invalidProperties errors. Creation/destruction return per-object forbidden errors. Other email properties cannot be changed. |
| Email/changes | Durable, account-scoped created/updated/destroyed IDs, bounded pagination and restart/restore continuity. |
| GET download URL | Owner-scoped raw message or decoded MIME part; another account gets 404. |
| Mailbox/set | Create, rename/reparent, subscribe/unsubscribe, sort order and empty-mailbox deletion; conditional state checks and per-object errors. |
| Mailbox/changes | Durable, account-scoped created/updated/destroyed mailbox IDs, including count and IMAP changes. |
| Upload | HTTP 501. |

## Mailbox management

Mailbox IDs are opaque and survive renames, reparenting, restart and backup
restore. Use IDs returned by Mailbox/get for parentId, Email/get mailboxIds and
Email/query inMailbox; never derive an ID from a folder name.

Mailbox/set accepts create/update objects with name, parentId, isSubscribed and
sortOrder. Name is a single path component; the full IMAP path must fit 255 bytes
and ten levels. parentId null means top-level. Creation defaults to unsubscribed
with sortOrder 0; role assignment is automatic for standard folders and cannot
be changed through this API. Within one request, parentId may reference a create
key as `#key`; cyclic or missing references fail with invalidProperties.

```json
["Mailbox/set", {
  "accountId": "primary",
  "ifInState": "STATE_FROM_MAILBOX_GET",
  "create": {
    "projects": {"name": "Projects", "isSubscribed": true},
    "active": {"name": "Active", "parentId": "#projects"}
  }
}, "folders"]
```

Use update keyed by mailbox ID to rename or reparent, and destroy as an array of
IDs. INBOX cannot be renamed, reparented or deleted. Deletion rejects nonempty
mailboxes with mailboxHasEmail and parents with remaining children with
mailboxHasChild; children requested for deletion are processed first.
onDestroyRemoveEmails must be absent or false. Each mailbox mutation commits
atomically, while a batch can partly succeed: inspect notCreated, notUpdated and
notDestroyed. ifInState is checked before any mutations under the shared write
lock; stale stateMismatch requests make no changes. IMAP sessions selected on a
renamed/deleted mailbox receive BYE and must reconnect.

Mailbox/changes uses the same paging/recovery procedure as Email/changes, with
its own state from Mailbox/get and separate 10,000-event history per account.
updatedProperties is null: fetch complete updated mailbox objects. Counts,
subscriptions and hierarchy changes through SMTP/IMAP also advance this feed.

**Upgrade:** back up before starting the new binary. Startup migrates mailbox.db
once, replacing the old path-derived mailbox IDs with persistent IDs. Refresh
Mailbox/get and cached Email/get mailboxIds after upgrading; the migration also
emits email update events for existing messages. Old mailbox hash states return
cannotCalculateChanges. Deleting and recreating a folder assigns a new ID.
The SQLite backup includes both change journals and stable mailbox identities.

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

Email creation/import/destruction, JMAP moves, upload, thread
grouping, submission and push remain unimplemented. Use SMTP/IMAP for these
supported mail workflows. This is not a claim of full RFC 8620/8621 or named
client interoperability. See the implementation regression tests and
scripts/jmap-sync.py (invoked by verify-release.sh) for automated coverage.
