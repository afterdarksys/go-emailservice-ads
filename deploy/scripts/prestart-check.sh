#!/bin/bash
# Pre-start checks for go-emailservice-ads
# This script verifies the system is ready to run the email service

set -e

echo "Running pre-start checks..."

# Check configuration file exists
if [ ! -f /etc/goemailservices/config.yaml ]; then
    echo "ERROR: Configuration file not found at /etc/goemailservices/config.yaml"
    exit 1
fi

# Check data directory exists and is writable
if [ ! -d /var/lib/goemailservices ]; then
    echo "Creating data directory /var/lib/goemailservices"
    mkdir -p /var/lib/goemailservices
fi

if [ ! -w /var/lib/goemailservices ]; then
    echo "ERROR: Data directory /var/lib/goemailservices is not writable"
    exit 1
fi

# Check log directory exists and is writable
if [ ! -d /var/log/goemailservices ]; then
    echo "Creating log directory /var/log/goemailservices"
    mkdir -p /var/log/goemailservices
fi

if [ ! -w /var/log/goemailservices ]; then
    echo "ERROR: Log directory /var/log/goemailservices is not writable"
    exit 1
fi

# Check TLS certificates exist (if configured)
if grep -q "cert:" /etc/goemailservices/config.yaml; then
    CERT_FILE=$(grep "cert:" /etc/goemailservices/config.yaml | head -1 | awk '{print $2}' | tr -d '"')
    KEY_FILE=$(grep "key:" /etc/goemailservices/config.yaml | head -1 | awk '{print $2}' | tr -d '"')

    if [ -n "$CERT_FILE" ] && [ "$CERT_FILE" != "./data/certs/server.crt" ]; then
        if [ ! -f "$CERT_FILE" ]; then
            echo "ERROR: TLS certificate not found at $CERT_FILE"
            exit 1
        fi
    fi

    if [ -n "$KEY_FILE" ] && [ "$KEY_FILE" != "./data/certs/server.key" ]; then
        if [ ! -f "$KEY_FILE" ]; then
            echo "ERROR: TLS key not found at $KEY_FILE"
            exit 1
        fi
    fi
fi

# Check port availability
SMTP_PORT=$(grep "addr:" /etc/goemailservices/config.yaml | head -1 | awk -F: '{print $NF}' | tr -d ' "')
if [ -n "$SMTP_PORT" ]; then
    if netstat -tuln | grep -q ":$SMTP_PORT "; then
        echo "WARNING: Port $SMTP_PORT appears to be in use"
        # Don't fail - let the service handle it with better error messages
    fi
fi

# Check DNS resolution
if ! getent hosts gmail.com > /dev/null 2>&1; then
    echo "WARNING: DNS resolution may not be working properly"
fi

echo "Pre-start checks completed successfully"
exit 0
