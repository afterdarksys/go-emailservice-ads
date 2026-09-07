# Configuration and relay safety

The default SMTP listener is now port 587. It requires TLS and SMTP AUTH using
SASL PLAIN; PLAIN credentials are not offered over plaintext. Relay networks are
empty. The generated configuration has no shared bootstrap account or password
and is written with mode 0600. Provision real accounts before allowing users to
submit. Existing installations retain their explicitly configured ports and
accounts; removing a bootstrap entry does not delete an already persisted user.
Rotate/delete old demonstration accounts through account management.

For an Internet MX, configure a separate `platform.listeners` perimeter listener
on port 25 and local domains/recipient validation. It receives local mail without
requiring strangers to authenticate, but rejects external-to-external relay.
Submission listeners require TLS and account authentication. Trusted filtering
proxies do not automatically grant relay. An authenticated IMAP/IMAPS session
does not grant SMTP relay to its client IP. SMTP AUTH/SASL is the supported
submission mechanism; IMAPS-before-SMTP is not implemented.

Validation rejects public/global relay CIDRs, malformed networks, public
plaintext AUTH, submission without authentication/TLS or a SASL mechanism,
invalid local domains, and overlapping active listener bindings. Explicit
private/loopback relay networks remain an operator-controlled option. Avoid
large private grants behind shared proxies/NAT; prefer per-account AUTH.
Plaintext AUTH is permitted only for an explicitly bound loopback test listener.
The default management bind is 127.0.0.1:8080; the web console and gRPC require TLS
and explicit enablement.

Run `mailhub --check-config --config /etc/mailhub/config.yaml` before installing
changes. It validates syntax, known fields and semantic combinations without
starting services. It does not prove runtime dependencies are available.
SIGHUP and `/api/v1/config/reload` additionally validate certificates/keys and
reject storage-directory, identity-database, fencing-lease and HA topology
changes. Such changes need a stopped-service migration. Invalid candidates leave
the current configuration running. Valid reloads drain and restart connections;
clients must reconnect. Keep the previous configuration for rollback.

No configuration validator can guarantee absence of outages: port races, expired
certificates, provider failures, failed disks and DNS/network faults remain
possible. Required scanners/plugins and HA quorum deliberately fail closed when
safety cannot be established. Check service readiness and perform an isolated
submission/readback after reload. Use qualified HA and traffic switching where
maintenance downtime is unacceptable.

The release smoke test covers plaintext AUTH suppression, invalid credentials,
unauthenticated submission rejection, authenticated external RCPT permission
(without sending external mail), perimeter relay denial and permitted local
receipt. It also verifies rejected invalid reloads and successful valid reloads.
