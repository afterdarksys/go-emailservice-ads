package amp

import (
	"context"
	"errors"

	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/crypto"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/ledger"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/telemetry"
	"go.uber.org/zap"
)

// Server implements the AMPServer gRPC service
type Server struct {
	UnimplementedAMPServerServer
	Ledger ledger.Ledger
	// Delivery callback to abstract actual storage/routing logic
	OnMessageDelivered func(msg *AMPMessage) error
}

func NewServer(l ledger.Ledger) *Server {
	return &Server{
		Ledger: l,
	}
}

// DeliverMessage handles incoming AMP messages over gRPC
func (s *Server) DeliverMessage(ctx context.Context, msg *AMPMessage) (*DeliveryResponse, error) {
	if msg.Headers == nil || msg.Signature == nil || msg.EncryptedPayload == nil {
		return &DeliveryResponse{Success: false, ErrorMessage: "malformed message"}, nil
	}

	// 1. Resolve Sender Identity to Verify Signature
	senderRecord, err := s.Ledger.ResolveDID(msg.Headers.SenderDid)
	if err != nil {
		telemetry.Log.Error("AMP Ingress: Failed to resolve sender DID", zap.Error(err), zap.String("did", msg.Headers.SenderDid))
		return &DeliveryResponse{Success: false, ErrorMessage: "sender identity error: " + err.Error()}, nil
	}

	// 2. Validate Signature against Headers + Payload
	// We sign the combination of metadata (headers) and the payload to prevent tampering.
	// For this scaffold, we'll reconstruct the signed bytes simply (in production, use canonical protobuf serialization)
	signedPayload := append([]byte(msg.Headers.SenderDid+msg.Headers.RecipientDid+msg.Headers.MessageId), msg.EncryptedPayload...)
	if !crypto.Verify(senderRecord.SigningPublicKey, signedPayload, msg.Signature) {
		telemetry.Log.Warn("AMP Ingress: Signature rejected", zap.String("sender", msg.Headers.SenderDid))
		return &DeliveryResponse{Success: false, ErrorMessage: "cryptographic signature invalid"}, nil
	}

	// 3. (Optional) Validate Receiver Identity, routing logic
	recipientRecord, err := s.Ledger.ResolveDID(msg.Headers.RecipientDid)
	if err != nil {
		return &DeliveryResponse{Success: false, ErrorMessage: "recipient identity error: " + err.Error()}, nil
	}
	if recipientRecord.Revoked {
		return &DeliveryResponse{Success: false, ErrorMessage: "recipient key revoked"}, nil
	}

	// 4. Anchor Proof to Blockchain/Ledger (Proof of Receipt)
	receiptHash, err := s.Ledger.CreateProof(msg.Headers.MessageId)
	if err != nil {
		return nil, errors.New("internal ledger error during anchoring")
	}

	// 5. Enqueue for Local Delivery or Egress Routing
	if s.OnMessageDelivered != nil {
		if err := s.OnMessageDelivered(msg); err != nil {
			return &DeliveryResponse{Success: false, ErrorMessage: "internal routing error"}, nil
		}
	}

	telemetry.Log.Info("AMP Ingress: Successfully received verified message", zap.String("from", msg.Headers.SenderDid), zap.String("to", msg.Headers.RecipientDid))

	return &DeliveryResponse{
		Success:     true,
		ReceiptHash: receiptHash,
	}, nil
}
