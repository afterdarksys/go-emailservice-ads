package msgfmt

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// NewMessage creates a new message with required fields
func NewMessage(from, to, subject string) *Message {
	return &Message{
		Version: Version,
		Type:    TypeMessage,
		ID:      uuid.New().String(),
		Envelope: &Envelope{
			MessageID: fmt.Sprintf("<%s@generated.local>", uuid.New().String()),
			From: &Address{
				Address: from,
			},
			To: []*Address{
				{Address: to},
			},
			Subject:  subject,
			Date:     time.Now(),
			Priority: PriorityNormal,
		},
	}
}

// NewExtendedMessage creates a new extended message
func NewExtendedMessage(from, to, subject string) *ExtendedMessage {
	return &ExtendedMessage{
		Message: NewMessage(from, to, subject),
	}
}

// SetBody sets the message body text
func (m *Message) SetBody(text string) *Message {
	m.Body = &Body{
		Text: text,
		Size: int64(len(text)),
		Hash: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(text))),
	}
	return m
}

// SetHTMLBody sets the message HTML body
func (m *Message) SetHTMLBody(html string) *Message {
	if m.Body == nil {
		m.Body = &Body{}
	}
	m.Body.HTML = html
	return m
}

// AddRecipient adds a recipient to the To list
func (m *Message) AddRecipient(address, name string) *Message {
	m.Envelope.To = append(m.Envelope.To, &Address{
		Address: address,
		Name:    name,
	})
	return m
}

// AddCC adds a CC recipient
func (m *Message) AddCC(address, name string) *Message {
	m.Envelope.CC = append(m.Envelope.CC, &Address{
		Address: address,
		Name:    name,
	})
	return m
}

// AddAttachment adds an inline attachment
func (m *Message) AddAttachment(filename, contentType string, data []byte) *Message {
	hash := sha256.Sum256(data)
	att := &Attachment{
		ID:          uuid.New().String(),
		Filename:    filename,
		ContentType: contentType,
		Size:        int64(len(data)),
		Hash:        fmt.Sprintf("sha256:%x", hash),
		Storage:     StorageInline,
		Data:        base64.StdEncoding.EncodeToString(data),
		Disposition: DispositionAttachment,
	}
	m.Attachments = append(m.Attachments, att)
	return m
}

// AddLabel adds a label to the message
func (m *Message) AddLabel(labels ...string) *Message {
	if m.Metadata == nil {
		m.Metadata = &Metadata{}
	}
	m.Metadata.Labels = append(m.Metadata.Labels, labels...)
	return m
}

// AddTag adds a tag to the message
func (m *Message) AddTag(tags ...string) *Message {
	if m.Metadata == nil {
		m.Metadata = &Metadata{}
	}
	m.Metadata.Tags = append(m.Metadata.Tags, tags...)
	return m
}

// SetPriority sets the message priority
func (m *Message) SetPriority(priority Priority) *Message {
	m.Envelope.Priority = priority
	return m
}

// SetThreadID sets the thread ID
func (m *Message) SetThreadID(threadID string) *Message {
	m.Envelope.ThreadID = threadID
	return m
}

// SetInReplyTo sets the In-Reply-To header
func (m *Message) SetInReplyTo(messageID string) *Message {
	m.Envelope.InReplyTo = messageID
	return m
}

// Clone creates a deep copy of the message
func (m *Message) Clone() (*Message, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	var clone Message
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, err
	}

	// Generate new ID for clone
	clone.ID = uuid.New().String()

	return &clone, nil
}

// Size returns the approximate size of the message in bytes
func (m *Message) Size() int64 {
	data, err := json.Marshal(m)
	if err != nil {
		return 0
	}
	return int64(len(data))
}

// HasAttachments returns true if the message has attachments
func (m *Message) HasAttachments() bool {
	return len(m.Attachments) > 0
}

// IsEncrypted returns true if the message is encrypted
func (m *Message) IsEncrypted() bool {
	return m.Encrypted || (m.Security != nil && m.Security.Encrypted)
}

// IsSigned returns true if the message is signed
func (m *Message) IsSigned() bool {
	return m.Security != nil && m.Security.Signed
}

// GetAttachment returns an attachment by ID
func (m *Message) GetAttachment(id string) *Attachment {
	for _, att := range m.Attachments {
		if att.ID == id {
			return att
		}
	}
	return nil
}

// GetAttachmentByFilename returns an attachment by filename
func (m *Message) GetAttachmentByFilename(filename string) *Attachment {
	for _, att := range m.Attachments {
		if att.Filename == filename {
			return att
		}
	}
	return nil
}

// ExtractText returns the plain text body
func (m *Message) ExtractText() string {
	if m.Body != nil {
		return m.Body.Text
	}
	return ""
}

// ExtractHTML returns the HTML body
func (m *Message) ExtractHTML() string {
	if m.Body != nil {
		return m.Body.HTML
	}
	return ""
}

// CalculateHash calculates the SHA256 hash of the message
func (m *Message) CalculateHash() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", hash), nil
}

// Validate performs validation on the message
func (m *Message) Validate() error {
	if m.Version == "" {
		return fmt.Errorf("missing version")
	}
	if m.ID == "" {
		return fmt.Errorf("missing message ID")
	}
	if m.Envelope == nil {
		return fmt.Errorf("missing envelope")
	}
	if m.Envelope.MessageID == "" {
		return fmt.Errorf("missing envelope message_id")
	}
	if m.Envelope.From == nil {
		return fmt.Errorf("missing sender")
	}
	if len(m.Envelope.To) == 0 {
		return fmt.Errorf("missing recipients")
	}
	return nil
}

// EncryptAES256GCM encrypts a message using AES-256-GCM
func EncryptAES256GCM(msg *Message, key []byte) (*EncryptedMessage, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes for AES-256")
	}

	// Marshal message to JSON
	plaintext, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Create encrypted message
	encMsg := &EncryptedMessage{
		Version: Version,
		Type:    TypeEncryptedMessage,
		ID:      msg.ID,
		Encryption: &EncryptionInfo{
			Algorithm: "aes-256-gcm",
			Nonce:     base64.StdEncoding.EncodeToString(nonce),
		},
		EncryptedData: base64.StdEncoding.EncodeToString(ciphertext),
	}

	return encMsg, nil
}

// DecryptAES256GCM decrypts an encrypted message using AES-256-GCM
func DecryptAES256GCM(encMsg *EncryptedMessage, key []byte) (*Message, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes for AES-256")
	}

	// Decode nonce
	nonce, err := base64.StdEncoding.DecodeString(encMsg.Encryption.Nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decode nonce: %w", err)
	}

	// Decode ciphertext
	ciphertext, err := base64.StdEncoding.DecodeString(encMsg.EncryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	// Unmarshal message
	var msg Message
	if err := json.Unmarshal(plaintext, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return &msg, nil
}

// GenerateKey generates a random 32-byte key for AES-256
func GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	return key, nil
}

// BuildReply creates a reply message
func (m *Message) BuildReply(from string) *Message {
	reply := NewMessage(from, m.Envelope.From.Address, "Re: "+m.Envelope.Subject)
	reply.SetInReplyTo(m.Envelope.MessageID)

	// Set thread ID
	if m.Envelope.ThreadID != "" {
		reply.SetThreadID(m.Envelope.ThreadID)
	} else {
		reply.SetThreadID(m.Envelope.MessageID)
	}

	// Add references
	reply.Envelope.References = append([]string{}, m.Envelope.References...)
	reply.Envelope.References = append(reply.Envelope.References, m.Envelope.MessageID)

	return reply
}

// BuildForward creates a forwarded message
func (m *Message) BuildForward(from, to string) *Message {
	forward := NewMessage(from, to, "Fwd: "+m.Envelope.Subject)

	// Include original message body
	originalText := fmt.Sprintf(
		"\n\n---------- Forwarded message ---------\nFrom: %s\nDate: %s\nSubject: %s\n\n%s",
		m.Envelope.From.Address,
		m.Envelope.Date.Format(time.RFC1123),
		m.Envelope.Subject,
		m.ExtractText(),
	)

	forward.SetBody(originalText)

	// Copy attachments as references
	for _, att := range m.Attachments {
		forward.Attachments = append(forward.Attachments, att)
	}

	return forward
}

// ToJSON converts the message to JSON string
func (m *Message) ToJSON(indent bool) (string, error) {
	var data []byte
	var err error

	if indent {
		data, err = json.MarshalIndent(m, "", "  ")
	} else {
		data, err = json.Marshal(m)
	}

	if err != nil {
		return "", err
	}

	return string(data), nil
}

// FromJSON parses a message from JSON string
func FromJSON(jsonStr string) (*Message, error) {
	var msg Message
	if err := json.Unmarshal([]byte(jsonStr), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// =========================================
// Digital Signature Functions
// =========================================

// SigningKey represents a private key for signing
type SigningKey struct {
	Algorithm string
	PrivateKey interface{} // *rsa.PrivateKey, *ecdsa.PrivateKey, or ed25519.PrivateKey
}

// VerifyingKey represents a public key for verification
type VerifyingKey struct {
	Algorithm string
	PublicKey interface{} // *rsa.PublicKey, *ecdsa.PublicKey, or ed25519.PublicKey
}

// SignMessage signs a message and adds signature to Security section
func SignMessage(msg *Message, signingKey *SigningKey, signer string) error {
	// Calculate message hash
	hash, err := msg.CalculateHash()
	if err != nil {
		return fmt.Errorf("failed to calculate hash: %w", err)
	}

	// Generate signature based on algorithm
	var signatureData string
	var algorithm string

	switch key := signingKey.PrivateKey.(type) {
	case *rsa.PrivateKey:
		algorithm = "rsa-sha256"
		hashBytes := sha256.Sum256([]byte(hash))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashBytes[:])
		if err != nil {
			return fmt.Errorf("RSA signing failed: %w", err)
		}
		signatureData = base64.StdEncoding.EncodeToString(signature)

	case *ecdsa.PrivateKey:
		algorithm = "ecdsa-sha256"
		hashBytes := sha256.Sum256([]byte(hash))
		r, s, err := ecdsa.Sign(rand.Reader, key, hashBytes[:])
		if err != nil {
			return fmt.Errorf("ECDSA signing failed: %w", err)
		}
		signature := append(r.Bytes(), s.Bytes()...)
		signatureData = base64.StdEncoding.EncodeToString(signature)

	case ed25519.PrivateKey:
		algorithm = "ed25519"
		signature := ed25519.Sign(key, []byte(hash))
		signatureData = base64.StdEncoding.EncodeToString(signature)

	default:
		return fmt.Errorf("unsupported key type")
	}

	// Add signature to message
	if msg.Security == nil {
		msg.Security = &Security{}
	}

	msg.Security.Signed = true
	msg.Security.Signature = &SignatureInfo{
		Algorithm:     algorithm,
		Signer:        signer,
		KeyID:         generateKeyID(signingKey),
		Timestamp:     time.Now(),
		SignatureData: signatureData,
		Valid:         true,
	}

	return nil
}

// VerifySignature verifies a message signature
func VerifySignature(msg *Message, verifyingKey *VerifyingKey) (bool, error) {
	if msg.Security == nil || msg.Security.Signature == nil {
		return false, fmt.Errorf("message is not signed")
	}

	// Save and temporarily remove signature to recalculate hash
	originalSig := msg.Security.Signature
	msg.Security.Signature = nil

	hash, err := msg.CalculateHash()
	if err != nil {
		msg.Security.Signature = originalSig
		return false, fmt.Errorf("failed to calculate hash: %w", err)
	}

	// Restore signature
	msg.Security.Signature = originalSig

	// Decode signature
	signatureBytes, err := base64.StdEncoding.DecodeString(originalSig.SignatureData)
	if err != nil {
		return false, fmt.Errorf("failed to decode signature: %w", err)
	}

	// Verify based on algorithm
	var valid bool

	switch key := verifyingKey.PublicKey.(type) {
	case *rsa.PublicKey:
		hashBytes := sha256.Sum256([]byte(hash))
		err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hashBytes[:], signatureBytes)
		valid = (err == nil)

	case *ecdsa.PublicKey:
		hashBytes := sha256.Sum256([]byte(hash))
		// Split signature into r and s
		r := new(big.Int).SetBytes(signatureBytes[:len(signatureBytes)/2])
		s := new(big.Int).SetBytes(signatureBytes[len(signatureBytes)/2:])
		valid = ecdsa.Verify(key, hashBytes[:], r, s)

	case ed25519.PublicKey:
		valid = ed25519.Verify(key, []byte(hash), signatureBytes)

	default:
		return false, fmt.Errorf("unsupported key type")
	}

	// Update signature validity
	msg.Security.Signature.Valid = valid

	return valid, nil
}

// GenerateRSAKeyPair generates an RSA key pair for signing
func GenerateRSAKeyPair(bits int) (*SigningKey, *VerifyingKey, error) {
	if bits < 2048 {
		return nil, nil, fmt.Errorf("RSA key must be at least 2048 bits")
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	signingKey := &SigningKey{
		Algorithm:  "rsa-sha256",
		PrivateKey: privateKey,
	}

	verifyingKey := &VerifyingKey{
		Algorithm: "rsa-sha256",
		PublicKey: &privateKey.PublicKey,
	}

	return signingKey, verifyingKey, nil
}

// GenerateECDSAKeyPair generates an ECDSA key pair for signing
func GenerateECDSAKeyPair() (*SigningKey, *VerifyingKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate ECDSA key: %w", err)
	}

	signingKey := &SigningKey{
		Algorithm:  "ecdsa-sha256",
		PrivateKey: privateKey,
	}

	verifyingKey := &VerifyingKey{
		Algorithm: "ecdsa-sha256",
		PublicKey: &privateKey.PublicKey,
	}

	return signingKey, verifyingKey, nil
}

// GenerateEd25519KeyPair generates an Ed25519 key pair for signing
func GenerateEd25519KeyPair() (*SigningKey, *VerifyingKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate Ed25519 key: %w", err)
	}

	signingKey := &SigningKey{
		Algorithm:  "ed25519",
		PrivateKey: privateKey,
	}

	verifyingKey := &VerifyingKey{
		Algorithm: "ed25519",
		PublicKey: publicKey,
	}

	return signingKey, verifyingKey, nil
}

// ExportSigningKeyPEM exports a signing key to PEM format
func ExportSigningKeyPEM(key *SigningKey) (string, error) {
	var privateKeyBytes []byte
	var err error
	var blockType string

	switch k := key.PrivateKey.(type) {
	case *rsa.PrivateKey:
		privateKeyBytes = x509.MarshalPKCS1PrivateKey(k)
		blockType = "RSA PRIVATE KEY"
	case *ecdsa.PrivateKey:
		privateKeyBytes, err = x509.MarshalECPrivateKey(k)
		if err != nil {
			return "", fmt.Errorf("failed to marshal ECDSA key: %w", err)
		}
		blockType = "EC PRIVATE KEY"
	case ed25519.PrivateKey:
		privateKeyBytes, err = x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			return "", fmt.Errorf("failed to marshal Ed25519 key: %w", err)
		}
		blockType = "PRIVATE KEY"
	default:
		return "", fmt.Errorf("unsupported key type")
	}

	block := &pem.Block{
		Type:  blockType,
		Bytes: privateKeyBytes,
	}

	return string(pem.EncodeToMemory(block)), nil
}

// ExportVerifyingKeyPEM exports a verifying key to PEM format
func ExportVerifyingKeyPEM(key *VerifyingKey) (string, error) {
	var publicKeyBytes []byte
	var err error

	switch k := key.PublicKey.(type) {
	case *rsa.PublicKey:
		publicKeyBytes, err = x509.MarshalPKIXPublicKey(k)
	case *ecdsa.PublicKey:
		publicKeyBytes, err = x509.MarshalPKIXPublicKey(k)
	case ed25519.PublicKey:
		publicKeyBytes, err = x509.MarshalPKIXPublicKey(k)
	default:
		return "", fmt.Errorf("unsupported key type")
	}

	if err != nil {
		return "", fmt.Errorf("failed to marshal public key: %w", err)
	}

	block := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}

	return string(pem.EncodeToMemory(block)), nil
}

// ImportSigningKeyPEM imports a signing key from PEM format
func ImportSigningKeyPEM(pemData string) (*SigningKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	var privateKey interface{}
	var algorithm string
	var err error

	switch block.Type {
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		algorithm = "rsa-sha256"
	case "EC PRIVATE KEY":
		privateKey, err = x509.ParseECPrivateKey(block.Bytes)
		algorithm = "ecdsa-sha256"
	case "PRIVATE KEY":
		privateKey, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		algorithm = "ed25519"
	default:
		return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return &SigningKey{
		Algorithm:  algorithm,
		PrivateKey: privateKey,
	}, nil
}

// ImportVerifyingKeyPEM imports a verifying key from PEM format
func ImportVerifyingKeyPEM(pemData string) (*VerifyingKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	var algorithm string
	switch publicKey.(type) {
	case *rsa.PublicKey:
		algorithm = "rsa-sha256"
	case *ecdsa.PublicKey:
		algorithm = "ecdsa-sha256"
	case ed25519.PublicKey:
		algorithm = "ed25519"
	default:
		return nil, fmt.Errorf("unsupported public key type")
	}

	return &VerifyingKey{
		Algorithm: algorithm,
		PublicKey: publicKey,
	}, nil
}

// generateKeyID generates a key identifier
func generateKeyID(key *SigningKey) string {
	data, _ := json.Marshal(key.PrivateKey)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:8])
}

// SignAndEncrypt signs then encrypts a message
func SignAndEncrypt(msg *Message, signingKey *SigningKey, signer string, encryptionKey []byte) (*EncryptedMessage, error) {
	// First, sign the message
	if err := SignMessage(msg, signingKey, signer); err != nil {
		return nil, fmt.Errorf("signing failed: %w", err)
	}

	// Then encrypt the signed message
	return EncryptAES256GCM(msg, encryptionKey)
}

// DecryptAndVerify decrypts then verifies a message
func DecryptAndVerify(encMsg *EncryptedMessage, decryptionKey []byte, verifyingKey *VerifyingKey) (*Message, bool, error) {
	// First, decrypt the message
	msg, err := DecryptAES256GCM(encMsg, decryptionKey)
	if err != nil {
		return nil, false, fmt.Errorf("decryption failed: %w", err)
	}

	// Then verify the signature
	valid, err := VerifySignature(msg, verifyingKey)
	if err != nil {
		return msg, false, fmt.Errorf("verification failed: %w", err)
	}

	return msg, valid, nil
}
