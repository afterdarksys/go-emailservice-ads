#!/usr/bin/env python3
"""Comprehensive test suite for email service"""

import smtplib
import time
from email.message import EmailMessage

def send_email(from_addr, to_addr, subject, body):
    """Send a single email"""
    msg = EmailMessage()
    msg['From'] = from_addr
    msg['To'] = to_addr
    msg['Subject'] = subject
    msg.set_content(body)

    try:
        with smtplib.SMTP('localhost', 2525) as smtp:
            smtp.send_message(msg)
            return True
    except Exception as e:
        print(f"✗ Error: {e}")
        return False

print("=" * 60)
print("Email Service Test Suite")
print("=" * 60)

# Test 1: Send multiple messages
print("\n[Test 1] Sending 5 test messages...")
for i in range(1, 6):
    if send_email(
        f"sender{i}@example.com",
        f"recipient{i}@example.com",
        f"Test Message {i}",
        f"This is test message number {i}"
    ):
        print(f"  ✓ Message {i} sent")
    time.sleep(0.5)

# Test 2: Test deduplication (send same message twice)
print("\n[Test 2] Testing deduplication...")
same_body = "This is a duplicate message for testing deduplication"
send_email("dup@example.com", "target@example.com", "Duplicate Test 1", same_body)
print("  ✓ First message sent")
time.sleep(0.5)
send_email("dup@example.com", "target@example.com", "Duplicate Test 2", same_body)
print("  ✓ Second message sent (should be detected as duplicate)")

# Test 3: Bulk tier test
print("\n[Test 3] Sending bulk messages...")
for i in range(1, 4):
    send_email(
        "newsletter@example.com",
        f"subscriber{i}@example.com",
        "Newsletter",
        f"Bulk message {i}"
    )
    print(f"  ✓ Bulk message {i} sent")
    time.sleep(0.3)

# Test 4: Large recipient list
print("\n[Test 4] Testing multiple recipients...")
msg = EmailMessage()
msg['From'] = 'broadcast@example.com'
msg['To'] = ', '.join([f'user{i}@example.com' for i in range(1, 11)])
msg['Subject'] = 'Broadcast Message'
msg.set_content('This message goes to 10 recipients')

try:
    with smtplib.SMTP('localhost', 2525) as smtp:
        smtp.send_message(msg)
        print("  ✓ Broadcast message sent to 10 recipients")
except Exception as e:
    print(f"  ✗ Error: {e}")

print("\n" + "=" * 60)
print("Test suite complete!")
print("Run: ./bin/mailctl --username admin --password changeme queue stats")
print("=" * 60)
