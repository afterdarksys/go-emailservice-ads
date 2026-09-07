# Engineering roadmap — reconciled for 2.7.0

Updated 2026-09-07. [TODO](../TODO) is the authoritative current backlog;
[documentation](../docs/README.md) describes the supported runtime. Original
24-week dates and all-Planned statuses no longer describe the implemented release.
Historical phase documents retain proposed requirements for future reconciliation.

| Historical milestone | Current disposition | Remaining work |
| --- | --- | --- |
| M1 Security remediation | Reviewed mail-path/storage findings closed in CODEX_TODO.md; later platform hardening delivered | Audit any additional experimental subsystem claims before enabling them |
| M2 Protocol completeness | Persistent UID/FETCH/search, JMAP ownership/limits and mail-authentication fixes delivered | Named desktop/mobile client qualification and any separately scoped protocol extensions |
| M3 Directory/identity | Persistent shared users, recipient lookup, aliases, API OAuth and compliance grants delivered | Production provider qualification; LDAP/AD, scoped SCIM Users and SAML broker requirements implemented/reconciled |
| M4 Classic spam control | IP/DNSBL/reputation input, required Rspamd/ClamAV, Starlark, quarantine and mailstorm controls delivered | Production scanner tuning and remaining historical scoring requirements |
| M5 AI spam control | Deterministic adaptive mailstorm control exists; no trained model completion claimed | Model/feature/learning/phishing pipeline remains proposed |
| M6 Hardening/observability | Metrics/alerts/probe, backup/restore, fenced standby, TLS reporting and release qualification delivered | Production drills and shared cluster state; DMARC aggregate reporting implemented |

Production hardening and platform remediation implementation lists are complete;
external rollout remains separate. Version 2.7 adds DSN lifecycle controls,
domain-scoped evidence operations, Object Lock preservation and qualification
tools. See [CHANGELOG](../CHANGELOG.md) and
[deployment evidence](../docs/DEPLOYMENT_QUALIFICATION.md).
