# General configuration reload

Change the configured YAML file atomically, validate it with
`goemailservices --check-config --config /etc/mailhub/config.yaml`, then call
`POST /api/v1/config/reload` using `config:write`, or send SIGHUP to the mailhub
process. The API accepts no arbitrary file path or configuration body.

Reload preflight validates the full YAML/configuration contract, log level, TLS
certificates, JMAP JWT key, DKIM signing keys and LDAP settings/secret files.
Invalid configuration leaves the running service active and returns HTTP 409.
A valid request returns 202 and starts a graceful shutdown followed by execution
of the same binary with the same arguments and environment. The PID is retained;
process memory, counters, connections and caches are recreated. Queue/mailbox
state and accounts remain durable. Every subsystem receives the new configuration.

This is a coordinated restart, not an uninterrupted hot swap. Clients reconnect;
allow the configured shutdown window plus normal startup time. Monitor readiness
and verify a changed setting before declaring reload complete. A 202 response is
acceptance of the request, not successful completion. Runtime resources such as
new listen ports and external databases can only be fully checked at startup;
failures leave the service unready or exited. Restore the last known-good file and
restart through your service manager if those checks fail. Do not change the
configuration again while a reload is draining.

Environment variables are inherited; changing a service-manager environment,
container image, binary path or operating-system limits requires a normal service
restart/deployment. Changing data-directory or database location selects that
store; it does not migrate existing mail. Back up and perform an explicit data
migration before making such changes.

Existing narrower live mechanisms remain useful: policy reload, TLS file refresh,
API key files, and LDAP bind-secret file rotation avoid replacing the process.
The release smoke test verifies that invalid reload leaves service available and
that a valid reload activates a new API key while preserving existing accounts.
