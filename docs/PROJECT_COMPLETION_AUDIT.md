# Project completion audit — 7 September 2026

Scope: active `cmd/goemailservices`, shared identity/mail stores, operational APIs,
protocol qualification, current TODO, historical roadmap, and pending PRs.
The knowledge graph was consulted but predates this branch, so source and tests
were used for the actual implementation audit. No open PRs existed when checked.

## Delivered in this series

Thread grouping/collapsing and direct SSE push; provisioned LDAP/AD authentication;
SAML-broker token requirements with audience enforcement; durable scoped SCIM
Users lifecycle; DMARC aggregate XML reporting; active security/DNS/greylisting
statistics; graceful general configuration reload; measured storage/listing/DNS
optimizations; offline privacy export/deletion and disposition records. Relevant
unit, API, persistence and race checks accompany the features. The full release
gate must pass for the final candidate; its live-stage JSON is not a substitute
for the preceding package/race/build checks.

The series follows the 12-commit plan plus two review-fix commits, within the
requested 8–14 commits. Review fixes serialize account lifecycle writes, protect
replacement accounts from stale SCIM deletes, preserve late DMARC observations,
and invalidate old thread-related synchronization state once on upgrade. Identity exports include persisted timestamps, metadata and entitlement notes. The
privacy tool is packaged in the container and checked by the container CI job.

## Work that is still incomplete

The TODO retains **15 open items**. It is not accurate to claim this project has
no incomplete work:

- Desktop/mobile UAT and accepting operator sign-off: no client/environment or
  acceptance results have been supplied. Automated protocol tests do not close it.
- Eleven production qualification/operations items: actual OAuth provider;
  Object Lock; provider fencing/RPO/RTO; production DNS/TLS/keys; monitor-to-enforce
  rollout; scanner budgets/outages; production directory/aliases; monitoring and
  alert exercises; representative load/failure drills; existing-install permission
  migration; retention/encryption/key-recovery/disposal schedules.
- Three separately scoped feature decisions: replicated/active-active ownership;
  trained AI/phishing/learning and shared reputation; optional gRPC/webhooks/plugins,
  web administration and advanced routing extensions.

For a real privacy request, local deletion still requires external log/backup/
IdP/shared-evidence disposition before closure. For identity integration, real
LDAP/AD/SAML broker/SCIM provider qualification is still deployment work. The SAML
boundary is a broker, not a newly claimed native browser assertion consumer.
SCIM is the documented Users profile; group/bulk/full-directory support is not
claimed. Performance figures are microbenchmarks, not production load targets.

## Historical and inactive artifacts

The unfinished OAuth web-session prototype was moved from `internal/api` to
`docs/archive/oauth-web-session-prototype.txt`. It is historical design material,
not a registered route or build input. The active REST server is `server.go`;
legacy `router.go` managers do not define the supported API.

Historical feature inventories (`FEATURES_IMPLEMENTED.md`, `DEPLOYED_FEATURES.md`,
`SECURITY_FEATURES.md`, `POSTFIX_FEATURES.md`, and experimental phase guides) are
not release acceptance evidence. Refer to TODO and docs/README.md for supported
runtime boundaries. The old master-daemon reload loop is not the new main-server
reload controller. Source TODOs for saslauthd, correlated PTR policy, replication,
and experimental managers remain proposals outside the supported paths.

No pending PR was left unmerged at audit time. This feature branch itself must be
reviewed and its required checks completed before merging. Human sign-off is
still pending; no UAT or production certification has been fabricated.

## Qualification record

The clean 12th-commit candidate `397d5032c4be11a4c5862f3dd3b42014dbc35823`
passed the complete local release gate on 7 September 2026. Its live stage ran
17:17:06–17:18:21 UTC, including reload, thread/push and restored-service checks.
Report: `/tmp/mailhub-completion-candidate.json`; log:
`/tmp/mailhub-completion-candidate.log`. Review fixes require a fresh final run;
use `/tmp/mailhub-completion-final14.json` and `/tmp/mailhub-completion-final14.log`
and verify their revision/clean-tree fields against the final commit. CI status
is authoritative for the eventual PR. Desktop/mobile UAT remains not performed.

## Extensions and HA follow-up

The follow-up implements TLS gRPC transport, a web administration console,
durable signed management webhooks, external admission plugins, advanced
transport selectors and DRBD replicated-volume ownership checks. See
EXTENSIONS_ADMINISTRATION.md, HIGH_AVAILABILITY.md and CONFIGURATION_SAFETY.md.
The current TODO has **14 open items**: eleven production qualification tasks,
desktop/mobile sign-off, active-active ownership and advanced filtering scope.
Earlier counts above describe the preceding series. Active/passive requires
actual replicated devices, quorum witness and provider fencing qualification;
active-active ownership has not been claimed. No real cluster or human client
sign-off has been performed. The final follow-up release gate and PR checks must
pass before merge; use their exact revision when recording qualification.
