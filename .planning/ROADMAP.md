# go-emailservice-ads — Engineering Roadmap
_Last updated: 2026-04-13_

## Milestone Structure

| Milestone | Theme | Phases | Status |
|-----------|-------|--------|--------|
| M1 | Security Remediation | 1.0–1.4 | Planned |
| M2 | Protocol Completeness | 2.0–2.5 | Planned |
| M3 | Directory & Identity | 3.0–3.2 | Planned |
| M4 | Spam Control — Classic | 4.0–4.3 | Planned |
| M5 | AI Anti-Spam Engine | 5.0–5.3 | Planned |
| M6 | Hardening & Observability | 6.0–6.2 | Planned |

---

## M1 — Security Remediation (Weeks 1–3)
Critical and high-severity issues from the architecture review. No new features until these are closed.

## M2 — Protocol Completeness (Weeks 4–7)
Fix RFC non-conformances that break interoperability with real mail clients (Thunderbird, Apple Mail, Outlook).

## M3 — Directory & Identity (Weeks 8–10)
Full LDAP/AD integration, SAML/OIDC SSO, SCIM provisioning.

## M4 — Classic Spam Control (Weeks 11–14)
Multi-layer reputation scoring, DNSBL lookups, fuzzy hash outbreak detection, threat intelligence feeds.

## M5 — AI Anti-Spam Engine (Weeks 15–20)
On-device ML inference pipeline: feature extraction, model serving, confidence scoring, continuous learning loop.

## M6 — Hardening & Observability (Weeks 21–24)
TLS-RPT reporting, DMARC aggregate reports, distributed cluster state sync, chaos testing.
