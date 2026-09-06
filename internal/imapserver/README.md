# IMAP server compatibility patch

This package is a copy of `github.com/emersion/go-imap/server` at v1.2.1,
including its upstream tests and MIT license. Package name remains `server`.
The parser, commands, responses, client and backend interfaces still come from
the pinned upstream module. Only the platform's server imports use this copy.

The upstream update broadcaster shares channel-backed FETCH, EXPUNGE and LIST
responses between connections. Writing a response drains its channel, so only
one recipient receives the update. `updateResponse` constructs a fresh response
for every recipient. The update reader also exits when its channel closes.
Notification delivery cancels enqueue/write waits when a recipient logs out,
so a departed session cannot hold the storage publisher indefinitely.
Test imports point here, and the TLS fixture generates an ephemeral certificate
instead of importing upstream's internal certificate helper. Two legacy status
error literals use named fields to satisfy the repository-wide vet check.

Keep changes here narrowly scoped. When upgrading go-imap, compare this directory
against upstream and retain the multi-session regression. Remove this copy when
the upstream server provides equivalent fan-out behavior. Do not edit the module
cache or apply patches during builds.

Validation: upstream server tests, race tests, and `scripts/imap-multisession.py`
through `scripts/verify-release.sh`. The live check uses the platform's durable
storage, TLS and authentication rather than the upstream in-memory backend.
