# Operational monitoring

Scrape each hub's HTTPS `/metrics` endpoint with its trusted CA. Restrict network
access to monitoring systems. Load `deploy/monitoring/mailhub-rules.yml` in
Prometheus and configure Alertmanager routing. Test notification delivery before
relying on it. Queue age, state counts, free disk, delivery duration count/sum,
required dependency health, antivirus database age, certificate expiry and
mailstorm circuit counts are exported from live runtime state. Destination
backoff/active counts are aggregated to avoid unbounded domain labels.

Run `go build -o mailflow-probe ./cmd/mailflow-probe` as a separate monitored
service. Use a dedicated mailbox because the probe expunges deleted messages in
that mailbox. Configure MAILFLOW_SMTP_ADDR, MAILFLOW_SMTP_USER/PASSWORD,
MAILFLOW_IMAP_ADDR, MAILFLOW_IMAP_USER/PASSWORD, MAILFLOW_SENDER and
MAILFLOW_RECIPIENT. Set MAILFLOW_IMAP_STARTTLS=true for port 1143; otherwise use
implicit IMAPS. Authentication requires verified STARTTLS on SMTP. Install your
private CA in the probe's trust store. Never use real user mailboxes.

The probe sends a random correlation header, checks IMAP delivery, deletes its
message and exposes metrics on port 9765. Alert on its failed/stale runs as well
as mailhub gauges. Exercise scanner outage, queue buildup and certificate alerts
in staging. Baselines and counters reset on process restart; active mailstorm
circuits persist. Default destination throttling is four concurrent deliveries
per domain, 30-second initial transient backoff, up to one hour. Configure
platform.destination_throttle to match provider limits.
