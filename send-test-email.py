#!/usr/bin/env python3
"""Send test email via SMTP"""

import smtplib
from email.message import EmailMessage

# Create message
msg = EmailMessage()
msg['From'] = 'sender@example.com'
msg['To'] = 'recipient@example.com'
msg['Subject'] = 'Test Email - Disaster Recovery System'

msg.set_content('''This is a test message to verify the email service is working.

Features tested:
- SMTP reception
- Queue management
- Persistent storage
- Journaling
- Deduplication
- Rate limiting

Message ID will be automatically assigned.

Regards,
Email Service Test Suite
''')

try:
    # Connect to SMTP server
    with smtplib.SMTP('localhost', 2525) as smtp:
        print("Connected to SMTP server")

        # Send email
        smtp.send_message(msg)
        print("✓ Email sent successfully!")
        print(f"  From: {msg['From']}")
        print(f"  To: {msg['To']}")
        print(f"  Subject: {msg['Subject']}")

except Exception as e:
    print(f"✗ Error sending email: {e}")
    exit(1)
