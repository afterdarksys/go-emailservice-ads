# Production hardening: 12 feature commits

1. Bounded API startup/shutdown; remove fake gRPC listener.
2. Consistent offline backups, verified restore and recovery drill.
3. Correct DNS mail routing semantics.
4. Durable policy CRUD and isolated testing API.
5. Destination-aware dispatch and throttling.
6. Operational metrics, alerts and synthetic probe.
7. Reloadable TLS/mTLS and overlapping API credential rotation.
8. Fenced active/passive recovery tooling.
9. Automated release qualification in CI.
10. Integration edge cases: transport/reporting.
11. Integration edge cases: recovery/security controls.
12. Starlark filtering extension enhancements.

Each feature receives focused tests and its own commit. Final release checks cover the complete tree. External production deployment is outside this implementation run.
