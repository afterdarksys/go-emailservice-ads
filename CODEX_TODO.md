# Code Review TODO

Review target: `security/tls-dane-dmarc-spf` against `main`.

## P1 — High Priority

- [x] Enforce JMAP message ownership in the storage layer.
  - `internal/jmap/server.go:400`
  - `IMAPAdapter.GetMessages` currently ignores the username and folder and returns every pending message, so the `Email/get` ownership set can include other users' messages.
  - Completed: mailbox entries are scoped by persisted username and folder before JMAP builds its owned-ID set; cross-account coverage is in `internal/jmap/server_test.go`.

- [x] Decode DNS TLSA association data before validating or matching records.
  - `internal/delivery/delivery.go:374`
  - `LookupTLSA` converts the hexadecimal association string with `[]byte(...)` instead of `hex.DecodeString`, causing common SHA-256 and SHA-512 TLSA records to be discarded.
  - Completed: TLSA association data is hex-decoded before validation; regression coverage verifies a DNS-derived SHA-256 record in `dane_test.go`.

- [x] Prevent opportunistic fallback when TLSA lookup results are indeterminate or DNSSEC-bogus.
  - `internal/delivery/delivery.go:380`
  - DNS timeouts and DNSSEC validation failures currently select opportunistic TLS, allowing DANE downgrade.
  - Completed: lookup errors, bogus DNSSEC, and empty validator results now defer delivery; opportunistic TLS is selected only after a determinate non-DANE result. Regression coverage is in `dane_policy_test.go`.

- [x] Apply organizational-domain and subdomain DMARC policies.
  - `internal/smtpd/server.go:491`
  - DMARC lookup currently checks only `_dmarc.<exact From domain>` and does not fall back to the organizational domain or apply `sp=`.
  - Completed: lookup falls back from the exact From domain to its organizational domain and applies `sp=` (including explicit `sp=none`) for subdomains; regression coverage is in `spf_dmarc_test.go`.

## P2 — Medium Priority

- [x] Evaluate HELO SPF for null reverse-path messages.
  - `internal/smtpd/server.go:340`
  - Bounce messages with an empty or `<>` reverse path skip SPF evaluation, even though DMARC later uses EHLO as the SPF identity.
  - Completed: null reverse paths now evaluate SPF against the HELO domain using the RFC-defined postmaster identity; regression coverage is in `spf_identity_test.go`.

- [x] Honor DMARC `pct=` before rejecting or quarantining messages.
  - `internal/smtpd/server.go:508`
  - The record parser captures `pct`, but enforcement applies the policy to every DMARC failure.
  - Completed: DMARC evaluation returns `pct`, and enforcement uses a stable message digest to sample partial rollouts; `pct=0` and `pct=100` are explicit boundary cases. Regression coverage is in `spf_dmarc_test.go` and `spf_identity_test.go`.

## P0 — Delivery and Storage Integrity

- [x] Preserve failed delivery state instead of unconditionally marking the message delivered.
  - `internal/smtpd/queue.go:233`
  - `deliverLocal` and `deliverRemote` set the message back to `pending` on failure, but `processMessage` immediately overwrites that state with `delivered` and removes the message from the in-memory index.
  - Completed: delivery paths report errors to a shared finalizer, which retains failed messages as `pending`; regression coverage is in `queue_persistence_test.go`.

- [x] Stop treating identical message bodies as duplicate mail transactions.
  - `internal/storage/store.go:103`
  - Deduplication uses only `SHA256(message data)`. Messages with different envelope senders, recipients, or transactions but identical RFC 5322 bytes are silently accepted and discarded by `QueueManager.Enqueue`.
  - Completed: each accepted transaction receives a unique durable message ID; regression coverage is in `store_test.go` and `queue_persistence_test.go`.

- [x] Separate mailbox persistence from the outbound retry queue.
  - `internal/storage/imap_adapter.go:54`
  - `StoreMessage` writes mailbox content into `MessageStore` as a `pending` `user`-tier queue entry. The retry scheduler scans every pending tier, changes the entry to `processing`, and `IMAPAdapter.GetMessages` then stops returning it; stored mail can disappear from IMAP after the first retry interval.
  - Completed: mailbox entries now use a dedicated `stored` state and `mailbox` tier, so retry scans exclude them; regression coverage is in `store_test.go`.

## P1 — Message Store and Recovery

- [x] Replay the final journal state, including delivered tombstones.
  - `internal/storage/store.go:71`
  - Recovery skips `delivered` records instead of removing an earlier pending version of the same message, so successfully delivered mail is resurrected and may be redelivered after restart.
  - Completed: recovery applies records in order, treats delivered entries as tombstones, and requeues interrupted `processing` entries; regression coverage is in `store_test.go`.

- [x] Do not discard an entire journal file because its final record is truncated.
  - `internal/storage/journal.go:97`
  - `Replay` logs and skips a whole file when `replayFile` encounters malformed JSON. A crash during the final append can therefore discard all valid records earlier in that file.
  - Completed: recovery retains valid entries before an incomplete final JSON record and fails startup for other corruption; regression coverage is in `store_test.go`.

- [x] Restrict raw message files and journals to the service account.
  - `internal/storage/journal.go:48`
  - Journal and tier files containing complete email bodies are created with mode `0644`, exposing message content to other local users under a typical umask.
  - Completed for new data: journal and tier files now use `0600` and their directories use `0700`; regression coverage is in `store_test.go`. Existing-installation permission migration remains operational work.

- [x] Make mailbox blob, UID, and metadata persistence atomic and propagate failures.
  - `internal/storage/mailbox_store.go:193`
  - UID allocation errors are converted to UID `0`, and the SQLite metadata insert error is ignored after the raw blob has already been accepted. This leaves successful APPEND/local-delivery responses for messages without valid mailbox membership or UID state.
  - Completed: UID and metadata errors now fail the operation and tombstone the orphaned raw blob; regression coverage is in `store_test.go`.

- [x] Track delivery results per domain and recipient.
  - `internal/delivery/delivery.go:107`
  - A failed domain followed by a successful domain can make `Deliver` return success because only `lastResult` and `lastError` are retained. Permanently rejected RCPT commands are also skipped without being represented in the final result.
  - Completed: delivery records recipient outcomes; successful and permanent recipients are removed from the durable transaction, permanent recipients receive bounces, and only temporary recipients remain for retry. Regression coverage is in `queue_persistence_test.go`.

## P1 — Protocol Tuning and Resource Limits

- [x] Wire configured SMTP connection, rate, and timeout controls into the active server.
  - `internal/smtpd/server.go:109`
  - `max_connections`, `max_per_ip`, `rate_limit_per_ip`, granular command timeouts, error limits, command restrictions, and client rate settings are parsed but unused. The active server hardcodes ten-minute read/write timeouts, and its connection counter is never consulted.
  - Completed: the active server enforces configured total/per-IP connection limits, per-IP DATA rate limits, and generic command timeout. Unsupported granular/error/client settings were removed from the production configuration contract; regression coverage is in `limits_test.go`.

- [x] Bound IMAP literal sizes.
  - `internal/imap/server.go:85`
  - `go-imap` defaults `MaxLiteralSize` to unlimited, while APPEND reads the complete literal with `io.ReadAll`. An authenticated client can exhaust memory with an arbitrarily large literal.
  - Completed: the IMAP server bounds literals using the configured SMTP message-size limit; regression coverage is in `mailbox_test.go`.

- [x] Enforce the limits advertised in the JMAP session resource.
  - `internal/jmap/server.go:190`
  - The server advertises request, call, object, upload, and concurrency limits but decodes an unbounded request body and processes every method call without checking them.
  - Completed: API bodies, method-call counts, and `Email/get` object counts are bounded; a semaphore enforces the advertised concurrent-request limit; unimplemented upload limits are no longer advertised. Regression coverage is in `server_test.go`.

## P1 — IMAP/JMAP Conformance

- [x] Return stable persistent UIDs, UIDNEXT, and UIDVALIDITY.
  - `internal/imap/mailbox.go:102`
  - `Status` uses `message count + 1` and the current Unix time, while `ListMessages` emits sequence numbers as UIDs. This ignores the persistent UID methods already exposed by the store and violates UID stability across expunge, reorder, and repeated STATUS calls.
  - Completed: mailbox status and UID FETCH now use persisted UID state; regression coverage is in `store_test.go` and `mailbox_test.go`.

- [x] Honor FETCH sequence sets and return the requested message data.
  - `internal/imap/mailbox.go:129`
  - `ListMessages` ignores both `uid` and `seqSet`, returns every message, fabricates envelopes and internal dates, and leaves body/body-structure requests empty. Normal IMAP clients cannot reliably retrieve mail.
  - Completed: FETCH filters by sequence number or persisted UID and derives envelopes, requested body sections, MIME body structures, size, flags, and internal date from persisted mailbox data. Regression coverage is in `mailbox_test.go`.

- [x] Implement mailbox mutation commands or return an explicit failure.
  - `internal/imap/user.go:63`
  - CREATE, DELETE, RENAME, subscription changes, and COPY report success without persisting any change; APPEND also ignores its supplied flags and internal date.
  - Completed: unsupported CREATE, DELETE, RENAME, SUBSCRIBE, COPY, and APPEND metadata requests now return an explicit error instead of acknowledging non-durable changes. Regression coverage is in `mutation_test.go`.

- [x] Emit a conformant JMAP response `sessionState`.
  - `internal/jmap/server.go:256`
  - `sessionState` is defined as `[]string` and populated from the request's `using` capability list. JMAP requires an opaque state string, so conforming clients receive a response with the wrong JSON type and semantics.
  - Completed: responses return a stable opaque initial state string; regression coverage is in `server_test.go`.

## P2 — Protocol Deployment Semantics

- [x] Separate IMAP STARTTLS and implicit-TLS modes from the generic `require_tls` flag.
  - `internal/imap/server.go:119`
  - With the generated default configuration, `require_tls: true` on port `1143` starts implicit TLS rather than a plaintext listener that requires STARTTLS. Clients expecting STARTTLS on the advertised port cannot connect.
  - Completed: `imap.tls_mode` explicitly chooses `starttls`, `implicit`, or `disabled`; the default is STARTTLS and invalid modes are rejected. Regression coverage is in `mailbox_test.go`.

## Verification

- [x] Run targeted tests:
  - `/usr/local/bin/go test ./internal/auth ./internal/config ./internal/delivery ./internal/imap ./internal/jmap ./internal/security/dane ./internal/smtpd ./internal/storage`
- [x] Add dedicated tests for `internal/delivery`, `internal/imap`, `internal/jmap`, and `internal/storage`.
  - Completed: targeted regression suites now cover the reviewed delivery/DANE, IMAP, JMAP, SMTP, and storage behavior.
- [x] Run `/usr/local/bin/go test ./...` after resolving the existing build failures in unchanged packages.
  - Completed: the repository-wide suite passes after reconciling routing, premail repository API, example, and vet-format build drift.
