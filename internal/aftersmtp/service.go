package aftersmtp

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	aftercrypto "github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/crypto"
	afterledger "github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/ledger"
	afteramp "github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/protocol/amp"
	afterclient "github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/protocol/client"
	afterlegacy "github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/protocol/legacy"
	aftertelemetry "github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/telemetry"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/smtpd"
)

// Service integrates AfterSMTP's AMP listeners with the go-emailservice-ads QueueManager.
type Service struct {
	cfg          *config.Config
	logger       *zap.Logger
	queueManager *smtpd.QueueManager

	ledger     *afterledger.SubstrateLedger
	serverDID  string
	bridge     *afterlegacy.Bridge
	ampServer  *afteramp.Server
	quicServer *afteramp.QUICServer

	grpcServer *grpc.Server
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewService creates a new AfterSMTP Integration Service.
func NewService(cfg *config.Config, logger *zap.Logger, qManager *smtpd.QueueManager) (*Service, error) {
	if !cfg.AfterSMTP.Enabled {
		return nil, errors.New("AfterSMTP is disabled in config")
	}

	// Initialize the embedded telemetry logger so AfterSMTP internal packages don't panic
	if err := aftertelemetry.Init(":9091", true); err != nil {
		logger.Error("Failed to initialize internal AfterSMTP telemetry", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())

	logger.Info("Initializing AfterSMTP Bridge Service", zap.String("ledgerUrl", cfg.AfterSMTP.LedgerURL))

	// Initialize Ledger
	subLedger, err := afterledger.NewSubstrateLedger(cfg.AfterSMTP.LedgerURL)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize substrate ledger: %w", err)
	}

	// Generate Server Identity
	serverKeys, _ := aftercrypto.GenerateIdentityKeys()
	serverDID := afterledger.FormatDID(cfg.Server.Domain, "node_1")
	logger.Info("AfterSMTP Server Node Identity generated", zap.String("did", serverDID))

	// Generate a Mock user identity on the blockchain for testing local delivery if this is test mode
	if cfg.Server.Mode == "test" {
		for _, user := range cfg.Auth.DefaultUsers {
			userKeys, _ := aftercrypto.GenerateIdentityKeys()
			userX25519, _ := ecdh.X25519().GenerateKey(rand.Reader)
			userDID := afterledger.FormatDID(cfg.Server.Domain, user.Username)
			userRecord := &afterledger.IdentityRecord{
				DID:              userDID,
				SigningPublicKey: userKeys.PublicKey,
				EncryptionPubKey: userX25519.PublicKey().Bytes(),
			}
			subLedger.PublishIdentity(userRecord)
			logger.Info("Registered mock blockchain DID for local user", zap.String("username", user.Username), zap.String("did", userDID))
		}
	}

	// Initialize the legacy Bridge to translate payloads and fetch keys
	bridge := afterlegacy.NewBridge(subLedger, serverDID, serverKeys)

	// Initialize AMP Service endpoints
	ampServer := afteramp.NewServer(subLedger)
	quicServer, err := afteramp.NewQUICServer(cfg.AfterSMTP.QUICAddr, subLedger, cfg.Server.TLS.Cert, cfg.Server.TLS.Key)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize QUIC transport: %w", err)
	}

	srv := &Service{
		cfg:          cfg,
		logger:       logger,
		queueManager: qManager,
		ledger:       subLedger,
		serverDID:    serverDID,
		bridge:       bridge,
		ampServer:    ampServer,
		quicServer:   quicServer,
		grpcServer:   grpc.NewServer(),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Wire AMTP routing natively into the platform's multi-tier queue system
	routingCallback := func(msg *afteramp.AMPMessage) error {
		srv.logger.Info("Received AMTP Native Message", zap.String("id", msg.Headers.MessageId), zap.String("from", msg.Headers.SenderDid), zap.String("to", msg.Headers.RecipientDid))

		// Map the destination DID back into a standard string email layout (`user@domain.com`) expected by QueueManager
		// Assuming RecipientDid format is did:aftersmtp:domain:user
		user, domain, parseErr := afterledger.ParseDID(msg.Headers.RecipientDid)
		if parseErr != nil {
			return fmt.Errorf("invalid destination DID format: %v", parseErr)
		}

		targetEmail := fmt.Sprintf("%s@%s", user, domain)

		// For the platform, the AMTP message becomes a raw byte stream the receiver can pull later.
		// For now, we simulate transforming it into an RFC5322 internal block
		var dummyData string
		if msg.EncryptedPayload != nil {
			dummyData = fmt.Sprintf("Subject: AMTP Native Encrypted Payload\r\nFrom: <%s>\r\nTo: <%s>\r\n\r\n[Encrypted Bytes Omitted]", msg.Headers.SenderDid, targetEmail)
		}

		envelope := &smtpd.Message{
			From: msg.Headers.SenderDid,
			To:   []string{targetEmail},
			Data: []byte(dummyData),
			Tier: smtpd.TierInt, // Push directly to internal routing queue
		}

		if enqueueErr := srv.queueManager.Enqueue(envelope); enqueueErr != nil {
			srv.logger.Error("Failed to enqueue native AMTP message into platform", zap.Error(enqueueErr))
			return enqueueErr
		}

		return nil
	}

	srv.ampServer.OnMessageDelivered = routingCallback
	srv.quicServer.OnMessageDelivered = routingCallback

	return srv, nil
}

// Start boots the AMP/QUIC/gRPC native servers.
func (s *Service) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.logger.Info("Starting AfterSMTP gRPC Bridge Ingress", zap.String("addr", s.cfg.AfterSMTP.GRPCAddr))
		lis, err := net.Listen("tcp", s.cfg.AfterSMTP.GRPCAddr)
		if err != nil {
			s.logger.Error("failed to listen on AfterSMTP gRPC port", zap.Error(err))
			return
		}

		afteramp.RegisterAMPServerServer(s.grpcServer, s.ampServer)

		clientServer := afterclient.NewServer(s.ledger)
		afterclient.RegisterClientAPIServer(s.grpcServer, clientServer)

		if err := s.grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			s.logger.Error("failed to serve AfterSMTP gRPC", zap.Error(err))
		}
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.logger.Info("Starting AfterSMTP QUIC Bridge Ingress", zap.String("addr", s.cfg.AfterSMTP.QUICAddr))
		if err := s.quicServer.ListenAndServe(s.ctx); err != nil {
			s.logger.Error("AfterSMTP QUIC server failed", zap.Error(err))
		}
	}()
}

// Shutdown gracefully halts the AMTP bridge components.
func (s *Service) Shutdown() {
	s.logger.Info("Shutting down AfterSMTP Bridge Service...")

	s.cancel() // Halts QUIC Context

	s.grpcServer.GracefulStop()

	s.wg.Wait()
	s.logger.Info("AfterSMTP Bridge Service shutdown complete.")
}
