# Replicated-volume active/passive HA

The supported integration has two data nodes and an independent diskless quorum
witness. DRBD 9 synchronously replicates the complete volume (protocol C). A
cluster manager controls primary promotion, the filesystem, mailhub and the
service IP, in that order; stop them in reverse order. Only one mailhub process
owns this volume. Active-active mailbox/shard ownership is not implemented.

`deploy/ha/mailhub.res.example` is a topology template, not an installer. Allocate
and initialize dedicated devices using your storage procedures; never run device
initialization against existing data. Use a private authenticated replication
network (IPsec/WireGuard or qualified DRBD TLS). Configure and test independent
power/storage fencing and redundant cluster communication before enabling
failover. Set cluster STONITH enabled, no-quorum policy stop, and prohibit the
witness from hosting the filesystem or service. Do not disable fencing to make
an unhealthy cluster start. See [LINBIT's DRBD guide](https://linbit.com/drbd-user-guide/drbd-guide-9_0-en/)
for quorum, protocol and cluster-manager requirements.

Mount `/dev/drbd100` as ext4 or XFS at `/srv/mailhub` on the current primary only.
Use the same mount path on both hosts. Migrate all mutable data while stopped,
including identity and mailbox databases, journal/tier files, policies, scripts,
DMARC reports, audit records and the webhook outbox. Do not place SQLite on NFS.
Configuration, secrets and binaries must match on both nodes.

```yaml
platform:
  data_dir: /srv/mailhub/data
  policy_path: /srv/mailhub/policies.yaml
  fencing_lease_file: /srv/mailhub/owner.lock
  ha:
    enabled: true
    volume: /srv/mailhub
    check_command:
      - /usr/local/bin/mailhub-ha-check
      - --resource
      - mailhub
      - --volume
      - /srv/mailhub
      - --device
      - /dev/drbd100
auth:
  user_database_url: /srv/mailhub/data/users.db
```

Build `go build -o mailhub-ha-check ./cmd/mailhub-ha-check`. It reads
`/usr/sbin/drbdsetup status mailhub --json` and `/usr/bin/findmnt` and refuses
missing quorum, stale disk, suspended I/O, multiple primary peers, incorrect
resource/device association or a missing read-write mount. Use current DRBD 9
utilities with boolean quorum output. The service account needs read access to
DRBD status; provide a narrowly scoped privileged status helper if your host
requires it. Never replace the check with an unconditional success command.
The generic check-command contract is the JSON `ha.Proof` structure; it must
independently verify the configured volume. Executables/configuration are trusted
operator inputs and must not be writable by mail users.

At startup the main server verifies ownership before opening stores. During
operation it checks every second with a two-second command deadline. Failed
verification terminates the process immediately, including outbound delivery;
`/ready` also reflects stale/failed proof. The external storage quorum and fencing
must protect the detection interval. A shell wrapper that leaves descendants
running is unsupported. The checker does not configure replication or certify
that a provider's fencing policy is correct.

`GET /api/v1/ha/status` requires `ha:read` and exposes proof freshness and peer
replication health. A surviving primary with quorum may operate while its data
peer is unavailable; redundancy is degraded until resynchronization completes.
Do not promise zero data loss across simultaneous failures or broken fencing.

Use the systemd unit template with your actual binary name and permissions.
Configure the cluster resource manager with one promoted DRBD instance, a
filesystem resource colocated with that promoted instance, then the mailhub
systemd resource and service IP. Do not independently enable mailhub at boot.
The earlier `mailhub-failover` tool remains available for explicitly fenced
cold-standby recovery; it is not an automatic promotion API for this cluster.

Acceptance requires disposable-host drills: sustained authenticated delivery,
mailbox edits and account changes; primary power loss; replication partition;
witness loss; loss of both peers; stale-node return; and resynchronization.
Verify exactly one sender/owner, retained acknowledged data, backup restore,
readiness/traffic routing and measured RPO/RTO. Automated tests cover parser,
ownership rejection and runtime check failure; no real cluster drill has been
performed in this workspace. Retain HA qualification as an operational gate.
