# Administration — 2.7.0

The supported deployment has one writer per spool, optionally with a separate
perimeter forwarding to an internal hub. Use a persistent data volume and a
service account that owns it. Read [configuration](CONFIGURATION.md) before
starting a new instance and [deployment gates](DEPLOYMENT_QUALIFICATION.md)
before routing production traffic.

## Build and start

From the repository root, build the service and operational utilities:

```sh
go build -o bin/goemailservices ./cmd/goemailservices
go build -o bin/adsemailadm ./cmd/adsemailadm
go build -o bin/gemsads-conf ./cmd/gemsads-conf
go build -o bin/mailhub-backup ./cmd/mailhub-backup
go build -o bin/mailhub-failover ./cmd/mailhub-failover
go build -o bin/mailhub-log ./cmd/mailhub-log
go build -o bin/mailhub-preserve ./cmd/mailhub-preserve
go build -o bin/mailhub-authcheck ./cmd/mailhub-authcheck
./bin/goemailservices --version
./bin/goemailservices --config /etc/mailhub/config.yaml
```

Run through your service supervisor in production. Set its working directory,
secret environment and volume mounts explicitly. Relative config paths resolve
from the working directory. Run `./bin/goemailservices --check-config --config /etc/mailhub/config.yaml`
before restarting. It returns zero for valid configuration and nonzero for invalid
or missing files, without creating defaults, opening listeners or touching the
spool. It validates configuration rules and logging level, not runtime dependency
health. Then test a candidate configuration in an isolated instance with a
separate data directory and blocked production egress. The repository's root config is a development example with
placeholder credentials and optional features, not a deployable production file.

Check HTTPS `/health`, `/ready`, and `/api/v1/version`, then exercise a dedicated
authenticated SMTP-to-IMAP test account. `/health` can succeed while `/ready`
returns 503. Use the [mailflow probe](MONITORING.md) for ongoing checks.

## Routine management

Use `adsemailadm` or [REST requests](API_REFERENCE.md) with a named, scoped operator key.
See the [CLI reference](CLI_ADMINISTRATION.md) for `gemsads-conf`, database
maintenance, queue commands and explicit Postfix compatibility boundaries.
`mailctl` retains legacy commands outside the active server.

1. List `/api/v1/mailboxes`; create an account using POST with username, password
   and email. New accounts persist immediately and can authenticate over TLS.
2. Disable a compromised account with PUT `/api/v1/mailboxes/{username}` and
   `{"enabled":false}`. Reset its password separately; a reset preserves disabled
   status. Re-enable explicitly after resolving the incident.
3. Edit `platform.aliases` in the deployed YAML for aliases and restart in a
   controlled window. Test `/api/v1/recipients/{address}` afterward.
4. Inspect `/api/v1/queue/stats`, `/queue/pending` and `/dlq/list` under `/api/v1`.
   Use the exact `id` returned by the API for message operations.
5. Investigate the stored error before POST `/api/v1/dlq/retry/{id}`. Retry only
   messages whose underlying cause is resolved. Record intentional deletion
   before DELETE `/api/v1/message/{id}`; this discards queued mail.

Deleting a mailbox account removes the identity; it is not a complete mailbox
content purge or compliance disposal operation. Plan content retention separately.
Queue listings can contain message bodies and recipient metadata; restrict
operator access and avoid copying responses into public incident reports.

## Pause a runaway sender

POST `/api/v1/mailstorm/pause` with:

```json
{"key":"user:billing-app","reason":"Runaway job; incident INC-123","duration":"30m"}
```

Use `ip:192.0.2.10` for an unauthenticated sender or `*` for an instance-wide
pause. New admissions defer; queued dispatch pauses without consuming retries.
Already running deliveries may complete. GET `/api/v1/mailstorm` confirms state.
Fix the sending application, then POST `/api/v1/mailstorm/resume` with the key.
A pause has an expiry; extend it if needed. Stop the process for maintenance that
requires exclusive storage access; a mailstorm pause does not release spool locks.

## Policy, quarantine and evidence review

Manage inline Starlark rules with the policy API. Save persists before activation;
use a disabled rule for staged testing and enable after reviewing test results.
POST `/api/v1/policies/{name}/test` evaluates synthetic content without queuing it.
The writable `data_dir/policies.yaml` becomes authoritative after bootstrap.

List `/api/v1/quarantine` for scanner-held mail. Release rescans and requires a
clean result; resolve scanner outages before retrying. Deletion removes a held
message. Compliance evidence uses separate APIs, domain grants, legal holds and
retention rules. A compliance release preserves evidence and creates a delivery
transaction. Follow [compliance operations](COMPLIANCE_OPERATIONS.md); generic
queue controls cannot manage compliance evidence.

## Stop, upgrade and recover

1. Remove the instance from ingress routing and stop the service through its
   supervisor (SIGTERM). Wait for process exit before copying storage.
2. Create and verify a complete [offline backup](BACKUP_RECOVERY.md). Record the
   binary version and separate configuration, keys and external identity backups.
3. Install the qualified binary/container and reviewed configuration. Preserve
   service-account ownership and the entire data mount.
4. Start one owner. Check readiness, known mailbox contents, pending queue state,
   TLS and a synthetic delivery before restoring ingress traffic.
5. For rollback from 2.7, restore a pre-upgrade backup with the corresponding
   older binary. A 2.7 journal contains transaction records older readers cannot
   safely consume. Do not downgrade a binary against the upgraded journal.

For host loss, use the [fenced standby procedure](FAILOVER.md). The REST
replication endpoints return 501 in the standard executable; they are not an
activation mechanism. Do not delete lock files or start a second owner to bypass
an ownership failure.

## Scheduled operations

Daily: inspect queue age, free space, failed/held mail, dependency health and probe
results. Verify backups and independent audit checkpoints reach off-host storage.
Periodically: rotate credentials/certificates, review access grants, test alert
routing and restore a backup into an isolated environment. Measure recovery time
and run provider fencing drills. Retain original audit JSONL for verification;
`mailhub-log` conversions are for analysis, not replacement of original evidence.
