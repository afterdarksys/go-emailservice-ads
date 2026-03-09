package imap

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/emersion/go-imap/server"
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
// RFC 3501 - INTERNET MESSAGE ACCESS PROTOCOL - VERSION 4rev1
// RFC 2595 - Using TLS with IMAP, POP3 and ACAP
type Server struct {
	logger     *zap.Logger
	store      Store
	config     *config.Config
	validator  *auth.Validator
	imapServer *server.Server
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
	s.logger.Info("Starting IMAP4rev1 server (go-imap)", zap.String("addr", addr))

	// Create backend
	backend := NewBackend(s.logger, s.store, s.validator)

	// Create IMAP server
	s.imapServer = server.New(backend)
	s.imapServer.Addr = addr

	// Server allows authentication over unencrypted connections (set to false in production)
	s.imapServer.AllowInsecureAuth = false

	// Configure TLS if available
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

		s.imapServer.TLSConfig = tlsConfig

		if s.config.IMAP.RequireTLS {
			s.logger.Info("IMAP server configured with mandatory TLS (IMAPS mode)")
			// For implicit TLS (port 993), use ListenAndServeTLS
			go func() {
				s.logger.Info("IMAP server listening (implicit TLS)", zap.String("addr", addr))
				if err := s.imapServer.ListenAndServeTLS(); err != nil {
					s.logger.Error("IMAP server error", zap.Error(err))
				}
			}()
		} else {
			s.logger.Info("IMAP server configured with STARTTLS support")
			go func() {
				s.logger.Info("IMAP server listening (STARTTLS)", zap.String("addr", addr))
				if err := s.imapServer.ListenAndServe(); err != nil {
					s.logger.Error("IMAP server error", zap.Error(err))
				}
			}()
		}
	} else {
		if s.config.IMAP.RequireTLS {
			return fmt.Errorf("IMAP RequireTLS is enabled but no TLS certificates configured")
		}

		// No TLS configured - run insecure (only for testing)
		s.logger.Warn("IMAP server running WITHOUT TLS - NOT RECOMMENDED FOR PRODUCTION")
		go func() {
			if err := s.imapServer.ListenAndServe(); err != nil {
				s.logger.Error("IMAP server error", zap.Error(err))
			}
		}()
	}

	s.logger.Info("IMAP4rev1 server started successfully", zap.String("addr", addr))
	return nil
}

// Shutdown gracefully stops the IMAP server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Stopping IMAP server...")
	if s.imapServer != nil {
		return s.imapServer.Close()
	}
	return nil
}
