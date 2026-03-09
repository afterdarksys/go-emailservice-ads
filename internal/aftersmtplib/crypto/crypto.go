package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	lru "github.com/hashicorp/golang-lru/v2"
)

var (
	pubKeyCache, _  = lru.New[string, *ecdh.PublicKey](50000)
	privKeyCache, _ = lru.New[string, *ecdh.PrivateKey](1000)
)

// IdentityKeys represents a DID's signing keys
type IdentityKeys struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// GenerateIdentityKeys creates a new Ed25519 keypair for signing
func GenerateIdentityKeys() (*IdentityKeys, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ed25519 keys: %w", err)
	}
	return &IdentityKeys{PublicKey: pub, PrivateKey: priv}, nil
}

// Sign payload using Ed25519 private key
func (k *IdentityKeys) Sign(payload []byte) []byte {
	return ed25519.Sign(k.PrivateKey, payload)
}

// Verify checks an Ed25519 signature
func Verify(pubKey ed25519.PublicKey, payload, sig []byte) bool {
	return ed25519.Verify(pubKey, payload, sig)
}

// Encrypt payload using X25519 and AES-GCM for a specific recipient
// Returns (ciphertext, ephemeralPublicKey, error)
func Encrypt(recipientPubKey []byte, plaintext []byte) ([]byte, []byte, error) {
	// 1. Parse recipient's public key
	var peerKey *ecdh.PublicKey
	if cachedKey, ok := pubKeyCache.Get(string(recipientPubKey)); ok {
		peerKey = cachedKey
	} else {
		var err error
		peerKey, err = ecdh.X25519().NewPublicKey(recipientPubKey)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid recipient public key: %w", err)
		}
		pubKeyCache.Add(string(recipientPubKey), peerKey)
	}

	// 2. Generate ephemeral key pair for this specific encryption
	ephemeralPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate ephemeral key: %w", err)
	}
	ephemeralPub := ephemeralPriv.PublicKey().Bytes()

	// 3. Compute shared secret
	sharedSecret, err := ephemeralPriv.ECDH(peerKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compute shared secret: %w", err)
	}

	// 4. Use shared secret to encrypt payload (AES-GCM)
	// Note: In a production system, use HKDF to derive the symmetric key from the shared secret.
	// For simplicity in this scaffold, we're using the shared secret directly as the 256-bit AES key.
	block, err := aes.NewCipher(sharedSecret) // sharedSecret is 32 bytes for X25519
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create gcm: %w", err)
	}

	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := aesgcm.Seal(nonce, nonce, plaintext, nil)

	return ciphertext, ephemeralPub, nil
}

// Decrypt payload using X25519 and AES-GCM
func Decrypt(myPrivKey []byte, ephemeralPubKey []byte, ciphertext []byte) ([]byte, error) {
	// 1. Parse my private key
	var myKey *ecdh.PrivateKey
	if cachedKey, ok := privKeyCache.Get(string(myPrivKey)); ok {
		myKey = cachedKey
	} else {
		var err error
		myKey, err = ecdh.X25519().NewPrivateKey(myPrivKey)
		if err != nil {
			return nil, fmt.Errorf("invalid private key: %w", err)
		}
		privKeyCache.Add(string(myPrivKey), myKey)
	}

	// 2. Parse ephemeral public key from sender
	var peerKey *ecdh.PublicKey
	if cachedKey, ok := pubKeyCache.Get(string(ephemeralPubKey)); ok {
		peerKey = cachedKey
	} else {
		var err error
		peerKey, err = ecdh.X25519().NewPublicKey(ephemeralPubKey)
		if err != nil {
			return nil, fmt.Errorf("invalid ephemeral public key: %w", err)
		}
		pubKeyCache.Add(string(ephemeralPubKey), peerKey)
	}

	// 3. Compute shared secret
	sharedSecret, err := myKey.ECDH(peerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute shared secret: %w", err)
	}

	// 4. Decrypt payload
	block, err := aes.NewCipher(sharedSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create gcm: %w", err)
	}

	nonceSize := aesgcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, encryptedMsg := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesgcm.Open(nil, nonce, encryptedMsg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt message: %w", err)
	}

	return plaintext, nil
}
