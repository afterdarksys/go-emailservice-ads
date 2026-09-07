# JMAP API: mail creation, submission and synchronization

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
rights allow moves and email deletion; submission rights are enabled when the SMTP
submission adapter is wired (as in the main executable). A custom backend without the synchronization interface stays
read-only. Request bodies, method counts and get/set/query results are bounded.

| Operation | Current behavior |
| --- | --- |
| Mailbox/get | Durable folders, parent relationships, counts, unread counts and supported rights. |
| Email/get | Owned messages across folders, keywords, internal receipt date, decoded MIME body values and attachment descriptors. |
| Email/query | Filters inMailbox, subject, from, to, text, hasKeyword and notKeyword; bounded pagination and durable query snapshots. |
| Email/queryChanges | Added IDs with indices and removed IDs for a retained, matching query snapshot. |
| Email/set | Create structured MIME emails; update keywords and mailboxIds using replacement or patch syntax; destroy owned emails; conditional ifInState and per-object errors. Existing message content is immutable. |
| Email/changes | Durable, account-scoped created/updated/destroyed IDs, bounded pagination and restart/restore continuity. |
| GET download URL | Owner-scoped raw message or decoded MIME part; another account gets 404. |
| Mailbox/set | Create, rename/reparent, subscribe/unsubscribe, sort order and empty-mailbox deletion; conditional state checks and per-object errors. |
| Mailbox/changes | Durable, account-scoped created/updated/destroyed mailbox IDs, including count and IMAP changes. |
| POST upload URL | Owner-scoped temporary blobs, bounded size/storage, expiry and authenticated download. |
| Email/import | Create messages from uploaded blobs or owned raw email blobs, with mailbox, keywords, receivedAt and conditional state. |
| Identity/get | One immutable primary identity using the enabled shared mail account's email address. |
| EmailSubmission/set | Submit owned emails, delete receipts, and apply success-triggered email updates/destruction; conditional state and per-object errors. |
| EmailSubmission/get | Owner-scoped durable acceptance receipts, including after source-email deletion or queue delivery. |
| EmailSubmission/query / queryChanges | Filter/sort/page receipts and reconcile retained query snapshots. |
| EmailSubmission/changes | Paginated created/destroyed receipt IDs from a retained state. |

## Upload and import

Discover uploadUrl and downloadUrl from the session. POST the raw file bytes to
`/jmap/upload/primary/` with mail-account authentication and Content-Type (for
example message/rfc822). A successful upload returns HTTP 201 with accountId,
blobId, type and size. Uploading alone does not create an email or advance either
change feed. Downloads require the same authenticated owner, use the supplied
filename/media type, and are served as attachments with no-store/nosniff headers.

Limits are fixed in this version: 10 MiB per upload, four concurrent uploads/API
requests per server, and at most 20 temporary blobs or 100 MiB per account.
Temporary uploads expire after 24 hours. To stay within the storage limits,
new uploads evict the owner's oldest temporary blobs first, potentially before
24 hours. Retain the source file until import succeeds so it can be reuploaded.
Imported messages use independent durable payload IDs and survive upload expiry.

Pass the returned blobId to Email/import. Each import requires exactly one
owned mailbox ID and accepts keywords (default empty) and receivedAt as a UTC
RFC3339 string ending in Z. If receivedAt is omitted, the importer uses the date
in the first Received header when parseable, otherwise the import time.

```json
["Email/import", {
  "accountId": "primary",
  "ifInState": "STATE_FROM_EMAIL_GET",
  "emails": {
    "message": {
      "blobId": "UPLOADED_BLOB_ID",
      "mailboxIds": {"DESTINATION_ID": true},
      "keywords": {"$seen": true},
      "receivedAt": "2020-01-02T00:00:00Z"
    }
  }
}, "import"]
```

created maps each successful creation key to id, blobId, threadId and size.
The importer preserves raw MIME bytes, including UTF-8 headers and attachments.
Duplicate content is allowed and creates independent emails, so retrying after
an uncertain response can create a duplicate. Use ifInState and reconcile
Email/changes before retrying. Existing owned raw email blob IDs can also be
imported. Owned MIME-part blobs may be imported only when their decoded content
is itself a complete valid RFC 5322 email; a text/image attachment is not an email.

Each import is atomic: mailbox metadata, UID allocation and both change feeds
commit together. Failed metadata writes discard orphan payloads; startup recovery
handles interrupted cleanup. Successful imports generate IMAP arrival updates.
Temporary upload storage is separate from platform.mailbox_quota_bytes, which
is enforced when importing. Uploaded blobs reside in mailbox.db and are included
in backup/restore; account for up to 100 MiB per uploading account when sizing
SQLite and backups. Expired rows are reclaimed at startup and on successful
uploads; SQLite can reuse freed pages without shrinking the database file.

Troubleshooting:

- HTTP 413: upload exceeds the advertised 10 MiB limit.
- HTTP 429: concurrent request limit; retry with backoff.
- HTTP 503: upload storage failure; check disk space, permissions and service logs.
- invalidProperties: missing/expired/evicted/foreign blob, invalid mailbox ID,
  unsupported property, keyword or date. Reupload lost temporary blobs.
- invalidEmail: raw message lacks readable headers or contains NUL bytes.
- tooManyMailboxes: more than one mailbox requested.
- overQuota: mailbox byte quota or destination UID capacity exhausted.
- stateMismatch: refresh email state before retrying; no imports were performed.

This import route creates mailbox mail; it does not submit outbound messages.
Use EmailSubmission/set or SMTP to send.

## Structured email creation

Email/set create accepts mailboxIds (exactly one owned destination), keywords,
receivedAt, from/to/cc/bcc/replyTo address arrays, subject, sentAt, messageId,
inReplyTo, references, bodyStructure, textBody, htmlBody, bodyValues and attachments. Address
objects accept email and optional name. Dates use UTC RFC3339 ending in Z;
sentAt and Message-ID default to server time and a generated identifier.

Use at most one UTF-8 text/plain part and one UTF-8 text/html part. Each part's
partId references a bodyValues entry containing value. Optional isTruncated and
isEncodingProblem must be false. Omitted bodies produce an empty text part.
Attachments accept an owned uploaded, raw-email or MIME-part blobId, optional
MIME type and name, and disposition attachment or inline. For an embedded image,
set disposition inline and cid (without angle brackets), then reference
`cid:YOUR_CONTENT_ID` in HTML. Convenience bodies and inline attachments are
wrapped in multipart/related; ordinary attachments use an outer multipart/mixed.

For an explicit MIME tree, supply bodyStructure instead of textBody, htmlBody and
attachments. Supported containers are multipart/mixed, multipart/alternative and
multipart/related, each with a nonempty subParts array. Leaves supply exactly one
of partId (text from bodyValues) or blobId (transfer-decoded bytes). Set type on
text parts; other leaves default to application/octet-stream. A partId produces
UTF-8 text; omit charset (the existing explicit utf-8 value is also accepted).
Blob-backed text may specify its original charset. Optional name, disposition,
cid, language and location map to MIME headers. Size is accepted and ignored only
for blob-backed parts. Arbitrary headers and client-supplied transfer encodings
remain unsupported; the server generates MIME boundaries and base64 encoding.

```json
["Email/set", {"create": {"draft": {
  "mailboxIds": {"DRAFTS_MAILBOX_ID": true},
  "bodyStructure": {
    "type": "multipart/related",
    "subParts": [
      {"type": "text/html", "partId": "html"},
      {"type": "image/png", "blobId": "OWNED_IMAGE_BLOB_ID",
       "disposition": "inline", "cid": "logo@example.test", "name": "logo.png"}
    ]
  },
  "bodyValues": {"html": {"value": "<img src=\"cid:logo@example.test\">"}}
}}}, "compose"]
```

Email/get returns bodyStructure and leaf descriptors containing cid, disposition,
charset and owned blobId values. Inline images appear in attachments, never in
textBody/bodyValues. Downloading or reusing a MIME-part blob decodes transfer
encoding but preserves the original charset bytes; bodyValues separately converts
text for display. Use returned blob IDs rather than deriving part numbers.

Copied parts are encoded into the new durable email. Upload expiry or deletion
of the source email does not remove that copy. Source-part blobs stop resolving
when the owner no longer has the source email; foreign sources always fail.
Header injection, duplicate partIds/CIDs, unused bodyValues, conflicting body
representations and unsupported fields return invalidProperties.

Creation permits at most 128 MIME nodes and 16 nesting levels, including generated
convenience containers. Reads use the same tree bounds plus a 64 MiB raw/decoded
budget. Malformed or excessive trees return serverFail for Email/get and text
queries rather than a misleading empty body; inspect the raw message through
its original blob or IMAP. Part downloads return an error for unreadable MIME.
These are bounded supported subsets, not a claim of complete RFC 8621 support.

Each encoded email and the total prepared MIME in one Email/set are limited to
10 MiB; exceeding either returns tooLarge for the affected creation. Creation
keys are processed lexically. Split large batches into separate requests.
Mailbox quotas apply to the resulting MIME bytes, including encoding overhead.
Missing/expired/foreign attachment blobs return blobNotFound with notFound IDs.

Create, update and destroy share one ifInState check and transaction. Each object
has a savepoint: a failed creation leaves no mailbox row, allocated UID or change
event; other valid objects may succeed. New emails generate IMAP arrival updates.
Creating mail alone never submits it. To change an existing draft's content,
create a replacement and destroy the old draft after confirming creation.

## Submission and acceptance receipts

Discover urn:ietf:params:jmap:submission and Identity/get before sending. The
main executable reuses the first configured submission listener's backend,
falling back to the first SMTP listener. There is no separate send queue or
additional JMAP submission configuration. The shared account must be enabled;
both the visible From and envelope mailFrom must match its primary email address.

This example creates a draft and submits it in one API request. Replace the
mailbox ID and addresses with discovered/configured values:

```json
{
  "using": ["urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail", "urn:ietf:params:jmap:submission"],
  "methodCalls": [
    ["Email/set", {"accountId": "primary", "create": {"draft": {
      "mailboxIds": {"DRAFTS_MAILBOX_ID": true}, "keywords": {"$draft": true},
      "from": [{"email": "alice@example.test"}],
      "to": [{"email": "bob@example.test"}], "subject": "Hello",
      "textBody": [{"partId": "text", "type": "text/plain"}],
      "bodyValues": {"text": {"value": "Hello from JMAP"}}
    }}}, "compose"],
    ["EmailSubmission/set", {"accountId": "primary", "create": {"send": {
      "emailId": "#draft", "identityId": "primary"
    }}}, "submit"]
  ]
}
```

emailId may reference a creation key from a preceding method in the same request,
or the request's createdIds map. Other cross-method result references and
same-Email/set update/destroy references to new creations are not implemented.
Creation and submission are separate operations: a rejected send leaves the
created draft available for correction. A successful response returns a submission
ID and createdIds mappings. Store the submission ID for subsequent get requests.

Without envelope, recipients are derived from To, Cc and Bcc and deduplicated.
An explicit envelope requires mailFrom: {email} and rcptTo: [{email}]; parameters
must be absent or null. Bcc headers are removed from submitted bytes while the
owner's draft retains them. Recipient lookup/aliases, sender authorization,
message/recipient limits, sender quotas, mailstorm protection, policies, scanners
and durable queue admission run through the existing SMTP path. Outbound delivery
uses the existing routing/signing/retry machinery. Per-IP message limits see the
HTTP peer (normally the TLS proxy); forwarded headers are not trusted as identity.

A receipt means **accepted for processing**, including configured policy discard
or hold; it does not prove delivery. undoStatus is final, sendAt is acceptance
time, deliveryStatus is null, and DSN/MDN blob lists are empty. Normal queue mail
and its receipt share one journal transaction. Policy discard persists its receipt
before success; compliance hold persists preserved evidence before its receipt.
A receipt write failure after hold capture can leave preserved evidence despite
an API failure. Inspect compliance records before repeating that request.

Use onSuccessUpdateEmail to file Sent and clear $draft as part of submission,
without a separate client request. Keys are submission IDs or `#creationKey` for
new submissions in that method. onSuccessDestroyEmail takes submission IDs using
the same convention. Successful creation or receipt deletion triggers the requested
email action; failed operations do not. A single implicit Email/set response
follows EmailSubmission/set with the same call ID. Inspect **both** responses:
acceptance can succeed while filing fails, and retrying the send would duplicate it.

```json
["EmailSubmission/set", {
  "accountId": "primary",
  "create": {"send": {"emailId": "DRAFT_EMAIL_ID", "identityId": "primary"}},
  "onSuccessUpdateEmail": {"#send": {
    "mailboxIds": {"SENT_MAILBOX_ID": true}, "keywords/$draft": null
  }}
}, "send"]
```

Without these arguments, the source email is unchanged. A malformed hook argument
fails before sending; an unsupported email patch is reported by the implicit
Email/set after acceptance. Success hooks run on accepted creation, including
scheduled creation, and successful cancellation/deletion; they do not wait for
actual delivery. Identity mutation remains unsupported. See delayed sending below.

Destroying an owned receipt through EmailSubmission/set removes the receipt only.
It neither cancels its independently queued mail nor deletes its source email,
unless onSuccessDestroyEmail also requests that email deletion. Receipt deletions
are journaled and survive restart/restore. Get methods retain the 500-object limit;
use EmailSubmission/query pagination to find IDs in larger accounts.

Receipts survive delivery, compaction, restart and backup restore and remain
owner-scoped even after the email is deleted. They contain envelope addresses and
email/identity IDs, but no MIME body. They consume disk, not pending-message quota.
Automatic expiry is opt-in through platform.submission_retention_days; zero
retains indefinitely. Pending receipts and receipts linked to active queue records
are protected. Explicit deletion of pending receipts is forbidden: cancel first.
Include retained metadata in capacity and retention planning. After an uncertain response, inspect
receipts and refresh state before retrying: an explicit retry with fresh state
creates another send. There is no retry idempotency key.

Submission troubleshooting:

- stateMismatch: refresh EmailSubmission/get state; no submissions were attempted.
- invalidProperties: missing/foreign/non-email emailId, invalid identity/envelope,
  unsupported property or an unresolved creation reference.
- forbiddenFrom / forbiddenMailFrom: use the account's Identity/get address.
- noRecipients / tooManyRecipients / tooLarge: correct recipients or encoded size;
  server.max_recipients and server.max_message_bytes apply.
- forbiddenToSend: SMTP rejected admission; inspect recipient validation, policy
  and scanner logs. Temporary admission or persistence failures return serverFail.
- Receipt exists but no mail arrives: inspect queue, quarantine, compliance hold,
  policy discard, routing and recipient Sieve. A receipt is not delivery status.

## Delayed sending and cancellation

The built-in submission backend advertises maxDelayedSend=2592000 (30 days) and
FUTURERELEASE in its account submissionExtensions. Supply HOLDFOR (seconds as a
string) or HOLDUNTIL (UTC `YYYYMMDDTHHMMSSZ`) in envelope.mailFrom.parameters.
Exactly one parameter is supported; the visible From and envelope sender still
must match Identity/get. A past release time sends immediately. sendAt is a
server-set response property, not a writable scheduling argument.

```json
["EmailSubmission/set", {"create": {"later": {
  "emailId": "DRAFT_EMAIL_ID", "identityId": "primary",
  "envelope": {
    "mailFrom": {"email": "you@example.test", "parameters": {"HOLDFOR": "300"}},
    "rcptTo": [{"email": "recipient@example.test"}]
  }
}}}, "schedule"]
```

The accepted copy and its pending receipt commit together and survive restart,
backup restore, and deletion of the source draft. Scheduling consumes spool quota.
The retry scheduler checks every 30 seconds and releases due mail to normal
delivery; it never releases before sendAt. Actual delivery can be later because
of backpressure, scanner/policy decisions or recipient outages. Quarantined mail
remains held after release. Delayed submissions matching compliance custody rules
are rejected before custody side effects; use immediate submission for those
routes. Admission checks/scanning run when the submission is accepted.

Cancel a pending submission with:

```json
["EmailSubmission/set", {"update": {
  "SUBMISSION_ID": {"undoStatus": "canceled"}
}}, "cancel"]
```

Cancellation and release use one serialized durable transition. Successful
cancellation removes the active delivery payload and preserves a canceled receipt;
repeating cancellation is safe. Once released, cancellation returns cannotUnsend.
Deleting a pending receipt returns forbidden, preserving its cancellation handle.
After a successful cancellation, deletion removes only the receipt. Original
journal/tier bytes are reclaimed by compaction, not necessarily at cancellation.

EmailSubmission/changes reports updated IDs for pending/final/canceled transitions
and destroyed IDs for receipt expiry/deletion. Retained query snapshots also track
membership changes for undoStatus filters. Refresh state before retrying a stale
mutation. Pre-upgrade snapshots may conservatively report existing receipts as
updated. Existing snapshot limits and cannotCalculateChanges recovery still apply.

## Query pagination and troubleshooting

Email/query and EmailSubmission/query accept `anchor` and signed `anchorOffset`
as well as signed `position`. The anchor must occur in the authenticated account's
filtered, sorted results. An offset of -1 starts one item before it; a negative
resulting index is clamped to zero. With no anchor, a negative position counts
backward from the end. An anchor overrides position, and anchorOffset is ignored
without an anchor. These rules follow [RFC 8620 section 5.5](https://www.rfc-editor.org/rfc/rfc8620.html#section-5.5).

```json
["Email/query", {
  "accountId": "primary",
  "filter": {"inMailbox": "MAILBOX_ID"},
  "anchor": "EMAIL_ID",
  "anchorOffset": -10,
  "limit": 21
}, "around-email"]
```

`position` in the response is the resolved index. Positions past the end return
an empty `ids` list while retaining that index. Limit zero can locate an anchor
without returning IDs; pages remain capped at 500 objects. Pagination does not
change queryState or restrict the full snapshot used by queryChanges.

- `anchorNotFound`: the anchor was deleted, belongs to another account, or does
  not match the filter. Refresh the query without an anchor to locate a new one.
- `invalidArguments`: check anchor type, integer position/offset, and nonnegative
  limit. Fractional and out-of-range integers are rejected.
- Paging results change between calls: compare queryState, then use queryChanges
  or refresh. Anchors locate current results; they do not freeze a result set.

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

The durable store uses per-email savepoints: a failed combined keyword/membership
update rolls back all changes to that email. Independent objects in the batch
can succeed; always inspect notUpdated and notDestroyed.

## Move and delete emails

Email/set updates mailboxIds to move an existing email. The server advertises
maxMailboxesPerEmail=1: the final membership must contain exactly one owned,
existing mailbox. An empty set returns invalidProperties; multiple memberships
return tooManyMailboxes. Missing or foreign destinations return invalidProperties.

Replace the entire membership with `"mailboxIds": {"DESTINATION_ID": true}`,
or remove the old membership and add the new one in the same update:

```json
["Email/set", {
  "accountId": "primary",
  "ifInState": "STATE_FROM_EMAIL_GET",
  "update": {
    "EMAIL_ID": {
      "mailboxIds/SOURCE_ID": null,
      "mailboxIds/DESTINATION_ID": true,
      "keywords/$seen": true
    }
  }
}, "move"]
```

Use IDs from Mailbox/get. Replacement and patch syntax cannot be mixed for the
same property. Moving preserves email/blob identity, payload, internal date and
existing flags, assigns a destination IMAP UID, and consumes no additional
message quota. Assigning the current mailbox is a membership no-op. A simultaneous
keyword update commits with the move or rolls back with it.

To delete, send `"destroy": ["EMAIL_ID"]` in Email/set. This removes the message
from active mail and download access; it does not move it to Trash. To retain a
recoverable message, move it to the mailbox with role trash instead. Destruction
removes only requested IDs, even when other messages carry the IMAP Deleted flag.
Missing, already-destroyed or foreign-owned email IDs return notFound. An ID in
both update and destroy gets willDestroy in notUpdated; destruction is processed.

Selected IMAP sessions receive descending EXPUNGE notifications for removed
messages and destination arrival/flag notifications for moves. Email/changes
reports moved IDs as updated and deleted IDs as destroyed; Mailbox/changes reports
the corresponding count updates. Both feeds survive restart and backup restore.
Metadata, UID allocation and change events commit together. Payload tombstoning
follows the commit; interrupted cleanup is retried at startup and logged as
"Deferred JMAP deletion payload cleanup". A cleanup failure does not make a
committed deletion appear to have failed. Storage retention/compaction governs
physical reclamation; this API does not promise forensic erasure.

## Incremental email synchronization

Call Email/changes with `sinceState` from Email/get or the previous changes
response. Optionally set positive `maxChanges` (the server caps a page at 500 IDs).
Apply created/updated/destroyed IDs, then continue with `newState` while
`hasMoreChanges` is true. Fetch created/updated objects with Email/get. An object
may disappear between changes and get; handle notFound and continue syncing.

The SQLite change journal records mutations from SMTP delivery, IMAP flags,
MOVE, folder rename/deletion, expunge and JMAP email writes. It retains the
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

Email queries use newest internal date first with deterministic ID tie-breaking.
Explicit email sort requests return unsupportedSort; unsupported filters return
unsupportedFilter. Receipt queries default to newest sendAt first and support
sorting by emailId, threadId and sendAt (sentAt is accepted as an alias), with
isAscending and an ID tie-breaker. Receipt filters accept identityIds, emailIds,
threadIds, undoStatus, before and after; all supplied conditions must match.

Query methods return canCalculateChanges=true when the complete result has at
most 10,000 IDs and the durable snapshot backend is available. Larger queries
remain pageable but require full reconciliation. Pass the queryState plus the
same filter/sort to the corresponding queryChanges method. Remove returned IDs
first, then insert each added ID at its returned index. calculateTotal=true adds
the new total. maxChanges is bounded to 500; tooManyChanges means refresh the
query, not an incomplete successful delta. Query pagination supports anchors as
described above; queryChanges does not accept pagination anchors. upToId and
thread collapsing are not implemented and fail explicitly. Email/changes remains the independent
object change feed.

The mailbox database retains 64 distinct recently used snapshots per account for
each of Email queries, receipt queries and receipt object states. Tokens are
owner- and query-scoped and survive backup/restore. Old, foreign, mismatched or
unavailable tokens return cannotCalculateChanges; issue a fresh query/get and
reconcile. Full snapshots store IDs only, capped at 10,000 per snapshot. Include
this bounded metadata in SQLite/backup capacity planning. Refreshing a snapshot
renews its place in history. Future tokens are unavailable after restoring an
older backup.

EmailSubmission/changes accepts sinceState and maxChanges, returns created and
destroyed IDs (updated is empty), and supplies a resumable newState when
hasMoreChanges is true. Receipt states are recorded by get/query/set. Deleting
receipts does not remove earlier membership snapshots; those age out under the
same 64-snapshot bound. This is synchronization history, not an audit log.

Arbitrary-header composition, thread grouping and push remain
unimplemented. This is not a claim of full RFC 8620/8621 or named
client interoperability. See the implementation regression tests and
scripts/jmap-sync.py, scripts/jmap-email-mutations.py, scripts/jmap-import.py and
scripts/jmap-compose-send.py and scripts/jmap-workflows.py (invoked by verify-release.sh) for automated coverage.

Protocol references: [JMAP mail and submission](https://www.rfc-editor.org/rfc/rfc8621.html), [JMAP core](https://www.rfc-editor.org/rfc/rfc8620.html). Supported subsets and limits above remain authoritative for this implementation.
