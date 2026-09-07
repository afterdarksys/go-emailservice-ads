# DMARC aggregate reports

Enable `platform.dmarc_reporting: true` and `server.dmarc.enabled: true`.
The server domain supplies the report organization and `postmaster@` contact;
configure a valid, monitored domain before enabling outbound reports.

Inbound unauthenticated SMTP evaluations record connecting IP, authentication
results/alignment, header/envelope domains, published policy and actual DMARC
disposition. Monitor and sampled-out outcomes include override reasons. Reports
exclude message bodies and recipient/local-part identities. A reporting write
failure returns SMTP 451 so an evaluation is not acknowledged without evidence.
SMTP retries are separate evaluations and may increment counts again.

Reports are aggregated by UTC day and published policy (including destination),
stored with atomic replacement and fsync under `platform.data_dir/dmarc-reports`.
A changed policy gets a separate report. Each report is limited to 10,000 distinct
rows; exceeding it temporarily defers affected mail. Include this directory in
backup/restore and disk monitoring. Retain completed reports according to your
organization's IP-address retention policy; the server does not silently erase
report evidence.

An hourly worker sends closed-day XML reports through the durable outbound
queue. Only mailto RUA destinations are supported. External organizational-domain
destinations must publish `<policy-domain>._report._dmarc.<destination-domain>`
with `v=DMARC1`. Missing authorization fails closed. URI size modifiers and query
parameters are rejected explicitly. Sent destinations are acknowledged separately;
a failed destination remains retryable. A crash between queue acceptance and the
report acknowledgement can resend the same stable report ID (at-least-once).

Authenticated API endpoints:

- `GET /api/v1/dmarc/reports` (`dmarc:read`): report IDs, domains, intervals, row
  counts and destination acknowledgement state.
- `GET /api/v1/dmarc/reports/{id}` (`dmarc:read`): RFC 7489 aggregate XML.
- `POST /api/v1/dmarc/reports/send` (`dmarc:write`): retry closed-day pending reports.

For missing reports check that DMARC and reporting are enabled, the sender has a
valid policy with RUA, and the reporting interval has closed. For send failures
check external authorization TXT, supported mailto syntax, outbound routing,
queue/DLQ, and filesystem permissions/free space. Acknowledgement means durably
queued, not confirmation that the remote report processor accepted the XML.
TLS aggregate reports remain a separate feature and directory.

Format and authorization reference: [RFC 7489](https://www.rfc-editor.org/rfc/rfc7489.html).
