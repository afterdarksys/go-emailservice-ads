# Platform completion plan

Baseline: main `91d5105`, 7 September 2026. This plan tracks implementation and
evidence separately from deployment and human acceptance.

## Commit sequence (12 planned commits)

1. Record this plan, acceptance boundaries, and desktop/mobile UAT matrix.
2. Implement account-scoped JMAP threads, Thread/get, and collapsed queries.
3. Add authenticated JMAP event streams with bounded connections and reconnect.
4. Implement and document LDAP/AD authentication requirements and integration.
5. Implement and document SAML authentication requirements and integration.
6. Implement SCIM provisioning with durable account lifecycle and authorization.
7. Implement durable DMARC aggregate reporting and reporting controls.
8. Expose operational security, DNS, and greylisting statistics on active APIs.
9. Implement validated general configuration reload with explicit restart boundaries.
10. Measure and improve storage/listing/DNS behavior; record reproducible benchmarks.
11. Implement personal-data inventory/export/deletion, preserving legal holds and
    recording external backup/log disposal obligations explicitly.
12. Run release qualification, reconcile documentation/TODOs, and audit remaining work.

Every feature commit includes relevant tests and operating/API documentation.
Changes to sequence are recorded here; total implementation remains 8–14 commits.

## Acceptance

- Protocol tests cover ownership isolation, invalid input, state synchronization,
  persistence/restart, and bounded resource use where applicable.
- Identity integrations fail closed on provider failures and disabled accounts;
  provisioning cannot grant administrative API privileges implicitly.
- Reports and deletion jobs remain recoverable after restart. External report
  destinations are verified before mail is sent. Legal holds are not bypassed.
- Reload validation is transactional: unsupported changes are rejected clearly;
  existing configuration remains usable after rejection.
- Performance claims include baseline, workload, command, and measured results.
- Final candidate passes the repository release qualification and race checks.
- Desktop/mobile acceptance requires named clients, versions, environment, results,
  and an identified accepting operator. Automated protocol evidence does not count
  as a human sign-off or as a production-provider certification.

## UAT matrix

Pending client/environment selection from the operator. For each desktop and
mobile client record version, OS, transport (IMAP/SMTP or JMAP), timestamp, tester,
candidate revision, and pass/fail evidence for:

1. Login, invalid credentials, disabled account, TLS validation.
2. Receive, send, reply, reply-all, forwarding, attachments and inline images.
3. Folder creation/move/delete, flags, unread counts and multi-client sync.
4. Thread grouping, collapsed results, new-mail push and reconnect (JMAP clients).
5. Offline changes/reconnect, delayed send/cancel where client supports them.
6. Account isolation, logout/revocation, restart and restored mailbox visibility.

Sign-off: **pending**. Record defects and retest evidence; do not replace missing
client tests with a blanket acceptance statement.

## Remaining-scope audit

Review active TODOs, incomplete artifacts, open PRs, working-tree changes, and
historical feature claims. Report production integrations and separately scoped
features still outstanding; do not mark them complete merely because this plan
finishes. Preserve an explicit evidence record for anything requiring external
credentials, a real device, or an operator decision.
