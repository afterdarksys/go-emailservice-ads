package legacy

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/crypto"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/ledger"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/protocol/amp"
	"google.golang.org/protobuf/proto"
)

// Bridge handles the translation between legacy EML/MIME text and the new AMP protocol.
type Bridge struct {
	Ledger     ledger.Ledger
	ServerKeys *crypto.IdentityKeys // Server's delegatory key for signing on behalf of legacy inbound
	ServerDID  string
}

func NewBridge(l ledger.Ledger, serverDID string, keys *crypto.IdentityKeys) *Bridge {
	return &Bridge{
		Ledger:     l,
		ServerDID:  serverDID,
		ServerKeys: keys,
	}
}

// ConvertToAMP takes raw flat legacy SMTP payload and converts it to a structured, E2E encrypted AMPMessage
func (b *Bridge) ConvertToAMP(senderAddress, recipientAddress string, rawMIME []byte) (*amp.AMPMessage, error) {
	// 1. Resolve Recipient DID
	// In reality, this requires an alias lookup from 'user@domain.com' to 'did:aftersmtp:domain.com:user'
	recipientDID := ledger.FormatDID("msgs.global", "ryan") // Mock translation

	recipientRecord, err := b.Ledger.ResolveDID(recipientDID)
	if err != nil {
		return nil, fmt.Errorf("could not find AMP keys for recipient %s: %w", recipientAddress, err)
	}

	// 2. Parse MIME to AMF Payload (Simplified)
	// Here we would parse headers, extract body and attachments fully.
	amfPayload := &amp.AMFPayload{
		Subject:  "Subject Extracted from MIME", // Mock extraction
		TextBody: string(rawMIME),
	}

	// Serialize AMF Payload
	amfBytes, err := proto.Marshal(amfPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal AMF: %w", err)
	}

	// 3. Encrypt for recipient using X25519
	ciphertext, ephemeralPub, err := crypto.Encrypt(recipientRecord.EncryptionPubKey, amfBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt message: %w", err)
	}

	// 4. Create Metadata
	senderDID := b.ServerDID // In a legacy translation, the bridge signs on behalf of the unauthenticated internet
	msgIDBytes := make([]byte, 16)
	rand.Read(msgIDBytes)
	messageID := hex.EncodeToString(msgIDBytes)

	headers := &amp.AMPHeaders{
		SenderDid:    senderDID,
		RecipientDid: recipientDID,
		Timestamp:    time.Now().Unix(),
		MessageId:    messageID,
		PreviousHop:  senderAddress, // Legacy email trail
	}

	// 5. Sign the metadata + ciphertext
	signedPayload := append([]byte(headers.SenderDid+headers.RecipientDid+headers.MessageId), ciphertext...)
	signature := b.ServerKeys.Sign(signedPayload)

	// 6. Anchor Proof
	proof, _ := b.Ledger.CreateProof(messageID)

	msg := &amp.AMPMessage{
		Headers:            headers,
		EncryptedPayload:   ciphertext,
		EphemeralPublicKey: ephemeralPub,
		Signature:          signature,
		BlockchainProof:    proof,
	}

	log.Printf("Legacy Bridge: Successfully wrapped legacy message from %s into encrypted AMP (%d bytes)", senderAddress, len(ciphertext))
	return msg, nil
}
