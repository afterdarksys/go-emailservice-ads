# Mailhub documentation — 2.7.0

Start here for the supported single-owner mail platform. These guides describe
the active `cmd/goemailservices` executable as reviewed on 2026-09-06.

| Task | Guide |
| --- | --- |
| Start, stop, upgrade, manage users and queues | [Administration](ADMINISTRATION.md) |
| Configure listeners, storage, authentication and filtering | [Configuration](CONFIGURATION.md) |
| Diagnose admission, delivery, TLS and API failures | [Troubleshooting](TROUBLESHOOTING.md) |
| Integrate with the management REST API | [API reference](API_REFERENCE.md), [OpenAPI contract](openapi.json) and [authentication](../API_AUTHENTICATION.md) |
| Choose topology and understand the mail path | [Platform operations](PLATFORM_OPERATIONS.md) |
| Manage bounce, evidence and legal holds | [Compliance operations](COMPLIANCE_OPERATIONS.md) |
| Back up, restore and activate a standby | [Backup/recovery](BACKUP_RECOVERY.md) and [failover](FAILOVER.md) |
| Monitor and rotate credentials | [Monitoring](MONITORING.md) and [rotation](CREDENTIAL_ROTATION.md) |
| Qualify a release and production dependencies | [Release checks](RELEASE_QUALIFICATION.md) and [deployment gates](DEPLOYMENT_QUALIFICATION.md) |
| Write policies and configure IP admission | [Starlark filters](STARLARK_FILTERS.md) and [IP filtering](IP_FILTERING.md) |
| Execute QA/UAT and configure Sieve | [QA/UAT guide](QA_UAT.md) and [Sieve flags](SIEVE_FLAGS.md) |
| Integrate the optional JMAP read subset | [JMAP API](JMAP_API.md) |
| See outstanding work | [TODO](../TODO) |

The REST API reference takes precedence over older CLI/router examples. Source
packages for experimental administration, cluster coordination or protocols do
not by themselves establish support in the main executable. Historical design
plans under `.planning/phases` remain proposals unless reconciled in TODO.
