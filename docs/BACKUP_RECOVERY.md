# Consistent backup and recovery

Build `go build -o mailhub-backup ./cmd/mailhub-backup`. Stop the mailhub before
creating a snapshot. The command acquires the same spool lock as the server and
refuses to copy an active spool. The complete data directory includes SQLite
databases and WAL files, journals, circuit state and reports.

```
mailhub-backup create --data-dir /data --archive /mnt/offhost/mailhub-20260906.tar.gz
mailhub-backup verify --archive /mnt/offhost/mailhub-20260906.tar.gz
mailhub-backup restore --archive /mnt/offhost/mailhub-20260906.tar.gz --data-dir /data-restored
```

Every file has a SHA-256 manifest entry. Verification checks archive integrity and
SQLite integrity. Restore validates in a temporary directory, rejects path
traversal, links, duplicate entries, oversized archives and existing destinations,
then installs the directory. Failed validation leaves the destination untouched.
`--max-bytes` bounds extraction (default 1 TiB); size it to the expected snapshot.

Configuration, TLS/DKIM keys and external PostgreSQL identities must be backed up
separately while the hub is stopped. A data-directory snapshot cannot make an
external database consistent. Record those backup identifiers with the snapshot.
Protect archives as sensitive mail, preferably on encrypted off-host storage with
immutable retention. Keep daily copies for 30 days and selected monthly copies
according to organizational policy; prune only after a newer copy passes verify.

Run a monthly restore drill into a disposable destination. Start an isolated hub
against it, confirm known mailbox content and queue IDs, and record elapsed
recovery time. Never start restored pending queues with production egress enabled
during a drill. The package's `TestOfflineBackupRestoreDrill` automates storage
round-trip and corruption checks; deployment drills exercise credentials and DNS.

Recovery point is the last completed snapshot. Recovery time includes restoring
the archive, provisioning secrets and verifying the hub. Measure these before
promising an availability target.
