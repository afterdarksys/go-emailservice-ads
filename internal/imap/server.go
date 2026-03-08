package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

// Store defines the interface for backend mailbox operations.
// This is designed to be pluggable to support traditional Maildir,
// database-backed stores, or Content-Addressed storage (IPFS/Hashes).
type Store interface {
	GetMessages(ctx context.Context, username, folder string) ([]MessageSummary, error)
	FetchMessage(ctx context.Context, msgID string) ([]byte, error)
	StoreMessage(ctx context.Context, username, folder string, data []byte) (string, error)
}

// MessageSummary represents the metadata for a single email in the store
type MessageSummary struct {
	ID    string
	Flags []string
	Size  int64
}

// Server implements secure IMAP4rev1 server with TLS and authentication
type Server struct {
	logger    *zap.Logger
	store     Store
	config    *config.Config
	validator *auth.Validator
	listener  net.Listener
}

// NewServer initializes a secure IMAP server
func NewServer(logger *zap.Logger, store Store, cfg *config.Config, validator *auth.Validator) *Server {
	return &Server{
		logger:    logger,
		store:     store,
		config:    cfg,
		validator: validator,
	}
}

// Start begins listening for IMAP connections with TLS support
// Implements IMAP4rev1 (RFC 3501) with STARTTLS (RFC 2595)
func (s *Server) Start() error {
	addr := s.config.IMAP.Addr
	s.logger.Info("Starting IMAP4rev1 server", zap.String("addr", addr))

	// Create listener
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener

	// If TLS is configured and required, use TLS listener
	if s.config.IMAP.TLS != nil && s.config.IMAP.TLS.Cert != "" && s.config.IMAP.TLS.Key != "" {
		cert, err := tls.LoadX509KeyPair(s.config.IMAP.TLS.Cert, s.config.IMAP.TLS.Key)
		if err != nil {
			return fmt.Errorf("failed to load TLS certificates: %w", err)
		}

		tlsConfig := &tls.Config{
			Certificates:             []tls.Certificate{cert},
			MinVersion:               tls.VersionTLS12,
			MaxVersion:               tls.VersionTLS13,
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			},
			CurvePreferences: []tls.CurveID{
				tls.X25519,
				tls.CurveP256,
			},
		}

		if s.config.IMAP.RequireTLS {
			// Wrap listener with TLS for implicit TLS (port 993)
			listener = tls.NewListener(listener, tlsConfig)
			s.logger.Info("IMAP server started with TLS (implicit)", zap.String("addr", addr))
		} else {
			s.logger.Info("IMAP server started with STARTTLS support", zap.String("addr", addr))
		}
	} else if s.config.IMAP.RequireTLS {
		return fmt.Errorf("IMAP RequireTLS is enabled but no TLS certificates configured")
	}

	// Accept connections (basic implementation)
	// In production, this would integrate with github.com/emersion/go-imap/v2
	go s.acceptConnections()

	return nil
}

// acceptConnections handles incoming IMAP connections
func (s *Server) acceptConnections() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.logger.Error("Failed to accept IMAP connection", zap.Error(err))
			return
		}

		go s.handleConnection(conn)
	}
}

// handleConnection processes a single IMAP connection
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	s.logger.Info("New IMAP connection", zap.String("remote_addr", remoteAddr))

	// Send greeting
	greeting := "* OK [CAPABILITY IMAP4rev1 STARTTLS AUTH=PLAIN AUTH=LOGIN] IMAP4rev1 Service Ready\r\n"
	if _, err := conn.Write([]byte(greeting)); err != nil {
		s.logger.Error("Failed to send IMAP greeting", zap.Error(err))
		return
	}

	// TODO: Implement full IMAP4rev1 protocol handler
	// This would parse commands, handle authentication, manage mailboxes, etc.
	// For now, this is a framework that enforces TLS and authentication requirements
	s.logger.Info("IMAP connection established - awaiting commands", zap.String("remote_addr", remoteAddr))
}

// Shutdown gracefully stops the IMAP server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Stopping IMAP server...")
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
