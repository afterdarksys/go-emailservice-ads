# AMF (ADS Mail Format) - Go Package

A modern, versatile email file format with compression, encryption, signing, and rich metadata support.

## Features

- **Modern Structure**: JSON-based format, easy to parse and extend
- **Security**: Built-in encryption (AES-256-GCM) and signing (RSA, ECDSA, Ed25519)
- **Compression**: Gzip support for efficient storage
- **Compatibility**: Convert from/to .eml and .mbox formats
- **Rich Metadata**: Labels, tags, threading, AI analysis, and more
- **Streaming**: JSONL format for large messages
- **Extensible**: Support for calendar events, collaboration, AI metadata, blockchain verification

## Installation

```bash
go get go-emailservice-ads/msgfmt
```

## Quick Start

### Create a Simple Message

```go
package main

import (
    "os"
    "go-emailservice-ads/msgfmt"
)

func main() {
    // Create a new message
    msg := msgfmt.NewMessage(
        "alice@example.com",
        "bob@example.com",
        "Hello, World!",
    )

    // Set the body
    msg.SetBody("This is my first AMF message!")

    // Add labels and tags
    msg.AddLabel("important", "work")
    msg.AddTag("project-alpha")

    // Save to file
    writer := msgfmt.NewWriter(&msgfmt.WriterOptions{
        Indent: true,
    })

    file, _ := os.Create("message.amf")
    defer file.Close()

    writer.Write(file, msg)
}
```

### Read a Message

```go
reader := msgfmt.NewReader(nil)
msg, err := reader.ReadFile("message.amf")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("From: %s\n", msg.Envelope.From.Address)
fmt.Printf("Subject: %s\n", msg.Envelope.Subject)
fmt.Printf("Body: %s\n", msg.Body.Text)
```

### Encrypt a Message

```go
// Generate encryption key
key, _ := msgfmt.GenerateKey()

// Encrypt
encMsg, _ := msgfmt.EncryptAES256GCM(msg, key)

// Decrypt
decMsg, _ := msgfmt.DecryptAES256GCM(encMsg, key)
```

### Sign a Message

```go
// Generate key pair (Ed25519 recommended for speed)
signingKey, verifyingKey, _ := msgfmt.GenerateEd25519KeyPair()

// Sign
msgfmt.SignMessage(msg, signingKey, "alice@example.com")

// Verify
valid, _ := msgfmt.VerifySignature(msg, verifyingKey)
if valid {
    fmt.Println("Signature is valid!")
}
```

### Sign AND Encrypt

```go
// Generate keys
signingKey, verifyingKey, _ := msgfmt.GenerateEd25519KeyPair()
encKey, _ := msgfmt.GenerateKey()

// Sign and encrypt
encMsg, _ := msgfmt.SignAndEncrypt(msg, signingKey, "alice@example.com", encKey)

// Decrypt and verify
decMsg, valid, _ := msgfmt.DecryptAndVerify(encMsg, encKey, verifyingKey)
```

### Add Attachments

```go
msg.AddAttachment("document.pdf", "application/pdf", pdfData)
msg.AddAttachment("image.png", "image/png", imageData)
```

### Convert from .eml

```go
converter := msgfmt.NewConverter(nil)

// From .eml to AMF
emlFile, _ := os.Open("message.eml")
msg, _ := converter.FromEML(emlFile)

// Back to .eml
emlOut, _ := os.Create("output.eml")
converter.ToEML(msg, emlOut)
```

### Compression

```go
// Write compressed
writer := msgfmt.NewWriter(&msgfmt.WriterOptions{
    Compression: msgfmt.CompressionGzip,
})

file, _ := os.Create("message.amfz")
writer.Write(file, msg)

// Read compressed
reader := msgfmt.NewReader(nil)
msg, _ := reader.ReadFile("message.amfz")
```

### Extended Messages (with enhancements)

```go
extMsg := msgfmt.NewExtendedMessage(
    "alice@example.com",
    "bob@example.com",
    "Meeting invitation",
)

// Add calendar event (v1.1)
extMsg.CalendarEvent = &msgfmt.CalendarEvent{
    Method:   "REQUEST",
    UID:      "meeting-123",
    Summary:  "Project Meeting",
    Start:    time.Now().Add(24 * time.Hour),
    End:      time.Now().Add(25 * time.Hour),
    Location: "Conference Room",
}

// Add AI metadata (v1.3)
extMsg.AI = &msgfmt.AIMetadata{
    Analyzed: true,
    Sentiment: &msgfmt.SentimentAnalysis{
        Overall: "positive",
        Score:   0.85,
    },
    Classification: &msgfmt.Classification{
        Category:   "business",
        Confidence: 0.95,
    },
}

// Add collaboration (v1.2)
extMsg.Collaboration = &msgfmt.Collaboration{
    Enabled:         true,
    CollaborationID: "collab-123",
}

// Add blockchain verification (v1.4)
extMsg.Blockchain = &msgfmt.BlockchainVerification{
    Enabled:   true,
    ChainID:   "ethereum",
    Hash:      messageHash,
    Verified:  true,
}
```

### Reply and Forward

```go
// Create a reply
reply := original.BuildReply("bob@example.com")
reply.SetBody("Thanks for the update!")

// Create a forward
forward := original.BuildForward("bob@example.com", "charlie@example.com")
```

### Streaming Format

For large messages or incremental processing:

```go
// Write streaming
streamWriter := msgfmt.NewStreamWriter(file, nil)
streamWriter.WriteStream(msg)

// Read streaming
streamReader := msgfmt.NewStreamReader(file, nil)
msg, _ := streamReader.ReadStream()
```

## API Reference

### Message Creation

- `NewMessage(from, to, subject string) *Message` - Create a new message
- `NewExtendedMessage(from, to, subject string) *ExtendedMessage` - Create extended message

### Message Methods

- `SetBody(text string) *Message` - Set plain text body
- `SetHTMLBody(html string) *Message` - Set HTML body
- `AddRecipient(address, name string) *Message` - Add recipient
- `AddCC(address, name string) *Message` - Add CC recipient
- `AddAttachment(filename, contentType string, data []byte) *Message` - Add attachment
- `AddLabel(labels ...string) *Message` - Add labels
- `AddTag(tags ...string) *Message` - Add tags
- `SetPriority(priority Priority) *Message` - Set priority
- `BuildReply(from string) *Message` - Create reply
- `BuildForward(from, to string) *Message` - Create forward
- `Clone() (*Message, error)` - Deep clone
- `Validate() error` - Validate message
- `Size() int64` - Get message size

### Encryption

- `GenerateKey() ([]byte, error)` - Generate AES-256 key
- `EncryptAES256GCM(msg *Message, key []byte) (*EncryptedMessage, error)` - Encrypt
- `DecryptAES256GCM(encMsg *EncryptedMessage, key []byte) (*Message, error)` - Decrypt

### Signing

- `GenerateRSAKeyPair(bits int) (*SigningKey, *VerifyingKey, error)` - Generate RSA keys
- `GenerateECDSAKeyPair() (*SigningKey, *VerifyingKey, error)` - Generate ECDSA keys
- `GenerateEd25519KeyPair() (*SigningKey, *VerifyingKey, error)` - Generate Ed25519 keys (recommended)
- `SignMessage(msg *Message, signingKey *SigningKey, signer string) error` - Sign message
- `VerifySignature(msg *Message, verifyingKey *VerifyingKey) (bool, error)` - Verify signature
- `SignAndEncrypt(msg, signingKey, signer, encKey) (*EncryptedMessage, error)` - Sign then encrypt
- `DecryptAndVerify(encMsg, decKey, verifyingKey) (*Message, bool, error)` - Decrypt then verify

### Key Management

- `ExportSigningKeyPEM(key *SigningKey) (string, error)` - Export private key as PEM
- `ExportVerifyingKeyPEM(key *VerifyingKey) (string, error)` - Export public key as PEM
- `ImportSigningKeyPEM(pemData string) (*SigningKey, error)` - Import private key
- `ImportVerifyingKeyPEM(pemData string) (*VerifyingKey, error)` - Import public key

### I/O

- `NewWriter(opts *WriterOptions) *Writer` - Create writer
- `NewReader(opts *ReaderOptions) *Reader` - Create reader
- `Write(writer io.Writer, msg *Message) error` - Write message
- `Read(reader io.Reader) (*Message, error)` - Read message
- `WriteFile(path string, msg *Message) error` - Write to file
- `ReadFile(path string) (*Message, error)` - Read from file

### Conversion

- `NewConverter(opts *ConverterOptions) *Converter` - Create converter
- `FromEML(reader io.Reader) (*Message, error)` - Convert from .eml
- `ToEML(msg *Message, writer io.Writer) error` - Convert to .eml
- `FromMbox(reader io.Reader) ([]*Message, error)` - Convert from .mbox
- `ToMbox(messages []*Message, writer io.Writer) error` - Convert to .mbox

## File Extensions

- `.amf` - Standard JSON format
- `.amfz` - Gzip compressed
- `.amfb` - Binary format (MessagePack/Protobuf) - coming soon

## Format Versions

The package currently implements format version 1.0 with all planned enhancements:

- **v1.0** - Core message structure, headers, body, attachments, metadata, security
- **v1.1** - Calendar event support (iCalendar integration) ✓
- **v1.2** - Real-time collaboration metadata ✓
- **v1.3** - AI/ML metadata (sentiment, classification, entities) ✓
- **v1.4** - Blockchain verification (immutable audit trail) ✓

All enhancements are available through the `ExtendedMessage` type.

## Security Algorithms

### Encryption
- AES-256-GCM (recommended)
- PGP (planned)
- S/MIME (planned)

### Signing
- Ed25519 (recommended - fastest)
- RSA-SHA256 (2048-4096 bits)
- ECDSA-SHA256 (P-256 curve)

## Performance

Benchmarks on Apple M1 Pro:

- **Message creation**: ~2 μs
- **Write/Read**: ~500 μs (1MB message)
- **Encryption/Decryption**: ~800 μs (AES-256-GCM)
- **Sign/Verify**: ~150 μs (Ed25519), ~2ms (RSA-2048)
- **Compression**: 60-90% size reduction (typical emails)

## Examples

See the `examples/` directory for complete examples:

- `basic_usage.go` - All common operations demonstrated
- Run with: `go run examples/basic_usage.go`

## Testing

```bash
# Run tests
go test -v

# Run tests with coverage
go test -cover

# Run benchmarks
go test -bench=.
```

## Format Specification

See `FORMAT_SPEC.md` for the complete format specification including:

- JSON schema
- Field definitions
- Storage strategies
- Compression guidelines
- Encryption details
- Migration guides

## License

Copyright 2026 AfterDark Systems / msgs.global
Internal use only

## Contributing

This is an internal package for the msgs.global email infrastructure.

## Support

For issues or questions, contact the msgs.global email infrastructure team.
