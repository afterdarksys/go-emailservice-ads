# AfterSMTP Protocol Setup Guide

## Overview

**AfterSMTP** is a revolutionary next-generation mail protocol built on modern internet standards. It replaces legacy SMTP with QUIC/HTTP3 transport, gRPC streaming, and blockchain-based message verification.

This service provides a **bridge** that allows traditional SMTP clients to seamlessly communicate with AfterSMTP-native systems.

## Key Features

- **QUIC Transport** - HTTP/3 based messaging with multiplexing, zero-RTT, and improved congestion control
- **gRPC Streaming** - Native gRPC bidirectional streaming for real-time message flow
- **Blockchain Ledger** - Substrate-based distributed ledger for immutable audit trails and message verification
- **Cryptographic Identity** - DID (Decentralized Identifier) based identity management
- **Legacy Bridge** - Seamless translation between legacy SMTP and modern AMP protocol
- **Enhanced Security** - MTA-STS, TLS Reporting, DANE/TLSA verification, and ARC support

---

## Architecture

### AfterSMTP Messaging Protocol (AMP)

AfterSMTP uses the **AMP (AfterSMTP Messaging Protocol)** which consists of:

1. **Transport Layer**: QUIC (UDP-based, multiplexed, encrypted by default)
2. **Application Layer**: gRPC with Protocol Buffers
3. **Identity Layer**: Blockchain-based DID verification
4. **Audit Layer**: Immutable ledger records

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                   Legacy SMTP Client                         │
│              (Thunderbird, Outlook, etc.)                    │
└───────────────────────────┬─────────────────────────────────┘
                            │ SMTP (Port 2525)
                            ▼
┌─────────────────────────────────────────────────────────────┐
│              go-emailservice-ads (Bridge)                    │
│                                                               │
│  ┌─────────────┐         ┌──────────────┐                   │
│  │ SMTP Server │────────▶│ Queue Manager│                   │
│  └─────────────┘         └──────┬───────┘                   │
│                                  │                            │
│                                  ▼                            │
│  ┌─────────────────────────────────────────────┐            │
│  │        AfterSMTP Bridge Service              │            │
│  │  • Legacy → AMP Translation                  │            │
│  │  • Identity Management (DID)                 │            │
│  │  • Blockchain Ledger Integration             │            │
│  └────────┬──────────────────────┬──────────────┘            │
└───────────┼──────────────────────┼───────────────────────────┘
            │                      │
            │ gRPC (4433)          │ QUIC (4434)
            ▼                      ▼
┌─────────────────────────────────────────────────────────────┐
│              Substrate Blockchain Ledger                     │
│         (Identity, Audit Trail, Verification)                │
└─────────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│          AfterSMTP Native Receivers                          │
│       (Future: Mobile apps, native clients)                  │
└─────────────────────────────────────────────────────────────┘
```

---

## Configuration

### 1. Enable AfterSMTP in config.yaml

The AfterSMTP configuration is in `config.yaml`:

```yaml
aftersmtp:
  enabled: true                              # Set to true to start AMP/QUIC/gRPC listeners
  ledger_url: "ws://127.0.0.1:9944"          # Substrate blockchain node WebSocket endpoint
  quic_addr: ":4434"                         # Port for QUIC (HTTP/3) transport
  grpc_addr: ":4433"                         # Port for native gRPC streaming
  fallback_db: "./data/fallback_ledger.db"   # SQLite fallback when blockchain unavailable
```

### 2. Set Up Substrate Blockchain Node

AfterSMTP requires a running Substrate blockchain node for identity management and audit trails.

#### Option A: Run Local Substrate Node (Development)

```bash
# Install Substrate (one-time setup)
curl https://getsubstrate.io -sSf | bash -s -- --fast

# Clone substrate-node-template
git clone https://github.com/substrate-developer-hub/substrate-node-template
cd substrate-node-template

# Build and run
cargo build --release
./target/release/node-template --dev --ws-external
```

The node will expose WebSocket endpoint at `ws://127.0.0.1:9944`.

#### Option B: Connect to Production Blockchain

Update `config.yaml` with production ledger URL:

```yaml
aftersmtp:
  ledger_url: "wss://ledger.msgs.global:9944"  # Production blockchain endpoint
```

### 3. Configure Ports

Ensure ports are available:

- **4433**: gRPC API (AfterSMTP native protocol)
- **4434**: QUIC transport (HTTP/3)
- **2525**: Legacy SMTP bridge (already configured)

```bash
# Check port availability
lsof -i :4433
lsof -i :4434
```

### 4. TLS Certificates

AfterSMTP QUIC transport requires TLS certificates (reuses SMTP certs):

```yaml
server:
  tls:
    cert: "./data/certs/server.crt"
    key: "./data/certs/server.key"
```

Generate self-signed certificates for testing:

```bash
openssl req -x509 -newkey rsa:4096 -nodes \
  -keyout ./data/certs/server.key \
  -out ./data/certs/server.crt \
  -days 365 -subj "/CN=localhost.local"
```

---

## How It Works

### Message Flow: Legacy SMTP → AfterSMTP

1. **SMTP Client Connects** to port 2525 (legacy SMTP)
2. **SMTP Server** receives message, queues it
3. **Queue Manager** processes message
4. **AfterSMTP Bridge** intercepts outbound messages:
   - Generates **DID** for sender/recipient
   - **Signs message** with server's cryptographic identity
   - **Publishes to blockchain ledger** for audit trail
   - **Encrypts payload** (optional)
   - **Sends via QUIC or gRPC** to destination
5. **Destination AfterSMTP Node** receives, verifies, delivers

### Message Flow: AfterSMTP Native → Platform

1. **Native Client** connects via gRPC (4433) or QUIC (4434)
2. **AfterSMTP Bridge** receives AMP message
3. Bridge **parses DID**, maps to email format (`user@domain.com`)
4. Bridge **enqueues to internal queue** (TierInt)
5. **Queue Manager** delivers to mailbox
6. User retrieves via **IMAP** or **AfterSMTP native client**

### Identity Management (DID)

AfterSMTP uses **Decentralized Identifiers (DIDs)**:

**Format**: `did:aftersmtp:domain:username`

**Example**: `did:aftersmtp:msgs.global:john`

Each identity has:
- **Signing Key** (Ed25519) - Message authentication
- **Encryption Key** (X25519) - End-to-end encryption
- **Blockchain Record** - Immutable identity verification

### Blockchain Audit Trail

Every message creates an immutable ledger entry:

```json
{
  "message_id": "unique-message-id",
  "sender_did": "did:aftersmtp:msgs.global:alice",
  "recipient_did": "did:aftersmtp:msgs.global:bob",
  "timestamp": "2026-03-09T12:34:56Z",
  "content_hash": "sha256:abc123...",
  "signature": "ed25519:signature...",
  "block_number": 12345,
  "tx_hash": "0xabc..."
}
```

---

## Testing AfterSMTP

### 1. Start the Service

```bash
# Start with AfterSMTP enabled
./bin/goemailservices -config config.yaml
```

Expected log output:

```
INFO  Initializing AfterSMTP Bridge Service  {"ledgerUrl": "ws://127.0.0.1:9944"}
INFO  AfterSMTP Server Node Identity generated  {"did": "did:aftersmtp:localhost.local:node_1"}
INFO  Registered mock blockchain DID for local user  {"username": "testuser", "did": "did:aftersmtp:localhost.local:testuser"}
INFO  Starting AfterSMTP gRPC Bridge Ingress  {"addr": ":4433"}
INFO  Starting AfterSMTP QUIC Bridge Ingress  {"addr": ":4434"}
```

### 2. Verify Blockchain Connection

```bash
# Check ledger connectivity
curl -H "Content-Type: application/json" -d '{"id":1, "jsonrpc":"2.0", "method": "system_health"}' http://127.0.0.1:9933

# Expected response:
# {"jsonrpc":"2.0","result":{"isSyncing":false,"peers":0,"shouldHavePeers":true},"id":1}
```

### 3. Test Legacy SMTP → AfterSMTP

Send a test email via SMTP:

```bash
# Send via telnet
telnet localhost 2525
EHLO test.local
MAIL FROM:<sender@localhost.local>
RCPT TO:<testuser@localhost.local>
DATA
Subject: AfterSMTP Test

This message will be logged to the blockchain ledger.
.
QUIT
```

Check logs for blockchain publishing:

```
INFO  Received AMTP Native Message  {"id": "...", "from": "did:aftersmtp:localhost.local:sender", "to": "did:aftersmtp:localhost.local:testuser"}
```

### 4. Test Native gRPC Client

Use `grpcurl` to test native protocol:

```bash
# Install grpcurl
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# List services
grpcurl -plaintext localhost:4433 list

# Expected output:
# aftersmtp.protocol.amp.AMPServer
# aftersmtp.protocol.client.ClientAPI
```

---

## Advanced Configuration

### Fallback Database

When blockchain is unavailable, AfterSMTP uses SQLite fallback:

```yaml
aftersmtp:
  fallback_db: "./data/fallback_ledger.db"
```

The service automatically:
1. Detects blockchain unavailability
2. Writes records to SQLite
3. Syncs to blockchain when it reconnects

### Multi-Region Deployment

For multi-region setups with shared blockchain:

```yaml
# Region A (US-East)
aftersmtp:
  ledger_url: "wss://ledger-us.msgs.global:9944"

# Region B (EU-West)
aftersmtp:
  ledger_url: "wss://ledger-eu.msgs.global:9944"
```

Both regions share the same distributed ledger.

### Custom Identity Keys

For production, use persistent identity keys:

```go
// Generate once, store securely
serverKeys, _ := aftercrypto.GenerateIdentityKeys()

// Save to secure storage
saveKeys("server-identity.key", serverKeys)

// Load on startup
serverKeys = loadKeys("server-identity.key")
```

---

## Library API (Go)

### Using AfterSMTP Library in Your Code

```go
import (
    afteramp "github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/protocol/amp"
    afterledger "github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/ledger"
    aftercrypto "github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/crypto"
)

// Initialize ledger connection
ledger, err := afterledger.NewSubstrateLedger("ws://127.0.0.1:9944")

// Generate identity
keys, _ := aftercrypto.GenerateIdentityKeys()
did := afterledger.FormatDID("example.com", "alice")

// Publish identity to blockchain
identityRecord := &afterledger.IdentityRecord{
    DID:              did,
    SigningPublicKey: keys.PublicKey,
    EncryptionPubKey: encryptionKey,
}
ledger.PublishIdentity(identityRecord)

// Send AMP message
msg := &afteramp.AMPMessage{
    Headers: &afteramp.MessageHeaders{
        MessageId:    "unique-id",
        SenderDid:    "did:aftersmtp:example.com:alice",
        RecipientDid: "did:aftersmtp:example.com:bob",
    },
    EncryptedPayload: encryptedData,
}
```

### Key Library Packages

- **`aftersmtplib/protocol/amp`** - AMP protocol implementation
- **`aftersmtplib/protocol/client`** - Client API
- **`aftersmtplib/protocol/legacy`** - Legacy SMTP bridge
- **`aftersmtplib/ledger`** - Blockchain ledger integration
- **`aftersmtplib/crypto`** - Cryptographic operations (signing, encryption)
- **`aftersmtplib/security`** - Security features (MTA-STS, DANE, TLS reporting)
- **`aftersmtplib/routing`** - Message routing
- **`aftersmtplib/dns`** - DNS utilities
- **`aftersmtplib/telemetry`** - Observability

---

## Troubleshooting

### "Failed to initialize substrate ledger"

**Possible causes:**

1. **Blockchain node not running**
   ```bash
   # Check if node is running
   curl http://127.0.0.1:9933
   ```

2. **Wrong WebSocket URL**
   - Verify `ledger_url` in config.yaml
   - Check firewall rules

3. **Network connectivity**
   ```bash
   # Test WebSocket connection
   wscat -c ws://127.0.0.1:9944
   ```

### "Port already in use"

```bash
# Check what's using ports
lsof -i :4433
lsof -i :4434

# Kill process if needed
kill <PID>
```

### "TLS certificate error"

Ensure certificates are valid:

```bash
# Verify certificate
openssl x509 -in ./data/certs/server.crt -text -noout

# Check if cert matches key
openssl x509 -noout -modulus -in server.crt | openssl md5
openssl rsa -noout -modulus -in server.key | openssl md5
# Both should match
```

### Enable Debug Logging

```yaml
logging:
  level: "debug"
```

Then filter for AfterSMTP logs:

```bash
./bin/goemailservices -config config.yaml 2>&1 | grep -i "aftersmtp"
```

---

## Performance Considerations

### QUIC vs gRPC

- **QUIC** (Port 4434)
  - Better for high-latency networks
  - Multiplexing without head-of-line blocking
  - Zero-RTT connection establishment
  - Recommended for internet-facing connections

- **gRPC** (Port 4433)
  - Better for internal/datacenter use
  - Lower overhead in low-latency networks
  - Easier debugging (HTTP/2)
  - Recommended for internal service-to-service

### Blockchain Performance

- **Block time**: ~6 seconds (Substrate default)
- **Transaction finality**: ~12 seconds (2 blocks)
- **Throughput**: ~1000 messages/second per node

For high volume:
- Use **fallback database** to buffer writes
- Deploy **multiple blockchain validators**
- Consider **sharding** for horizontal scaling

---

## Security Considerations

### Cryptographic Guarantees

1. **Identity Authentication** - Ed25519 signatures
2. **Message Integrity** - SHA256 content hashing
3. **Transport Security** - TLS 1.3 for QUIC
4. **End-to-End Encryption** - X25519 key exchange (optional)

### Network Security

```yaml
# Firewall rules
# Allow inbound:
#   - TCP 4433 (gRPC) - internal only
#   - UDP 4434 (QUIC) - internet if needed
#   - TCP 9944 (Blockchain WS) - internal only
```

### Key Management

- **Server Identity Keys** - Store in HSM or secure vault
- **Rotate keys** every 90 days
- **Backup keys** to secure offline storage
- **Use separate keys** per deployment/region

---

## Migration from Legacy SMTP

### Phase 1: Bridge Mode (Current)

Run AfterSMTP bridge alongside legacy SMTP:

```yaml
server:
  addr: ":2525"  # Legacy SMTP
aftersmtp:
  enabled: true
  grpc_addr: ":4433"  # Native protocol
  quic_addr: ":4434"
```

Clients can use **either** protocol.

### Phase 2: Dual Mode

- Internal systems → AfterSMTP native
- External systems → Legacy SMTP bridge

### Phase 3: Full AfterSMTP

- All systems → AfterSMTP native
- Legacy SMTP deprecated

---

## Integration with msgs.global

AfterSMTP is the **native protocol** for the msgs.global enterprise mail infrastructure:

- **Identity Management** - Integrated with After Dark Systems Directory
- **Multi-Region** - Shared blockchain across all regions
- **Audit Compliance** - Immutable message records for regulatory compliance
- **API Access** - Native gRPC APIs for integration

Learn more: https://github.com/afterdarksys/go-emailservice-ads

---

## Support & Documentation

- **GitHub Repository**: https://github.com/afterdarksys/go-emailservice-ads
- **Issues**: https://github.com/afterdarksys/go-emailservice-ads/issues
- **Substrate Docs**: https://substrate.io/
- **QUIC Spec**: https://quicwg.org/
- **gRPC Docs**: https://grpc.io/

---

**Status**: ✅ AfterSMTP Integration Complete

**Version**: 2.1.0

**Last Updated**: 2026-03-09
