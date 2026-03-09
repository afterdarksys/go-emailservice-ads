package ledger

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrIdentityNotFound = errors.New("identity not found on ledger")
	ErrKeyRevoked       = errors.New("key revoked on ledger")
)

// IdentityRecord represents the blockchain state for a DID
type IdentityRecord struct {
	DID              string            // e.g., did:aftersmtp:example.com:user
	SigningPublicKey ed25519.PublicKey // For verifying payload signatures
	EncryptionPubKey []byte            // X25519 public key for encrypting messages to this user
	Revoked          bool
}

// Ledger defines the interface for Decentralized Identity (DID) and proof anchoring
type Ledger interface {
	// ResolveDID looks up a public identity
	ResolveDID(did string) (*IdentityRecord, error)

	// PublishIdentity creates or updates an identity on the blockchain
	PublishIdentity(record *IdentityRecord) error

	// CreateProof anchors a message transit event (hash) to the ledger
	CreateProof(receiptHash string) (string, error)

	// VerifyProof checks if a transit event exists on the chain
	VerifyProof(proof string) bool
}

// ParseDID extracts the domain and user from a DID string
func ParseDID(did string) (user, domain string, err error) {
	if !strings.HasPrefix(did, "did:aftersmtp:") {
		return "", "", errors.New("invalid DID format")
	}
	parts := strings.Split(strings.TrimPrefix(did, "did:aftersmtp:"), ":")
	if len(parts) != 2 {
		return "", "", errors.New("invalid DID parts, expected domain:user")
	}
	return parts[1], parts[0], nil
}

// FormatDID creates a standard DID string
func FormatDID(domain, user string) string {
	return fmt.Sprintf("did:aftersmtp:%s:%s", domain, user)
}
