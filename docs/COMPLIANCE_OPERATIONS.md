# Bounce, compliance and logging operations

The active YAML configuration file is authoritative. Built-in defaults apply
to omitted settings. Use an environment-specific configuration file via
`--config`; existing documented environment overrides (such as MAILHUB_DATA_DIR)
still apply. OAuth client secrets use a named environment reference. Changing
these settings requires a controlled restart. APIs expose effective settings
but do not rewrite them. See `examples/compliance-config.yaml`.

## Bounce management

`platform.bounce` exposes `suppress`, `postmaster`, `include_original_headers`,
`max_header_bytes`, `max_attempts`, `initial_delay`, and `max_delay`.
Defaults are notifications enabled, the existing postmaster identity, original
headers included with a 1024-byte budget, five attempts, a one-minute initial
delay and four-hour maximum delay. Backoff doubles between attempts.
`GET /api/v1/bounce/config` requires `bounce:read` and returns effective values
(duration values in API JSON are nanoseconds; YAML accepts units such as `5m`).

Permanent remote failures generate multipart delivery-status notifications to
the envelope sender. Exhausted temporary retries now attempt a final DSN before
moving to the failed queue. Null senders and DSNs do not generate another bounce.
Explicit suppression is audited. Invalid recipients should still be rejected
during SMTP rather than accepted and bounced to potentially forged senders.
`enable_dsn` advertises SMTP DSN support and captures ENVID, RET and per-recipient
NOTIFY/ORCPT preferences. `delay_warning_after` enables one durable delay notice
per recipient after that age; zero disables it. SUCCESS requests produce a local
`delivered` or remote `relayed` notice. Delay/success notification checkpoints and
outbox messages commit together. Failure DSN submission remains at least once:
a crash between notification submission and source completion can duplicate it.

`full_return_max_bytes` bounds RET=FULL content; zero keeps reports header-only.
`track_incoming` stores parsed reports in `/api/v1/bounce/reports` (`bounce:read`).
These reports are explicitly untrusted and never automatically suppress a
recipient. Manage `suppressed_recipients` in the configuration file; suppression
is enforced at recipient admission and queued dispatch. Report metadata remains
until an operator disposes of it through queue controls; choose a retention
schedule before enabling tracking. RFC references:
https://www.rfc-editor.org/rfc/rfc3461.html
https://www.rfc-editor.org/rfc/rfc3464.html


## Compliance engine and queues

Rules match exact envelope domains, case-insensitively, with `match` set to
`sender`, `recipient`, or `either` (default). No content headers grant access or
bypass a rule. Modes are:

- `hold`: preserve the whole message and suspend normal delivery.
- `reroute`: intercept into the compliance queue for review, using the same
  preservation and explicit-release workflow as hold.
- `copy`: preserve a monitoring copy while original delivery continues.
- `bcc`: add a configured envelope recipient without adding a Bcc header.

Holds take precedence over BCC. When multiple domain holds match, one case
combines them, uses the longest retention, and preserves any legal hold.
Zero retention means indefinite retention. A mixed-domain envelope is held as
a whole; it is not partially delivered. Newly configured holds also apply when
existing queued mail reaches dispatch. A reviewed release is explicitly marked
to avoid being captured again by the same configured hold.

Captures are stored durably before acknowledging acceptance or allowing normal
delivery. A capture failure defers delivery. Capture is at least once and can
preserve attempted transactions whose later submission failed. Compliance
storage consumes spool quota and is included in full data-directory backups.
Automatic quarantine expiration does not expire compliance evidence.

List/config access uses `compliance:read`. Export, release, deletion and hold
changes require `compliance:export`, `compliance:release`, `compliance:delete`,
and `compliance:legal-hold`, respectively:

- `GET /api/v1/compliance/config`: inspect configured rules.
- `GET /api/v1/compliance?domain=example.test`: list case metadata.
- `GET /api/v1/compliance/{id}/export`: audited RFC822 evidence export.
- `POST /api/v1/compliance/{id}/legal-hold`: `{"hold":false,"reason":"Case closed"}`.
- `POST /api/v1/compliance/{id}/release`: `{"reason":"Approved delivery"}`.
- `POST /api/v1/compliance/{id}/delete`: `{"reason":"Retention approved for disposal"}`.

Release preserves the evidence and creates a new delivery transaction. It cannot
release monitoring copies or active legal holds. Malware quarantine flags remain
in force. Deletion requires expired, finite retention and no legal hold; removal
from the active index is followed by physical journal cleanup at compaction.
Backups and exported copies have separate disposal obligations.

Release now writes the evidence decision and delivery outbox in one journal
transaction. A crash applies both or neither; compaction cannot recreate a
completed delivery. SMTP delivery itself still has the usual remote-acknowledgment
ambiguity. Legacy 2.6 records already stuck in `compliance_releasing` require
manual reconciliation; the new transaction format cannot reconstruct facts that
were not preserved by the old workflow.

Enable `platform.compliance.enforce_domain_access` and configure `access` grants
with principal, domains and actions. OAuth principals use `oauth:<subject>`; API
keys use their configured name. Every domain in a combined case must be allowed.
Listing hides unauthorized records; item access returns 404. Explicit `*` grants
are supported for designated administrators. Domain enforcement defaults off for
upgrade compatibility; enable it before delegating officer access.


## OAuth access protection

Set `api.oauth.enabled` and `require_for_compliance` to true to prevent API keys
from bypassing OAuth on compliance endpoints. Configure HTTPS on the API itself;
forwarded headers do not establish TLS trust. `compliance_only` limits OAuth use
to these endpoints; false allows access-token scopes on the other APIs as well.

The configured RFC 7662 introspection endpoint must return an active token with
matching issuer and audience, an unexpired expiry, subject, and the exact API
scope. Client credentials authenticate introspection, redirects are disabled,
and timeouts fail authorization. Tokens are not logged or cached. This is
resource-server access-token protection, not a browser login implementation;
configure MFA and authorization-code/PKCE login at your identity provider/client.
Reference: https://www.rfc-editor.org/rfc/rfc7662.html

## Logging and evidence integrity

`logging.level` and `logging.format` select operational output. JSON is the new
default; YAML emits document separators and syslog emits RFC 5424 envelopes.
All formats write to stdout for the configured collector. Configure retention,
rotation, secure transport, access control and replication in that collector.
Operational delivery logs can contain addresses; restrict their readers.

Security-sensitive operations append actor, action, record ID, UTC timestamp,
sequence and SHA-256 chain fields to `mail-storage/audit-chain.jsonl`, with fsync.
Capture records also bind the preserved envelope and exact message bytes with
an evidence hash. Release and export verify that hash before proceeding.
Failure prevents protected actions. Startup verifies the existing chain.
Existing `audit.jsonl` remains an unchanged legacy artifact. Audit records are
not automatically deleted; they contain no message bodies or bearer tokens.
Avoid putting personal information or secrets in operator-entered reasons.

`mailhub-log --file audit-chain.jsonl --verify-audit` verifies the chain and emits
its sequence/head. Independently preserve that head in an immutable external
repository. Supply it later with `--expected-head` to detect truncation or a
rewritten local chain. Local hashes alone cannot defeat an administrator who
can replace the entire log and its checkpoint. The implementation is not WORM
storage, regulatory certification, a trusted timestamp authority, or a digital
signature system. Use restricted accounts, synchronized clocks, immutable
off-host retention, tested exports and a documented retention schedule.

NIST SP 800-92 guides operational log management:
https://csrc.nist.gov/pubs/sp/800/92/final
For broker-dealers, SEC recordkeeping rules have specific preservation and
record-reconstruction requirements that a local hash chain alone does not meet:
https://www.sec.gov/investment/amendments-electronic-recordkeeping-requirements-broker-dealers
Select the actual jurisdiction, record classes and retention periods with the
organization's compliance owner before claiming adherence to a legal standard.

## Log utility

Build `go build ./cmd/mailhub-log` or use the packaged command.
`mailhub-log --config examples/mailhub-log.yaml --file service.log` detects JSON
objects/arrays/JSONL, YAML mapping documents/sequences, or RFC 5424/3164 syslog.
It converts to JSON, YAML or syslog with `--output`. Unknown text and malformed
records fail explicitly. RFC 3164 timestamps retain the missing-year ambiguity.
Syslog export embeds normalized fields as JSON in the message; conversion does
not recreate byte-identical original files. Verify audit chains from original
JSONL, not converted output. Detection statistics go to stderr, data to stdout.

All utility settings are available in its YAML config; explicit command flags
override that file, which overrides defaults. Input is bounded to 64 MiB by
default and may be raised up to 1 GiB. Split larger files before conversion.

## Release validation

See `docs/DEPLOYMENT_QUALIFICATION.md` for version 2.7 commands and the distinction
between isolated qualification and production deployment gates.
