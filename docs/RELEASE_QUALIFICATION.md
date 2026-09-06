# Release qualification

Run `bash scripts/verify-release.sh` before releasing. It verifies downloaded
modules, vets the complete tree, runs all tests plus race checks on mail-path
packages, builds every command, and compares the executable version with VERSION.

GitHub Actions repeats these checks on pull requests and main. Separate jobs
build the container and exercise pinned ClamAV with clean and EICAR messages;
Rspamd configuration must also validate. Scanner failures fail the job.
Configure repository branch protection to require all three jobs.

These checks complement deployment drills: exercise the synthetic mailbox
probe, offline restore, and provider-specific fencing in the target environment.
CI cannot certify the availability or fencing behavior of production storage.
