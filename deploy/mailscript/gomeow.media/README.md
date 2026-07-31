# gomeow.media baseline

This directory establishes the first served domain with MailScript as the
public perimeter and EmailService as the private system of record.

## Setup

Run `./scripts/setup-gomeow-media.sh`. It creates a private Docker network and
starts EmailService using `emailservice.yaml`. It deliberately does **not**
publish port 2525: only MailScript may reach the backend.

Build or obtain the MailScript image separately, then run its documented SMTP
proxy command on the same `gomeow-mailnet` network with:

```sh
mailscript proxy --script=/etc/mailscript/perimeter.star --upstream=emailservice:2525
```

For gomeow.media, use the production gateway policy and enable trusted
quarantine forwarding:

```sh
mailscript proxy \
  --script=/etc/mailscript/gomeow-media-gateway.star \
  --upstream=emailservice:2525 \
  --forward-quarantine \
  --enable-tls --cert=/etc/mailscript/tls/fullchain.pem --key=/etc/mailscript/tls/privkey.pem \
  --port=25,587
```

`--forward-quarantine` requires the matching EmailService `content_filter`
configuration. MailScript removes any client-supplied quarantine marker and
adds it only after a policy action, so public senders cannot forge the route.

Publish MailScript's TLS-enabled ports 25 and 587, never EmailService's 2525.
Replace the example `172.30.0.0/24` CIDR if the deployment network changes.

## Verify

After MailScript is running, execute:

```sh
./scripts/test-gomeow-mail-flow.sh --host 127.0.0.1 --port 587
```

The test confirms MailScript accepts a message and the backend stores it in
the `gomeow.media` local mailbox. It never sends external mail or changes DNS.

## DKIM signing

EmailService signs outbound mail itself (`internal/delivery` + `internal/security`);
this is separate from MailScript's inbound filtering.

**Status: done and live.** Key generated at `secrets/mail.private.pem`
(gitignored — never commit it; `setup-gomeow-media.sh` mounts it into the
container automatically), `mail._domainkey.gomeow.media` published to
ns1/ns2 via `dnsscienced/deploy-zones.sh gomeow.media` and verified
resolving on both authoritative servers and a public resolver, and
`server.dkim.enabled: true` in `emailservice.yaml` — all done 2026-07-31.
Remaining steps below are for rotation or standing this up on a new host.

1. (Already done for this key. To regenerate — e.g. on a different host, or to
   rotate — Ed25519 gives a smaller DNS TXT record than RSA and both are
   supported; macOS's default `/usr/bin/openssl` is LibreSSL and doesn't
   support `-algorithm ed25519`, use a real OpenSSL build, e.g. Homebrew's
   `/usr/local/opt/openssl/bin/openssl`):
   ```sh
   openssl genpkey -algorithm ed25519 -out mail.private.pem
   openssl pkey -in mail.private.pem -pubout -outform DER | tail -c 32 | base64
   ```
   `mail.private.pem` must end up at the path `server.dkim.private_key_path`
   points to (default `/etc/goemailservices/dkim/mail.private.pem` — mounted
   there by `setup-gomeow-media.sh` from `secrets/`), mode `0600`, owned by the
   service account only — it is as sensitive as a TLS private key.

2. Publish this TXT record at `mail._domainkey.gomeow.media` (already done —
   published via `~/development/dnsscienced/deploy-zones.sh gomeow.media`,
   source of truth is `gomeow.media.dnszone` in the `dnsscienced-zones` repo;
   for a future rotation, edit that file and rerun the same deploy script):
   ```
   v=DKIM1; k=ed25519; p=s7kTjXy8LNDyGLIG0tyaGGDBHZKF/GXYptptvUDU1gI=
   ```

3. Wait for DNS propagation, then set `server.dkim.enabled: true` and restart.
   Verify with `dig TXT mail._domainkey.gomeow.media` and by sending a test
   message to a mailbox that shows Authentication-Results (e.g. Gmail).

Signing only ever applies to mail whose envelope `MAIL FROM` domain matches
`server.dkim.domain` — it will never sign mail on behalf of a domain this
server doesn't own.

## Continuous mail-flow monitoring

`mailflow-probe` is a separate daemon, so it also detects a hung EmailService
process or blocked queue. It submits through public port 25, polls the dedicated
local mailbox over IMAPS, then flags and expunges only the message bearing its
random `X-Mailflow-Probe-ID` header.

Create a dedicated `mailflow-probe@gomeow.media` mailbox and copy
`mailflow-probe.env.example` to `/etc/goemailservices/mailflow-probe.env` with
mode `0600`. Install `deploy/systemd/mailflow-probe.service`, build the binary,
and enable it with `systemctl enable --now mailflow-probe`.

The daemon exposes `127.0.0.1:9765/healthz` and `/metrics`. Alert when the
latest probe fails, when latency approaches the 60-second timeout, or when the
failure counter increases. Keep the metrics listener private and scrape it via
the local Prometheus agent.
