#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go mod verify
go vet ./...
go test -timeout 10m ./...
go test -race -timeout 10m ./internal/auth ./internal/privacy ./internal/imap ./internal/imapserver ./internal/jmap ./internal/auditlog ./internal/bounce ./internal/compliance ./internal/logformat ./internal/oauthaccess ./internal/preservation ./internal/api ./internal/backup ./internal/delivery ./internal/dns ./internal/failover ./internal/filtering ./internal/policy ./internal/security ./internal/smtpd ./internal/storage ./internal/tlsutil
go build ./cmd/...
release_tmp=$(mktemp -d)
trap 'rm -rf "$release_tmp"' EXIT
go build -trimpath -o "$release_tmp/mailhub" ./cmd/goemailservices
test "$("$release_tmp/mailhub" -version)" = "$(cat VERSION)"

go build -trimpath -o "$release_tmp/backup" ./cmd/mailhub-backup
smoke_args=(--binary "$release_tmp/mailhub" --backup "$release_tmp/backup")
if [[ -n "${MAILHUB_QUALIFICATION_REPORT:-}" ]]; then
  smoke_args+=(--report "$MAILHUB_QUALIFICATION_REPORT")
fi
python3 scripts/smoke-mailhub.py "${smoke_args[@]}"
