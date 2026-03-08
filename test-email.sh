#!/bin/bash
# Test email sending script

{
  echo "EHLO test.local"
  sleep 0.5
  echo "MAIL FROM:<sender@example.com>"
  sleep 0.5
  echo "RCPT TO:<recipient@example.com>"
  sleep 0.5
  echo "DATA"
  sleep 0.5
  echo "From: sender@example.com"
  echo "To: recipient@example.com"
  echo "Subject: Test Email - Disaster Recovery System"
  echo ""
  echo "This is a test message to verify the email service is working."
  echo "Features tested:"
  echo "- SMTP reception"
  echo "- Queue management"
  echo "- Persistent storage"
  echo "- Journaling"
  echo "."
  sleep 0.5
  echo "QUIT"
} | nc localhost 2525
