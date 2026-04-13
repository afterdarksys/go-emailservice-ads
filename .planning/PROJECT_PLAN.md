# go-emailservice-ads — Master Project Plan
_Generated: 2026-04-13 | Status: Draft_

---

## 1. Overview

This plan covers six milestones addressing the findings from the Sr. Email Architect
review. It is ordered strictly by risk: security vulnerabilities first, then protocol
correctness, then new capability.

**Total estimated engineering effort**: ~57–63 days (single engineer)
**Recommended team**: 2 engineers → ~30–35 calendar days

---

## 2. Milestone Summary

```
Week  1-3   M1: Security Remediation      ████████████  [CRITICAL — start immediately]
Week  4-7   M2: Protocol Completeness     ████████████████
Week  8-10  M3: Directory & Identity      ████████████
Week 11-14  M4: Classic Spam Control      ██████████████████
Week 15-20  M5: AI Anti-Spam Engine       ████████████████████████
Week 21-24  M6: Hardening & Observability ████████████
```

---

## 3. Milestone Detail

### M1 — Security Remediation (Weeks 1–3)
_No new features ship until M1 is complete. Security issues are production blockers._

| Phase | Title | Effort | Priority |
|-------|-------|--------|----------|
| 1.0 | Critical Security Fixes (P0) | 3–4 days | P0 |
| 1.1 | High-Priority Security Fixes (P1) | 2–3 days | P1 |

**Phase 1.0 deliverables** (in execution order):
1. Remove hardcoded credentials from legacy SMTP bridge (`legacy/smtp_server.go:194`)
2. Wire `deliverLocal` to IMAP store — fix silent mail discard (`queue.go:237`)
3. Implement real ARC chain verification — remove stub `return true` (`arc.go:332-339`)
4. Make DKIM verification synchronous, feed result to policy (`server.go:411-425`)
5. Fix DMARC identifier alignment — add RFC5322.From domain check (`spf_dmarc.go:326`)

**Phase 1.1 deliverables**:
6. Tag SPF softfail with `X-SPF-Status` header, add to scoring
7. Guard `generateBounce` against null envelope sender
8. Replace `generateShortID` with `uuid.New().String()`
9. Non-blocking `Enqueue` with 100ms backpressure timeout
10. MailScript SSRF protection: HTTPS-only, private IP block, 64KB body limit
11. MailScript execution timeout: `thread.SetMaxSteps(1_000_000)`
12. Legacy SMTP TLS: add `MinVersion: tls.VersionTLS12`
13. ARC signing: `rsa.SignPKCS1v15(rand.Reader, ...)`

---

### M2 — Protocol Completeness (Weeks 4–7)

| Phase | Title | Effort |
|-------|-------|--------|
| 2.1 | IMAP Persistence (UIDVALIDITY, SEARCH, STORE, EXPUNGE, IDLE) | 3–4 days |
| 2.2 | Sieve Execution Engine | 3–4 days |
| 2.3 | JMAP Real Implementation | 2–3 days |
| 2.4 | SMTP Protocol Gaps (timeouts, SPF redirect, DMARC tags) | 1–2 days |
| 2.5 | DKIM Ed25519 support | 1 day |

Key outcomes:
- Thunderbird, Apple Mail, Outlook connect and operate correctly
- Sieve scripts (`fileinto`, `reject`, `vacation`, `imap4flags`) execute
- JMAP returns real stored messages; Bearer token auth works
- SMTP timeouts comply with RFC 5321 §4.5.3.2

---

### M3 — Directory & Identity (Weeks 8–10)

| Phase | Title | Effort |
|-------|-------|--------|
| 3.1 | LDAP group resolution + LDAP auth backend | 3 days |
| 3.2 | OIDC token validation + SCIM 2.0 provisioning | 2–3 days |

Key outcomes:
- Active Directory and OpenLDAP group membership drives routing
- OIDC tokens from Okta/Entra ID authenticate SMTP/IMAP/JMAP
- SCIM 2.0 enables automated user provisioning from IdP

---

### M4 — Classic Spam Control (Weeks 11–14)

| Phase | Title | Effort |
|-------|-------|--------|
| 4.1 | IP/Domain DNSBL + PTR/EHLO anomaly scoring | 3 days |
| 4.2 | Content scoring: URL scan, TLSH fuzzy hash, header anomaly | 3–4 days |
| 4.3 | Composite scoring engine + threat intelligence feeds | 2–3 days |

Key outcomes:
- Multi-layer scoring: IP reputation + envelope + content + authentication
- TLSH-based outbreak detection replaces SHA256 (fuzzy, not exact)
- Spamhaus ZEN, SURBL, URIBL, Barracuda DNSBL integrated
- `X-Spam-Score`/`X-Spam-Status` headers on all messages
- Quarantine → Spam folder; reject > threshold

---

### M5 — AI Anti-Spam Engine (Weeks 15–20)

| Phase | Title | Effort |
|-------|-------|--------|
| 5.1 | Feature extraction pipeline (300-dim vector) | 3 days |
| 5.2 | FastText + ONNX XGBoost + Rspamd sidecar ensemble | 5–6 days |
| 5.3 | Phishing detection (lookalike, URL expand, HTML analysis) | 3 days |
| 5.4 | Continuous learning (per-user Bayes, monitoring) | 1–2 days |

Key outcomes:
- **FastText** (< 2ms): primary text classifier, embedded pure-Go
- **XGBoost/ONNX** (< 10ms): structured feature classifier via ONNX Runtime
- **Rspamd** (async, < 60ms): deep neural + Bayesian + fuzzy hash sidecar
- **Ensemble**: weighted combination adds AI score to classic spam pipeline
- **Phishing**: homograph/typosquat detection, URL expansion, brand protection
- **Learning loop**: user moves to Spam/Ham → model feedback

---

### M6 — Hardening & Observability (Weeks 21–24)

| Phase | Title | Effort |
|-------|-------|--------|
| 6.1 | TLS-RPT + DMARC aggregate reporting | 2 days |
| 6.2 | Distributed cluster state (Redis pub/sub for outbreak) | 2 days |
| 6.3 | Chaos testing + resilience validation | 1–2 days |

---

## 4. Dependency Graph

```
M1.P1.0 (critical security)
    └── M1.P1.1 (high security)
            └── M2.P2.1 (IMAP persistence)
            │       └── M2.P2.2 (Sieve)
            │       └── M2.P2.3 (JMAP)
            ├── M3.P3.1 (LDAP)
            │       └── M3.P3.2 (OIDC/SCIM)
            └── M4.P4.1 (DNSBL)
                    └── M4.P4.2 (content scoring)
                            └── M4.P4.3 (composite scorer)
                                    └── M5.P5.1 (feature extraction)
                                            └── M5.P5.2 (model inference)
                                            └── M5.P5.3 (phishing)
                                            └── M5.P5.4 (learning loop)
                                                    └── M6 (hardening)
```

---

## 5. New Package Structure

```
internal/
├── auth/            (existing — add LDAP backend)
├── ai/
│   ├── features/    [NEW M5] feature extraction pipeline
│   ├── models/      [NEW M5] FastText + ONNX inference
│   ├── phishing/    [NEW M5] lookalike, URL expander
│   └── ensemble/    [NEW M5] score combiner
├── reputation/      [NEW M4]
│   ├── dnsbl.go     DNSBL lookup engine
│   ├── helo.go      PTR/EHLO anomaly scoring
│   ├── fuzzy_hash.go TLSH implementation
│   ├── url_scanner.go URL extraction + URIBL
│   ├── header_analyzer.go RFC5322 anomaly scoring
│   ├── scorer.go    composite scoring engine
│   └── threatfeed.go threat intelligence integration
├── imap/            (existing — fix UIDVALIDITY, SEARCH, STORE)
├── jmap/            (existing — wire to store, fix JWT)
├── policy/
│   └── sieve/       (existing stub — implement)
├── security/        (existing — fix ARC verification, DMARC alignment)
└── smtpd/           (existing — fix timeouts, wire scoring pipeline)
```

---

## 6. go.mod Additions

```go
// M3
// go-ldap/ldap/v3 — already present

// M4 — no new deps (uses miekg/dns already present)

// M5
github.com/yalue/onnxruntime_go v1.10.0  // ONNX Runtime CGo bindings

// M6 — Redis for distributed state
github.com/redis/go-redis/v9 v9.x.x
```

---

## 7. Success Metrics

| Metric | Baseline | Target after M4+M5 |
|--------|----------|-------------------|
| False positive rate | unmeasured | < 0.1% |
| False negative rate | unmeasured | < 2.0% |
| Spam score p99 latency | N/A | < 80ms |
| IMAP UIDVALIDITY persistence | broken | 100% stable |
| DMARC alignment compliance | broken | RFC 7489 compliant |
| Local delivery reliability | 0% (silent drop) | 99.99% |
| ARC verification | always true (fake) | real sig verify |

---

## 8. Risk Register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| ONNX Runtime CGo breaks musl/Alpine | Medium | High | Pure-Go GBM fallback tree walker |
| Rspamd sidecar adds k8s complexity | Low | Medium | Rspamd is optional; fast models cover P99 |
| TLSH patent concerns | Low | Medium | MIT-licensed Go impl exists; SSDEEP fallback |
| LDAP bind credentials leaked | Low | Critical | Vault/k8s secrets, never in config files |
| Sieve implementation bugs break delivery | Medium | High | Fail-open: Sieve error → deliver to INBOX |
| DMARC alignment change breaks existing flows | Medium | Medium | Rollout with `p=none` monitoring first |

---

## 9. Phase Files

Detailed task plans for each phase:

- [M1-P1.0 Critical Security](.planning/phases/M1-P1.0-critical-security.md)
- [M1-P1.1 High Security](.planning/phases/M1-P1.1-high-security.md)
- [M2-P2.0 Protocol Completeness](.planning/phases/M2-P2.0-protocol-completeness.md)
- [M3-P3.0 Directory & Identity](.planning/phases/M3-P3.0-directory-identity.md)
- [M4-P4.0 Classic Spam Control](.planning/phases/M4-P4.0-spam-control-classic.md)
- [M5-P5.0 AI Anti-Spam Engine](.planning/phases/M5-P5.0-ai-antispam.md)
- [M6-P6.0 Hardening & Observability](.planning/phases/M6-P6.0-hardening-observability.md)
- [ROADMAP](ROADMAP.md)

Research (pending background agents):
- `.planning/research/spam_control_research.md`
- `.planning/research/ai_antispam_research.md`
