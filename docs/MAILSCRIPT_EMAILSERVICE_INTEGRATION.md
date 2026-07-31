# MailScript Content-Filter Integration

MailScript is the perimeter content filter; go-emailservice-ads remains the
mail system of record. This boundary avoids duplicating rule evaluation,
authentication analysis, attachment inspection, and classification inside the
SMTP/queue service.

## Topology

```text
Internet SMTP clients
        |
        v
MailScript SMTP proxy (:25 / :587)
        |
        | private network only
        v
go-emailservice-ads SMTP listener (:2525)
        |
        v
queue, mailbox storage, IMAP/JMAP, outbound delivery
```

Never expose the EmailService upstream listener to untrusted clients; doing so
would permit a sender to bypass the MailScript policy.

## Responsibilities

| Component | Owns |
|---|---|
| MailScript | Starlark rules, RFC/header validation, SPF/DKIM/DMARC analysis, attachment/content inspection, ML classification, accept/reject/quarantine policy decisions |
| EmailService | SMTP authentication and recipient authorization, durable queueing, mailbox persistence, retries, bounces, IMAP/JMAP, outbound MX delivery |

MailScript's normal SMTP proxy is the supported integration path. Do **not**
use its gRPC `forward_to_upstream` path until MailScript resolves its documented
false-success forwarding defect.

## Initial deployment

1. Build and run MailScript from its own repository.

   ```sh
   ./build.sh
   ./mailscript proxy \
     --script=/etc/mailscript/perimeter.star \
     --upstream=goemailservices-internal:2525
   ```

2. Bind EmailService to an internal address or private Kubernetes Service.
   Permit inbound SMTP to that listener only from the MailScript workload or
   host. MailScript owns public ports 25 and 587.

3. Terminate STARTTLS at MailScript when it is the public endpoint. Configure
   its upstream connection and EmailService's `allow_insecure_auth` so
   credentials are never accepted over a plaintext public connection.

4. Begin in monitor/quarantine mode. Capture rule decisions and queue events;
   only change a rule to SMTP rejection after reviewing false positives.

## Action mapping

| MailScript rule action | SMTP result | EmailService behavior |
|---|---|---|
| `accept()` | Relay unchanged to upstream | Normal authorization, queue, and delivery |
| `quarantine()` | Relay only with an agreed quarantine marker/recipient | Store in a dedicated mailbox; do not send outbound |
| `reject()` | Return a permanent SMTP rejection before relay | No EmailService queue entry |
| `drop()` | Return an explicit SMTP failure or locally record the discard | Never claim accepted delivery and silently discard |

The exact quarantine contract must be implemented deliberately. Preferred
options are a MailScript-added trusted header that EmailService validates only
from the private proxy, or a dedicated internal recipient/address routed to a
quarantine mailbox. Do not trust a client-supplied header.

## Claude implementation checklist

- [ ] Add a `content_filter` configuration block to EmailService describing the
  trusted MailScript proxy network and quarantine contract.
- [ ] Add an integration deployment artifact (Compose or Kubernetes) that
  exposes MailScript publicly and EmailService only internally.
- [ ] Implement and test trusted quarantine-marker handling in SMTP DATA:
  public clients must not be able to set it.
- [ ] Preserve MAIL FROM, all RCPT TO values, SMTP response codes, and message
  bytes across proxying.
- [ ] Add an end-to-end SMTP test covering accept, reject, quarantine, proxy
  bypass denial, and upstream outage behavior.
- [ ] Add health checks and metrics for MailScript relay latency, policy
  decisions, upstream failures, and bypass attempts.
- [ ] Update the production runbook with certificate rotation, rollback, and
  a policy-monitoring rollout procedure.

## Rollout and rollback

Deploy in observe mode first. Keep an operator-controlled route that can move
public SMTP back to EmailService only during a MailScript outage, and record
that this bypass disables content policy. Roll back by changing the public
load-balancer target; do not expose the internal EmailService port permanently.

## Non-goals

This integration does not replace EmailService transport maps, hold queues,
queue retention, per-client limits, bounce handling, or delivery retries.
Those remain EmailService responsibilities.
