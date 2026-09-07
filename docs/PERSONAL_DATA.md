# Personal-data export and deletion

Build `go build ./cmd/mailhub-privacy`, or use the packaged
`/usr/local/bin/mailhub-privacy` container entrypoint with the stopped service
data/configuration volumes mounted. Operations are offline and acquire the
same exclusive spool lock as mailhub; stop the service first. Use the deployment
configuration so the correct mailbox and user databases are selected. Export
files and deletion cases are created with mode 0600 and must not already exist.
No remote deletion endpoint is exposed: these operations require host access and
an approved maintenance window.

```sh
mailhub-privacy --operation inventory --config /etc/mailhub/config.yaml --account alice@example.net
mailhub-privacy --operation export --config /etc/mailhub/config.yaml --account alice@example.net --output /secure/exports/alice.zip
mailhub-privacy --operation delete --config /etc/mailhub/config.yaml --account alice@example.net --output /secure/cases/alice-delete.json --reason 'Approved request CASE-123'
```

Inventory/export covers account identity (excluding password hashes), domain
entitlements/quota, owned mailbox payloads, flags/folders/sync history, JMAP
uploads/query snapshots/submission receipts, owned Sieve plans/effects and script,
related queue/quarantine/compliance records, and matching local audit records.
The ZIP includes JSON inventory, RFC 5322 payload files, and a SHA-256 manifest.
Shared related records are identified separately; exports are administrator-held
review packages and must be reviewed before disclosing another person's data.
They are not automatically delivered to the requester. Memory use scales with
account inventory; allow adequate memory for large accounts.

Before deletion disable the account, remove it from `auth.default_users`, and
stop external provisioning/recreation. The tool refuses enabled or bootstrap
accounts. It removes owned active payloads/checkpoints, all account mailbox
metadata/uploads/snapshots, and the personal Sieve script, compacts the journal,
and deletes the local identity/entitlements. Shared queue deliveries keep other
recipients but remove the deleted account from retry recipients. SQLite uses
secure-delete, checkpoint and vacuum. PostgreSQL physical cleanup remains an
operator task. Other users' mailbox copies are retained.

All compliance records are retained for independent disposition, including legal
holds and indefinite/unexpired retention. The tool does not bypass compliance
controls. Use the existing audited compliance export/delete controls after the
applicable hold and retention decisions. Legacy unowned hashed Sieve markers
cannot reliably be attributed and are listed as a review obligation; new markers
carry account ownership.

A deletion case is atomically checkpointed through local phases and ends with
`local_deletion_complete_external_disposition_required`. On interruption keep the
service stopped, inspect the case, and rerun with a new case output referring to
the original request. Operations tolerate records already removed; the disabled
identity is removed last. After a user has been deleted, inventory can still
inspect username-owned remnants, but a former alternate email must be checked
against the original export/case record.

Completion requires disposition of **every** remaining obligation: external
LDAP/SCIM/SSO identities, shared mail and retained evidence, external logs/search
and scanner records, backups/replicas/object locks, aliases/policies, audit chains,
exports and the case itself. Assign owner, disposition/expiry, and evidence for
each. Local erasure is not proof that remote systems or backup media were erased.
Filesystem snapshots, SSD remnants and recovery keys require storage-layer
handling. Reapply deletion after restoring an older backup before serving clients.

The tool's final status intentionally does not say that all personal data has
been erased. Final request closure requires the operator's verified disposition
record. Protect exported archives, use encrypted storage, and delete them on the
approved schedule.
