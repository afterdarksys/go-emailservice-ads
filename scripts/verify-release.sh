#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go mod verify
go vet ./...
go test -timeout 10m ./...
go test -race -timeout 10m ./internal/api ./internal/backup ./internal/delivery ./internal/dns ./internal/failover ./internal/filtering ./internal/policy ./internal/security ./internal/smtp ./internal/storage ./internal/tlsutil
go build ./cmd/...
release_tmp=$(mktemp -d)
trap 'rm -rf "$release_tmp"' EXIT
go build -trimpath -o "$release_tmp/mailhub" ./cmd/goemailservices
test "$("$release_tmp/mailhub" -version)" = "$(cat VERSION)"
