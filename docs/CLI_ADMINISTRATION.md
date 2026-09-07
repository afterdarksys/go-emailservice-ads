# Administration command reference

`adsemailadm` is the supported administration CLI. `gemsads-conf` manages local
configuration documents and SQLite maintenance. Both are packaged in the container.
Build locally with:

```sh
go build -o bin/adsemailadm ./cmd/adsemailadm
go build -o bin/gemsads-conf ./cmd/gemsads-conf
```

Run local file commands from the service's working directory, with its environment
and filesystem identity. Relative paths in configuration use that working directory.
Use `--help` on any command. No command can manage every possible `.db` format or
repair arbitrary lost/corrupted data.

## Tool inventory

| Tool | Purpose |
| --- | --- |
| `adsemailadm` | Scoped REST administration, queue inspection/retry/deletion, mailbox operations, policies, health, actual security statistics and HA status |
| `gemsads-conf` | JSON/YAML syntax checks, platform configuration validation, redacted views, validated edits, SQLite inspection/backup/offline maintenance and SMTP envelope probes |
| `mailctl` | Older management client; queue/DLQ/message operations overlap, but domain/tenant/replication commands target legacy APIs and are not a complete active-server interface |
| `mailhub-backup` | Whole-deployment offline backup, verification and restore; use this for disaster recovery |
| `mailhub-privacy` | Account inventory/export/deletion with retained-evidence and external-disposition obligations |
| `mailhub-failover`, `mailhub-ha-check` | Fenced ownership transitions and external HA qualification |
| `mailhub-log`, `mailhub-preserve` | Audit evidence and external preservation workflows |
| `mailhub-authcheck`, `mailflow-probe` | Identity-provider qualification and live mailflow monitoring |

## Configuration and JSON/YAML files

```sh
gemsads-conf --config /etc/mailhub/config.yaml config check
gemsads-conf --config /etc/mailhub/config.yaml config check --tls
gemsads-conf --config /etc/mailhub/config.yaml config show
gemsads-conf config create /etc/mailhub/new-config.yaml
printf 'info\n' > /tmp/log-level.yaml
gemsads-conf --config /etc/mailhub/config.yaml config set /logging/level --value-file /tmp/log-level.yaml
gemsads-conf --config /etc/mailhub/config.yaml config set /logging/level --value-file /tmp/log-level.yaml --apply
gemsads-conf --config /etc/mailhub/config.yaml config edit --editor /usr/bin/vi --apply
gemsads-conf file check settings.json
gemsads-conf file show settings.yml
gemsads-conf file create new.json --content-file template.json
gemsads-conf --config /etc/mailhub/config.yaml file format settings.json --apply
gemsads-conf --config /etc/mailhub/config.yaml doctor
```

`config check` uses the same strict schema, semantic rules, runtime-setting rules
and logging-level validation as the service. `--tls` additionally loads configured
key pairs, required client CA material and checks leaf certificate dates. It does
not prove remote trust, hostname coverage, free ports, DNS or dependency readiness.
Generated defaults require STARTTLS and authentication on port 587, contain no
shared account, and bind management interfaces to loopback. Provision actual TLS
certificates and accounts before starting the service.

`set` uses JSON Pointer paths (for example `/server/max_recipients`); parent objects
must already exist. Value files contain one JSON/YAML value. Prefer private files
for secrets rather than shell arguments. `edit` takes an executable, not a shell
command with arguments. Documents are limited to 8 MiB; multiple documents,
duplicate mapping keys, YAML merge keys and excessive alias expansion are rejected.

`format`, `set` and `edit` validate a candidate and default to a dry run. `--apply`
creates a private timestamped `.bak.*` copy, retains original ownership/permissions,
and atomically replaces the original. Symlinks and detected concurrent edits fail.
The `.gemsads.lock` sidecar serializes this utility's writers. Coordinate other
editors; arbitrary external writers do not honor that lock. Generic `file` edits
require the deployment to be stopped and its existing spool lock acquired, because
runtime policy/credential files may have another authoritative writer. Use `config`
commands for the primary platform configuration so schema checks cannot be skipped.

Views redact recognized secret fields, credential-bearing URLs and YAML comments.
This is best-effort masking: custom fields and database payloads may still contain
personal or sensitive data. Do not publish output without review. Backups and editor
temporary files contain the original secrets and use mode 0600. Formatting requires
valid input; syntax errors need correction through an editor before they can be
validated. Changes to runtime policy files must also meet their own application
schema; generic JSON/YAML validation cannot establish that.

`doctor` checks config/TLS and SQLite files under `platform.data_dir`, with a
30-second database-check budget. External PostgreSQL/SQLite stores, external
services, filesystem capacity and actual readiness are outside that check.

## SQLite databases

The main account store defaults to `data_dir/users.db`; configuration may instead
select another SQLite path or PostgreSQL. Other SQLite files store subsystem state.
The spool also contains journals and message payloads: a `.db` snapshot alone is
not a complete mail-system backup.

```sh
gemsads-conf db inspect ./data/users.db
gemsads-conf db rows ./data/users.db users
gemsads-conf db check ./data/users.db
gemsads-conf db backup ./data/users.db --output /secure/backups/users-new.db
gemsads-conf db create /tmp/example.db --schema-file /tmp/schema.sql
# Stop the service before these operations; use the deployed config and data_dir.
gemsads-conf --config /etc/mailhub/config.yaml db edit ./data/users.db --sql-file /secure/edit.sql --backup /secure/backups/users-before-edit.db --apply
gemsads-conf --config /etc/mailhub/config.yaml db maintain ./data/users.db --backup /secure/backups/users-before-maintenance.db --apply
```

Read commands open existing SQLite files in read-only/query-only mode. `rows`
returns at most 100 rows, with recognized secret columns masked. `check` runs
integrity and foreign-key checks. Unsupported/non-SQLite formats fail explicitly.

Backups use SQLite `VACUUM INTO` to include committed WAL changes in a consistent
snapshot, then verify the snapshot. They require a new destination path and source
write access for this implementation. A failed command may leave an incomplete
output file; it is not a verified backup. Never substitute a raw copy of a live
`.db` file. See the [SQLite snapshot documentation](https://www.sqlite.org/lang_vacuum.html#vacuum_with_an_into_clause).

`db create` accepts explicit CREATE TABLE/INDEX statements, with no triggers; it
does not initialize application-specific tables or register a database with the
service. A failed creation may leave an empty database, and never overwrites an
existing file. The application remains responsible for its migrations.

`db edit` accepts one INSERT/UPDATE/DELETE statement, inside a transaction with
foreign-key checks. `db maintain` (aliases `fix`, `reformat`) rebuilds indexes and
compacts a healthy database. SQLite VACUUM may change implicit row IDs in tables
without an INTEGER PRIMARY KEY; confirm the schema does not depend on them.
It refuses corruption instead of claiming a repair.
Both require `--apply`, a verified new `--backup`, an existing deployment ownership
lock, and a target inside that deployment's `data_dir`. External databases are
excluded from these mutations because that lock would not establish ownership.
Use the normal API for account changes whenever possible: SQL cannot enforce every
application or cross-database invariant. For corrupt data, stop writes, preserve
evidence and follow [verified recovery](BACKUP_RECOVERY.md).

## Online administration and Postfix equivalents

Set `ADS_API_ENDPOINT` and `ADS_API_KEY` using your secret environment. There is no
default admin password. HTTPS is required for remote endpoints; certificate trust
uses the operating system trust store. Loopback HTTP is supported for local access.
Redirects and all non-2xx responses fail. Server-side scopes still govern operations.

```sh
adsemailadm health
adsemailadm queue stats
adsemailadm queue list
adsemailadm queue dlq list
adsemailadm queue inspect QUEUE_ID
adsemailadm queue retry QUEUE_ID
adsemailadm queue retry --all
adsemailadm queue delete QUEUE_ID
adsemailadm security stats
adsemailadm cluster status
adsemailadm config reload
adsemailadm api GET /api/v1/quarantine
adsemailadm api PUT /api/v1/mailboxes/alice --body-file /secure/account-update.json
```

Use the journal entry's **`id`**, not the RFC Message-ID or `message_id` field.
Queue output preserves the actual JSON API fields and nested statistics; listings
may contain message content and personal data. Bulk retry enumerates the current
DLQ and retries each ID; it stops on failure, with earlier retries possibly already
accepted. Configuration reload returns acceptance of process replacement, not proof
of completion: wait for readiness and verify the new settings.

| Postfix operation | Mailhub equivalent / boundary |
| --- | --- |
| `postqueue -p` / `-j` | `adsemailadm queue list`, `queue stats`, and `queue dlq list` |
| Inspect a queue item | `adsemailadm queue inspect ID` |
| Retry failed mail | `adsemailadm queue retry ID` or `--all`; specifically DLQ retries |
| `postqueue -f` | No full deferred-queue flush equivalent; normal scheduler retries remain active |
| `postsuper -d ID` | `adsemailadm queue delete ID`, subject to API eligibility and evidence protection |
| `postsuper -h`, `-H`, `-r` | No general queue hold/release/requeue parity; quarantine and compliance release use their own APIs |
| Queue-wide purge | Not supported; the previous CLI advertised a nonexistent endpoint |

Deleting queued state cannot withdraw an SMTP transaction already in flight.
Stored mail must be removed through mailbox protocols/privacy workflows, and
compliance evidence has separate retention controls. For exact Postfix semantics,
see [postqueue](https://www.postfix.org/postqueue.1.html) and
[postsuper](https://www.postfix.org/postsuper.1.html).

Previously fabricated TLS renewal/tests, LDAP sync results, cluster drain/rebalance,
security lookups and monitoring samples have been removed. Real status commands
remain; unavailable commands return errors. `policy validate` checks Starlark
syntax only. Use configured LDAP/SCIM provisioning, certificate automation and
[HA tooling](HIGH_AVAILABILITY.md) for those operations. The generic `api` command
covers existing REST routes without implying that legacy routes are implemented.

## Relay probe and Sieve scripts

```sh
gemsads-conf relay-check --address mail.example.net:587 --starttls --from probe@external-one.example --to probe@external-two.example
adsemailadm --config /etc/mailhub/config.yaml sieve list
adsemailadm --config /etc/mailhub/config.yaml sieve upload alice /secure/alice.sieve
```

Choose envelope addresses outside all local/relay domains and run the probe from
an untrusted source network. The probe never authenticates or sends DATA. A 5xx
MAIL/RCPT rejection passes only that specific envelope test; it does not establish
global closed-relay status (sender policy alone might have rejected it). A 4xx,
network/TLS error, or accepted RCPT requires investigation and returns nonzero.
Accepted RCPT is reported as `possible_relay` because DATA policy was not tested.
Use STARTTLS on TLS-required listeners to exercise the subsequent authentication
and relay checks. Repeat from the actual outside network after configuration changes.

Sieve upload uses the active delivery parser, configured data directory and escaped
account filename, with atomic backed-up replacement. `sieve show` contains the full
script and may expose personal information. Upload `keep;` to disable filtering
reversibly. Legacy inline edit/delete commands were removed; scripts must pass the
same validation as delivery. Check that the named account exists before provisioning.
