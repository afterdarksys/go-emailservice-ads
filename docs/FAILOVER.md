# Fenced active/passive recovery

For the replicated-volume integration, see [HIGH_AVAILABILITY.md](HIGH_AVAILABILITY.md).

This is cold-standby activation from verified storage, not active-active
replication. Stage a verified backup using mailhub-backup before activation.
Keep standby SMTP listeners and outbound delivery stopped during staging.

Configure both possible owners with the same `platform.fencing_lease_file` on
storage providing reliable cross-host POSIX locks, for example
`/shared/mailhub/owner.lock`. The process holds this lease until exit in addition
to its spool lock. An unavailable lease prevents startup. Do not use unrelated
local paths on two hosts and assume they coordinate ownership.

Build `go build -o mailhub-failover ./cmd/mailhub-failover`. Supply a JSON plan:

```json
{
  "lease_file": "/shared/mailhub/owner.lock",
  "audit_file": "/shared/mailhub/activation.jsonl",
  "fence": ["/usr/local/libexec/mailhub/fence-active"],
  "verify_fenced": ["/usr/local/libexec/mailhub/verify-active-fenced"],
  "start": ["/usr/bin/systemctl", "start", "mailhub-standby"],
  "ready": ["/usr/local/libexec/mailhub/wait-standby-ready"],
  "stop": ["/usr/bin/systemctl", "stop", "mailhub-standby"]
}
```

Run `mailhub-failover --plan /etc/mailhub/activation.json --timeout 5m` only for an
authorized failover. The fencing programs must use your provider's power/storage
fencing API; verification must independently confirm the old owner cannot write
or send mail. A failed ping is not fencing. These provider-specific executables
are prerequisites, not permissive defaults supplied by this repository.

The controller serializes activation attempts, records durable audit steps,
requires successful fencing and verification, checks lease availability, starts
the standby, and verifies readiness. Failed startup/readiness invokes the stop
program. Readiness verification should check HTTPS `/ready` with certificate
validation and perform an isolated local delivery probe. Route production traffic
only after success. Test the provider hooks using disposable hosts first.

Record snapshot completion time and actual data loss before activation. RPO is
the age of the last successfully staged snapshot. RTO is measured from incident
declaration through fencing, restore, startup, checks and traffic switch. A
reasonable initial drill objective is RPO <=24h and RTO <=60m; these are planning
targets, not guarantees. If unacceptable, introduce replicated durable storage
and continuously tested provider fencing before promising tighter targets.

Tests cover lease exclusion, failed-fence/verification refusal and stopping a
standby whose readiness check fails. Actual power fencing and traffic switching
must be qualified in your infrastructure. Never run two owners to test recovery.
