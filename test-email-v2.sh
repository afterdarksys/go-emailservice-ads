#!/bin/bash
# Test email sending script (improved)

(
  echo "EHLO test.local"
  sleep 1
  echo "MAIL FROM:<sender@example.com>"
  sleep 1
  echo "RCPT TO:<recipient@example.com>"
  sleep 1
  echo "DATA"
  sleep 1
  cat <<'EOF'
From: sender@example.com
To: recipient@example.com
Subject: Test Email - Disaster Recovery System

This is a test message to verify the email service is working.

Features tested:
- SMTP reception
- Queue management
- Persistent storage
- Journaling
- Deduplication
- Rate limiting

Regards,
Email Service Test Suite
EOF
  echo "."
  sleep 1
  echo "QUIT"
  sleep 1
) | nc localhost 2525
