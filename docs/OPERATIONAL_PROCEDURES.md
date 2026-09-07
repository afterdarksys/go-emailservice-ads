# Operational procedures — 2.8.0

The maintained runbooks are:

- [Administration](ADMINISTRATION.md): startup, accounts, queues, policies, maintenance and upgrades.
- [Configuration](CONFIGURATION.md): active settings, secrets, TLS and restart behavior.
- [Troubleshooting](TROUBLESHOOTING.md): SMTP, IMAP, API, scanner, storage and delivery diagnosis.
- [Backup and recovery](BACKUP_RECOVERY.md): stop the owner, create/verify an archive and restore to a new destination.
- [Fenced failover](FAILOVER.md): provider-specific fencing and standby activation.
- [Monitoring](MONITORING.md): operational metrics, alerts and synthetic mail probe.
- [Compliance operations](COMPLIANCE_OPERATIONS.md): evidence, legal holds and retention.
- [Deployment qualification](DEPLOYMENT_QUALIFICATION.md): production gates and recorded local evidence.

These replace the earlier procedures on this page. Do not copy a running spool
as a consistent backup or use REST replication promotion for the standard binary.
