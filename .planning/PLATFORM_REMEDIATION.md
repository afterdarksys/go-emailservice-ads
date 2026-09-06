# Platform remediation

Scope: functional internal hub and optional perimeter role, with layered defenses.

- [x] Durable configurable storage, exclusive queue ownership, deployment volumes and recovery checks
- [x] Shared SMTP/IMAP/API identity and recipient validation (aliases/disabled accounts)
- [x] Explicit next-hop transport, TLS/authentication, failover and loop prevention
- [x] Distinct SMTP listener roles and trusted original sender metadata
- [x] Required policy enforcement and real reputation input
- [x] Content scanner integration with bounded failure handling
- [x] Account/application/domain outbound limits
- [x] Mailbox/spool quotas and operational readiness
- [x] API permission enforcement
- [x] Managed quarantine with audited release/retention/rescanning
- [x] MTA-STS, TLS reporting and ARC integration
- [x] Targeted tests, build, deployment documentation and limitations

Validation: full Go suite passed; race-enabled storage/policy/SMTP/filtering/API/config/delivery/security/mailstorm suites passed; live ClamAV clean/EICAR tests passed; Rspamd configuration syntax and EICAR rejection verified. New tests cover queue pauses, disabled accounts, recipient validation, missing ARC sealing, and required malware scanner outage.

Mailstorm additions: deterministic adaptive baselines, burst/fan-out/duplicate limits, persistent escalating circuit breakers, queued-message pause without retry consumption, scoped operator API and audit records.

Deployment boundaries: one owner per spool; no active-active replication. ARC delegates to Rspamd and requires signing keys/DNS plus staging interoperability verification. Production rollout, external DNS, certificates and secret provisioning were not performed. See docs/PLATFORM_OPERATIONS.md for migration and operational limits.

Live validation exposed a Rspamd ClamAV timeout path without a failure symbol. Required direct ClamAV INSTREAM admission and release checks now require an explicit clean result independently of the Rspamd response.
