# JMAP threads and event streams

Threads use the first syntactically valid Message-ID in References, falling back
through In-Reply-To, Message-ID, and the internal email ID. This deliberately
avoids subject-only grouping. Truncated References chains can form separate
threads; IDs never change when an ancestor is deleted. Header-derived thread IDs
are opaque identifiers, not authorization credentials: every lookup is scoped to
the authenticated mailbox owner.

`Email/get`, `Email/import`, `Email/set` and new submission receipts return the
same thread identity. `Thread/get` requires explicit IDs (maximum 500), returns
email IDs oldest first, and supports property selection. `Thread/changes` uses
retained account snapshots; expired snapshots or changes exceeding the requested
limit return `cannotCalculateChanges`, requiring a fresh fetch. Existing submission
receipts retain their original immutable thread IDs.

`Email/query` and `Email/queryChanges` accept `collapseThreads: true`; filtering
and sorting precede collapse, then pagination applies to the representatives.
Mailbox thread totals count distinct threads within each folder. Unread thread
totals count threads with at least one unread message in that folder.

Protocol references: [JMAP Mail](https://www.rfc-editor.org/rfc/rfc8621.html),
[JMAP Core](https://www.rfc-editor.org/rfc/rfc8620.html).
