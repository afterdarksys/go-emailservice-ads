# Release qualification

Run `bash scripts/verify-release.sh` before releasing. It verifies downloaded
modules, vets the complete tree, runs all tests plus race checks on mail-path
packages, builds every command, and compares the executable version with VERSION.

GitHub Actions repeats these checks on pull requests and main. Separate jobs
build the container and exercise pinned ClamAV with clean and EICAR messages;
Rspamd configuration must also validate. Scanner failures fail the job.
Main branch protection requires `qualify`, `scanners` and `container`, with an
up-to-date base and administrator enforcement. The applied configuration is
[branch-protection.json](../.github/branch-protection.json). Keep job names stable
or update the required checks before renaming them. Force pushes and branch
deletion are disabled.

The release suite also runs `TestOpenAPI` in `internal/api`: it validates the
[OpenAPI contract](openapi.json), references, version, routing coverage, permissions,
Go JSON fields and representative live-handler responses. This catches common
contract drift; it is not exhaustive behavioral testing of every response.

These checks complement deployment drills: exercise the synthetic mailbox
probe, offline restore, and provider-specific fencing in the target environment.
CI cannot certify the availability or fencing behavior of production storage.
