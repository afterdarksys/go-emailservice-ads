# Sieve delivery workflows

Per-recipient Sieve supports keep, discard, reject, fileinto, redirect, stop,
flags/variables, copy and vacation. This completes the previously documented
redirect/vacation/multiple-action gaps; it does not claim every Sieve extension.
Multi-action and vacation scripts belong in recipient scripts. Global SMTP
policies reject those plans explicitly rather than silently executing one action.

## Install and change a script

Write `platform.data_dir/sieve/<escaped-username>.sieve`, where the filename uses
Go URL PathEscape (for example `qa@mail.test.sieve`). Use service-account ownership
and mode 0600. Validate the program through the existing Sieve policy test tooling,
then replace the file atomically. No restart is needed. Invalid/unreadable new
scripts defer delivery; they are never silently bypassed.

The first successful evaluation is journaled before applying actions. Retries of
that incoming message/user resume the saved plan, even if the script changes or
is removed. Script edits apply to new messages. Do not delete journal checkpoints
to force re-evaluation: that can repeat forwarding or mailbox delivery. Back up
the complete data directory before upgrades and include scripts and journal data
in restore/retention planning.

```sieve
require ["fileinto", "imap4flags", "copy", "vacation"];
if header :contains "Subject" "project" {
    addflag "\\Seen";
    fileinto :copy "Projects";
    redirect :copy "backup@example.test";
}
vacation :days 7 :subject "Away" "I will reply when I return.";
```

## Delivery actions

A script permits up to 32 executed actions. Each keep/fileinto captures the flags
at that point. Copies into the same folder are combined, unioning flags; different
folders receive independent copies. `:copy` on fileinto or redirect preserves
implicit keep. Without `:copy`, either action cancels implicit keep. An explicit
keep still requests local delivery. discard cancels implicit keep but does not
erase earlier explicit deliveries. reject conflicts with previously requested
deliveries. stop ends evaluation while preserving the accumulated plan.

Each mailbox copy uses a durable delivery key. Redirect acceptance and its
checkpoint share a journal transaction, so retries after child delivery do not
send again. Multiple actions can complete independently: if a later action fails,
the source remains retryable and earlier successful actions are skipped on retry.
This is not one transaction across every destination.

Redirect preserves the original envelope sender, removes Return-Path/Bcc headers,
adds a Received hop, and uses the normal durable queue, routing, compliance and
delivery machinery. Ten existing Received hops stop forwarding. A quarantined
source produces a held child instead of forwarding it. Source content has already
passed admission; redirects are not a new SMTP authentication session. Delivery
failures follow normal retry/DSN handling.

## Vacation replies

Declare vacation and use `vacation [tags] "reason";`. Supported tags are :days,
:subject, :from, :addresses, :mime and :handle. Days default to 7 and are clamped
to 1–365. At most one vacation action may execute. Vacation does not cancel
implicit keep. The default subject is "Auto: away". Plain reasons are MIME encoded;
:mime accepts a MIME fragment with content headers, not routing headers.

The visible From must be the account's primary identity (:from may repeat it).
The response uses an empty envelope sender and Auto-Submitted: auto-replied.
Replies target the incoming envelope sender. No response is generated for null
senders, bounces, quarantined mail, Auto-Submitted values other than no, List-*
headers, bulk/list/junk precedence, recognized system senders, or self-mail.
The account/delivery address (or a configured :addresses value) must appear in
To or Cc; Bcc-only delivery does not trigger a response.

Suppression is scoped to account, sender and response identity. :handle supplies
that identity; otherwise the subject/from/MIME/reason combination does. A queued
reply, its per-message checkpoint and interval checkpoint commit together.
Restart, compaction and backup/restore preserve suppression. Each new message
still gets its own normal mailbox delivery during the suppression interval.

## Troubleshoot and operate

- No forward: inspect source retry/DLQ state, invalid destination, hop limit,
  quarantine/compliance holds, routing and disk/spool limits.
- No vacation reply: check sender/envelope, personal To/Cc, automatic/list headers,
  quarantine, :from ownership and the existing suppression interval.
- A script edit appears ignored: the incoming message already has a saved plan;
  send a new synthetic message to test the replacement.
- Partial local copies: the remaining destination may have failed quota, folder
  validation or storage. Fix that failure; retry resumes without repeating prior
  copies or accepted forwards.
- History growth: Sieve plans and completed-action records are durable journal
  metadata. They are excluded from pending-message quota, but consume disk and
  backup capacity. Opt-in platform.sieve_retention_days expires completed journal
  plans/checkpoints hourly while protecting live sources, unexpired intervals and
  legacy records without provenance (see CONFIGURATION.md). Retained
  plans may contain addresses, folder names and vacation text; include them in
  deployment retention/access controls. Never remove live retry checkpoints.

Automated evidence is in the policy/SMTP/storage regression tests and
`scripts/sieve-workflows.py`, invoked by the release gate. Actual client UAT and
production routing/scanner qualification remain separate.

References: [Sieve copy](https://www.rfc-editor.org/rfc/rfc3894.html) and
[Sieve vacation](https://www.rfc-editor.org/rfc/rfc5230.html).
