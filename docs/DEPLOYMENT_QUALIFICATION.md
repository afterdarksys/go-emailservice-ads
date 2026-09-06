# Qualification and production gates (2.7)

Run `bash scripts/verify-release.sh` with Go 1.24.6 or newer. It runs module
verification, vet, package/race tests, command builds, version agreement and an
isolated SMTP-to-IMAP delivery/shutdown/backup/restore test. Python 3 and OpenSSL
are required for the process test. Test configuration forces its own data
directory, even if MAILHUB_DATA_DIR is set in the caller's environment.

`scripts/smoke-mailhub.py --binary /path/to/goemailservices --backup
/path/to/mailhub-backup` can run separately. It sends only between temporary
local test accounts and removes the entire test directory afterward.

## Immutable evidence and independent checkpoints

Build `cmd/mailhub-preserve` and supply a YAML configuration based on
`examples/preservation.yaml`. AWS CLI must be installed; the container includes
it. Credentials come from the standard AWS credential chain. File settings for
profile, region and endpoint override their corresponding CLI defaults.
The source can be exported evidence, a backup artifact, or the JSON head produced
by `mailhub-log --verify-audit`. Never keep the only independent checkpoint on
the same mutable filesystem as the audit log it verifies.
Archive a completed artifact; do not use a log or backup that is still being
written as the source.

The utility checks bucket Object Lock, uploads a private snapshot with COMPLIANCE
retention and optional legal hold, then checks the exact returned version's
retention and checksum metadata. A JSON receipt identifies the bucket, key,
version, checksum and retention. Preserve that receipt independently. Every
retry may create another protected version; it never overwrites a protected
version. Defaults cap artifacts at 1 GiB; `max_bytes` may allow up to 5 GiB.
Split larger artifacts. Configure a future `retain_until` approved for the record
class. The example deliberately contains an expired date to require a decision.

Run `python3 scripts/qualify-preservation.py /path/to/mailhub-preserve` for a local
MinIO drill. It uses the existing local image ID, a fresh container, loopback
port, disposable test credentials and synthetic data. It verifies locked-version
deletion is denied and recovered bytes match. The container is removed afterward.
`allow_local_http` is restricted to loopback tests; real endpoints require HTTPS.

This is an Object Lock integration, not a legal certification. Configure bucket
policies, access separation, encryption, replication, lifecycle disposal and
retention according to the actual environment. COMPLIANCE retention protects
specific versions; a delete marker is not proof that a protected version was
erased. Reference: https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html

## Production OAuth qualification

Build `cmd/mailhub-authcheck`. Its YAML config (`examples/authcheck.yaml`) names
the platform configuration, environment variables holding valid and revoked
access tokens, a required scope and a scope the valid token must lack.
It requires explicit insufficient-scope and inactive-token responses, then
rechecks a valid token; an outage does not count as proof of revocation.
Token contents are never printed. Configure MFA/PKCE in the identity provider
and its client, and delegate matching domain/action grants in mailhub.

This command has been compiled and the underlying introspection claims/scopes
tested against an isolated TLS server. No production identity provider was
selected or contacted in this implementation run. Supply the actual provider
configuration and qualification tokens before treating this gate as passed.

## Deployment evidence and remaining gates

Local qualification exercised authenticated SMTP/IMAP, graceful shutdown,
backup/restore, Object Lock upload/retention/delete denial/read-back, the
container build/version entry point, pinned ClamAV clean/EICAR scanning, and
Rspamd configuration validation. Rspamd reported a task-timeout warning; review
its scanner time budgets against production traffic before deployment.

The release workflow now runs the process smoke test through verify-release.sh.
Hosted qualification passed for PRs #3 and #4, including scanner integration,
container builds and the full release suite. The main branch now requires those
three checks on an up-to-date base, including administrator changes. Production
identity-provider, storage-destination and provider-specific fencing tests remain
deployment gates. Existing failover tests validate the activation sequence and
lock behavior; they do not prove a cloud or hypervisor fencing API works.
Use the production plan from `docs/FAILOVER.md` only after its provider-specific
fence, independent verification, activation and readiness hooks are configured.

Journal transaction records introduced in 2.7 require the 2.7 reader. Do not
downgrade binaries against a journal containing these records. Restore a
pre-upgrade backup when a binary downgrade is required.
