# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

go-emailservice-ads is mailhub software: a Kubernetes-native enterprise email service (Go) for msgs.global internal mail infrastructure. It provides an SMTP server (port 2525), IMAP server (1143), REST API (8080), Postfix-style access control, a Starlark policy engine, multi-tier worker queues with WAL-based disaster recovery, SPF/DKIM/DMARC/DANE verification, global multi-region routing, Elasticsearch event logging, SSO (OAuth2/OIDC), and optional AfterSMTP next-gen protocol support (QUIC/gRPC/blockchain ledger).

Module: `github.com/afterdarksys/go-emailservice-ads` (Go 1.24).

**Known critical gap (per README):** queue workers simulate delivery with a 10ms sleep (`internal/smtpd/queue.go` ~line 149) — messages are accepted, stored, and tracked but never actually sent to remote MTAs.

## Commands

```bash
# Build the three main binaries
go build -o bin/goemailservices ./cmd/goemailservices   # main SMTP/IMAP service
go build -o bin/adsemailadm ./cmd/adsemailadm           # admin CLI (12 command groups, 60+ commands)
go build -o bin/mailctl ./cmd/mailctl                   # legacy v1.0 CLI
# Also present: ./cmd/adspremail, ./cmd/mail-test

# Run standalone
./bin/goemailservices --config config.yaml
./run.sh start|stop            # convenience wrapper (pidfile + service.log)

# Tests
go test ./...
go test ./internal/smtpd/                       # single package
go test ./msgfmt/ -run TestConverter            # single test
python3 test-suite.py                           # root-level Python integration suite (live server)

# Docker / Kubernetes
./deploy.sh build && ./deploy.sh up
kubectl apply -f deploy/kubernetes/base/ ; kubectl apply -f deploy/kubernetes/perimeter/  # or internal/
```

## Architecture

Message flow: SMTP ingress (`internal/smtpd`) → Postfix-style access control (`internal/access`, 20+ lookup map types, stage-based restrictions) → SPF/DKIM/DANE checks (`internal/security`, cached DNS in `internal/dns`) → Starlark policy engine (`internal/policy`, scripts in `policies/*.star`) → persistent message store with WAL journal (`internal/storage`) → multi-tier priority queue (1,050 workers across emergency/msa/int/out/bulk tiers in `internal/smtpd`) → delivery (`internal/delivery` — currently simulated, see gap above).

Around that core:

- **Control plane**: REST/gRPC API (`internal/api`) is what `adsemailadm` and `mailctl` talk to (Basic Auth or `ads_*` Bearer API keys). Auth + account lockout in `internal/auth`; SSO providers in the auth/api layer.
- **Kubernetes/global routing**: `internal/k8s` (service discovery, deployment-mode detection: perimeter/internal/hybrid/standalone) and `internal/routing` (cross-region routing, health, latency/cost-aware) coordinate via etcd/Redis state stores. Manifests live in `deploy/kubernetes/{base,perimeter,internal}`.
- **Observability**: `internal/elasticsearch` (async bulk mail-event indexing with global TraceID correlation) and `internal/metrics` (Prometheus at `/metrics`).
- **AfterSMTP bridge**: `internal/aftersmtp` + vendored `internal/aftersmtplib` add AMP/QUIC/gRPC and Substrate ledger support behind the `aftersmtp.enabled` config flag.
- **msgfmt/**: ADS Mail Format (AMF) reader/writer/converter (EML/mbox ↔ AMF).

Configuration is a single `config.yaml` (see README for full annotated v1.0 and v2.1 examples); `config-premail.yaml`, `policies.yaml`, `divert.yaml`, `master.yaml`, `groups.yaml` configure sub-systems.

## Docs Worth Reading

The repo has extensive per-feature markdown at the root. Most load-bearing: `README.md`, `WORKER_ARCHITECTURE.md`, `SECURITY_FEATURES.md`, `POLICY_ENGINE_DESIGN.md`, `KUBERNETES_ENTERPRISE_ARCHITECTURE.md`, `CLUSTER_ARCHITECTURE.md`, `ELASTICSEARCH_INTEGRATION.md`, `SSO_SETUP.md`, `DEPLOYMENT.md`. License: internal use only (msgs.global infrastructure).
