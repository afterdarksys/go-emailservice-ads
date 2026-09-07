# Preliminary UAT acceptance record

This record covers a single-owner test deployment using SMTP submission, IMAP
and the management REST API. A passing automated run is prerequisite evidence;
the intended user's desktop/mobile client must also complete the acceptance
table. Leave unexecuted rows pending. Do not claim a preliminary UAT pass while
an in-scope workflow has a blocking defect or lacks evidence.

## Candidate and evidence

Record the commit, PR/merge status, build SHA-256 and CI run URL; test host/OS,
redacted configuration, TLS trust and synthetic accounts; client/OS versions,
operator and date; and evidence paths/results/defects for each acceptance row.

Run from a clean checkout, using new report and log paths for each attempt:

```sh
MAILHUB_QUALIFICATION_REPORT=/tmp/mailhub-live-candidate.json \
  bash scripts/verify-release.sh > /tmp/mailhub-candidate.log 2>&1
```

Require exit status zero and retain both files. The JSON records **only the live
smoke stage**, not earlier package/race/build stages. If execution fails before
the live stage, no fresh report is created. Never reuse a previous passing report
as evidence for a failed attempt. Compare its revision, dirty-tree status and
binary hashes to the intended candidate. Dirty-checkout reports are development
evidence and cannot identify an exact release candidate.

## Acceptance table

Automated coverage below is implemented in the release gate; record results from
the actual candidate run rather than pre-filling a pass. Client rows require the
named application and human-visible observations.

| ID | Test | Required result | Evidence/result |
| --- | --- | --- | --- |
| A1 | Full release gate | Package/race tests, builds, protocol checks and restored-service verification complete with exit zero. | Pending candidate run |
| A2 | Three selected sessions | APPEND counts, IDLE FLAGS, silent STORE and EXPUNGE reach intended sessions; another account receives none. | Included in A1 |
| A3 | Two concurrent writers | Sixteen messages have unique UIDs; both added keywords persist on all messages. | Included in A1 |
| A4 | Drop an IDLE connection | Remaining sessions mutate; reconnect preserves UIDVALIDITY, omits the deleted UID and exposes a new, higher UID. | Included in A1 |
| A5 | MOVE/UID MOVE | Selected messages move atomically; unrelated deleted messages remain; source/destination sessions update; moved payload/metadata survive restore. | Included in A1 |
| A6 | JMAP keyword updates | API writes appear in IMAP; stale/foreign writes fail; Email/changes survives restore. | Included in A1 |
| A7 | JMAP mailbox management | Stable IDs, create/rename/reparent, subscription and deletion protection; Mailbox/changes survives restore. | Included in A1 |
| C1 | Add two accounts in the target client | TLS/authentication work; accounts see only their own folders/messages. | Pending |
| C2 | Send/receive a reply with attachment | Recipient, subject, body and attachment are intact; Sent placement matches configuration. | Pending |
| C3 | Folder lifecycle | Create, subscribe, rename, move/copy, delete and reconnect show durable state. | Pending |
| C4 | Read/unread, flags and deletion across sessions | Changes appear in both; deletion/expunge leave no ghost messages. | Pending |
| C5 | Offline/reconnect | Change mail while offline, reconnect; messages and flags reconcile without loss or duplication. | Pending |
| C6 | API and supported Sieve | Operator manages accounts/policies and verifies a routing/flag rule on delivered mail. | Pending |

Test the actual client's move behavior in C3. Protocol MOVE/COPY/EXPUNGE coverage does
not prove every advertised client workflow works. A failing action used by the
target client is an in-scope defect, not a waived pass.

## Decision

Current record: **not signed off**. Fill in candidate, client and evidence first.
After all rows pass, the operator may record:

> Preliminary UAT passed for [commit/build], [environment] and [client/version]
> on [date], covering SMTP/IMAP, supported Sieve and management API workflows in
> this table. Evidence: [paths/links]. Accepted by: [operator].

This scope excludes production rollout, high availability, load/slow-client
stress, simultaneous body downloads with mailbox mutations, JMAP mail workflows
beyond mailbox management and keyword updates/object changes and
unsupported Sieve extensions. Production identity, DNS, storage and failover gates
remain in [deployment qualification](DEPLOYMENT_QUALIFICATION.md). Any workflow
the intended users require must enter scope before recording their acceptance.
