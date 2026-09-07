# Extensions, administration and high availability

Implementation sequence:

1. Authenticated gRPC management transport sharing REST authorization and handlers.
2. Embedded administration console for operational views and account management.
3. Durable signed management-event webhooks with bounded retries and inspection.
4. Versioned external admission plugin protocol with strict limits and failure policy.
5. Deterministic transport routing by recipient suffix and envelope sender.
6. Active/passive HA integration for synchronously replicated whole-volume storage,
   startup/runtime ownership checks, and fenced activation. The legacy message-only
   replication prototype is not a complete replication solution.
7. Regression tests, operator/API documentation and backlog reconciliation.
8. Full release and CI qualification, then merge and sync main.

HA defaults to two-node active/passive unless a different topology is selected.
Every mutable file (mailbox/identity databases, journal, scripts, policies and
operational state) must reside on replicated storage. Provider fencing and real
node-loss acceptance require the deployment; unit tests cannot certify them.
Extensions are explicit opt-ins. No arbitrary browser-supplied command execution,
plugin loading or webhook destinations are permitted.
