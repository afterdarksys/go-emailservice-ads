package ledger

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// MockLedger is an in-memory implementation of the Ledger interface for testing
type MockLedger struct {
	mu         sync.RWMutex
	identities map[string]*IdentityRecord
	proofs     map[string]bool
}

// NewMockLedger initializes a fresh test ledger
func NewMockLedger() *MockLedger {
	return &MockLedger{
		identities: make(map[string]*IdentityRecord),
		proofs:     make(map[string]bool),
	}
}

func (m *MockLedger) ResolveDID(did string) (*IdentityRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	record, exists := m.identities[did]
	if !exists {
		return nil, ErrIdentityNotFound
	}
	if record.Revoked {
		return nil, ErrKeyRevoked
	}
	return record, nil
}

func (m *MockLedger) PublishIdentity(record *IdentityRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.identities[record.DID] = record
	return nil
}

func (m *MockLedger) CreateProof(receiptHash string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Simulate generating a blockchain transaction ID
	txBytes := make([]byte, 32)
	_, _ = rand.Read(txBytes)
	txID := "tx_" + hex.EncodeToString(txBytes)

	m.proofs[txID] = true
	return txID, nil
}

func (m *MockLedger) VerifyProof(proof string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.proofs[proof]
}
