#!/usr/bin/env bash
set -euo pipefail

host=127.0.0.1
port=587
while [[ $# -gt 0 ]]; do
  case "$1" in
    --host) host=$2; shift 2 ;;
    --port) port=$2; shift 2 ;;
    *) echo "Usage: $0 [--host HOST] [--port PORT]" >&2; exit 2 ;;
  esac
done

python3 - "$host" "$port" <<'PY'
import smtplib, sys
from email.message import EmailMessage

host, port = sys.argv[1], int(sys.argv[2])
msg = EmailMessage()
msg["From"] = "probe@example.net"
msg["To"] = "postmaster@gomeow.media"
msg["Subject"] = "gomeow.media MailScript flow probe"
msg["X-Gomeow-Flow-Probe"] = "true"
msg.set_content("MailScript-to-EmailService baseline probe")
with smtplib.SMTP(host, port, timeout=20) as smtp:
    smtp.send_message(msg)
print("SMTP proxy accepted the gomeow.media flow probe.")
PY

echo "Verify backend persistence: docker exec gomeow-emailservice find data/mail-storage -type f"
