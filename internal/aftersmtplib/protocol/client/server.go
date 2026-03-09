package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/crypto"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/ledger"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/protocol/amp"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	JWTSecretKey = []byte("super-secret-aftersmtp-key-replace-in-production")
)

type activeChallenge struct {
	ChallengeHex string
	ExpiresAt    time.Time
}

type Server struct {
	UnimplementedClientAPIServer
	ledger     *ledger.SubstrateLedger
	challenges sync.Map // map[string]*activeChallenge (DID -> Challenge)
}

func NewServer(l *ledger.SubstrateLedger) *Server {
	return &Server{
		ledger: l,
	}
}

// 1. Authentication

func (s *Server) RequestChallenge(ctx context.Context, req *ChallengeRequest) (*ChallengeResponse, error) {
	if req.Did == "" {
		return nil, status.Error(codes.InvalidArgument, "DID is required")
	}

	// Generate 32 bytes of random entropy
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, status.Error(codes.Internal, "failed to generate crypto challenge")
	}
	challengeHex := hex.EncodeToString(nonce)
	expiresAt := time.Now().Add(5 * time.Minute)

	s.challenges.Store(req.Did, &activeChallenge{
		ChallengeHex: challengeHex,
		ExpiresAt:    expiresAt,
	})

	log.Printf("[ClientAPI] Generated Auth Challenge for %s", req.Did)

	return &ChallengeResponse{
		ChallengeHex: challengeHex,
		ExpiresAt:    expiresAt.Unix(),
	}, nil
}

func (s *Server) Authenticate(ctx context.Context, req *AuthRequest) (*AuthResponse, error) {
	if req.Did == "" || req.ChallengeHex == "" || len(req.Signature) == 0 {
		return &AuthResponse{Success: false, ErrorMessage: "invalid request arguments"}, nil
	}

	// 1. Check if challenge exists and is not expired
	val, ok := s.challenges.Load(req.Did)
	if !ok {
		return &AuthResponse{Success: false, ErrorMessage: "no active challenge found for DID"}, nil
	}
	challenge := val.(*activeChallenge)
	if time.Now().After(challenge.ExpiresAt) {
		s.challenges.Delete(req.Did)
		return &AuthResponse{Success: false, ErrorMessage: "challenge expired"}, nil
	}
	if challenge.ChallengeHex != req.ChallengeHex {
		return &AuthResponse{Success: false, ErrorMessage: "challenge mismatch"}, nil
	}

	// 2. Fetch the DID's public key from the Blockchain Ledger
	record, err := s.ledger.ResolveDID(req.Did)
	if err != nil {
		return &AuthResponse{Success: false, ErrorMessage: fmt.Sprintf("failed to resolve identity: %v", err)}, nil
	}

	// 3. Verify the Ed25519 cryptographic signature over the challenge
	if !crypto.Verify(record.SigningPublicKey, []byte(req.ChallengeHex), req.Signature) {
		return &AuthResponse{Success: false, ErrorMessage: "invalid signature"}, nil
	}

	// 4. Success! Generate JWT Session Token
	s.challenges.Delete(req.Did) // consumed
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"did": req.Did,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString(JWTSecretKey)
	if err != nil {
		return &AuthResponse{Success: false, ErrorMessage: "failed to generate session token"}, nil
	}

	log.Printf("[ClientAPI] Successfully authenticated client session for %s", req.Did)

	return &AuthResponse{
		Success: true,
		Token:   tokenString,
	}, nil
}

// Middleware Helper to extract and validate JWT from gRPC context
func VerifyClientAuth(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", errors.New("missing metadata in request context")
	}

	authHeaders := md.Get("authorization")
	if len(authHeaders) == 0 {
		return "", errors.New("missing authorization header")
	}

	tokenStr := authHeaders[0]
	// Expecting "Bearer <token>"
	if len(tokenStr) > 7 && tokenStr[:7] == "Bearer " {
		tokenStr = tokenStr[7:]
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return JWTSecretKey, nil
	})

	if err != nil || !token.Valid {
		return "", errors.New("invalid or expired authorization token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid token claims")
	}

	didAuth, ok := claims["did"].(string)
	if !ok {
		return "", errors.New("did not found in token")
	}

	return didAuth, nil
}

// 2. Messaging

func (s *Server) DispatchMessage(ctx context.Context, req *amp.AMPMessage) (*amp.DeliveryResponse, error) {
	didAuth, err := VerifyClientAuth(ctx)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}

	if req.Headers != nil && req.Headers.SenderDid != didAuth {
		return nil, status.Error(codes.PermissionDenied, "cannot dispatch messages on behalf of a different DID")
	}

	log.Printf("[ClientAPI] Authorized dispatch request from %s for target %s", didAuth, req.Headers.RecipientDid)

	// In a complete implementation, this would route to the core Delivery Engine
	return &amp.DeliveryResponse{
		Success:     true,
		ReceiptHash: "mock-receipt-hash-inserted-into-ledger",
	}, nil
}

func (s *Server) FetchInbox(req *InboxRequest, stream ClientAPI_FetchInboxServer) error {
	didAuth, err := VerifyClientAuth(stream.Context())
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}

	log.Printf("[ClientAPI] Fetching inbox for %s", didAuth)

	// Mocking returning a message stream from the local database
	// An actual implementation would query SQLite/Postgres for stored encrypted ciphertexts
	return nil
}

// 3. Management

func (s *Server) RegisterIdentity(ctx context.Context, req *IdentityRequest) (*IdentityResponse, error) {
	// A real implementation would require a proof-of-work or an administrative session
	// to prevent sybil identity registration spam. We simply register it here.

	record := &ledger.IdentityRecord{
		DID:              req.DesiredDid,
		SigningPublicKey: req.SigningPublicKey,
		EncryptionPubKey: req.EncryptionPublicKey,
	}

	if err := s.ledger.PublishIdentity(record); err != nil {
		return &IdentityResponse{Success: false, ErrorMessage: err.Error()}, nil
	}

	log.Printf("[ClientAPI] Received identity registration command for %s", req.DesiredDid)
	return &IdentityResponse{Success: true}, nil
}
