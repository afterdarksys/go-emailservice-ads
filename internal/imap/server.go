package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"strings"
	"time"

	imap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/server"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

// MessageSummary represents the metadata for a single email in the store.
type MessageSummary struct {
	ID      string
	UID     uint32
	Flags   []string
	Size    int64
	Date    time.Time
	From    string
	Subject string
	Deleted bool
}

// Store defines the interface for backend mailbox operations.
// This is designed to be pluggable to support traditional Maildir,
// database-backed stores, or Content-Addressed storage (IPFS/Hashes).
type Store interface {
	GetMessages(ctx context.Context, username, folder string) ([]MessageSummary, error)
	FetchMessage(ctx context.Context, msgID string) ([]byte, error)
	StoreMessage(ctx context.Context, username, folder string, data []byte) (string, error)

	// UID tracking — RFC 3501 §2.3.1.1
	GetUIDValidity(ctx context.Context, username, mailbox string) (uint32, error)
	GetUIDNext(ctx context.Context, username, mailbox string) (uint32, error)
	AllocateUID(ctx context.Context, username, mailbox string) (uint32, error)

	// Flag management — RFC 3501 §2.3.2
	UpdateMessageFlags(ctx context.Context, msgID, username, mailbox string, op imap.FlagsOp, flags []string) error

	// ExpungeDeleted permanently removes \Deleted messages and returns their message IDs.
	ExpungeDeleted(ctx context.Context, username, mailbox string) ([]string, error)
}

// Updater is an optional Store extension for IMAP IDLE push notifications.
// Stores that support delivery notifications implement this interface.
type Updater interface {
	DeliveryUpdates() <-chan [2]string
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

	// Create backend, passing master (proxy) auth config for trusted
	// control-plane services (e.g. the msgs.global mail-read gateway).
	master := MasterAuthConfig{
		User:       s.config.Auth.MasterUser,
		Password:   s.config.Auth.MasterPassword,
		AllowedIPs: s.config.Auth.MasterAllowedIPs,
		Separator:  s.config.Auth.MasterSeparator,
	}
	backend := NewBackend(s.logger, s.store, s.validator, master)

	// Create IMAP server
	s.imapServer = server.New(backend)
	s.imapServer.Addr = addr
	s.imapServer.MaxLiteralSize = imapLiteralLimit(s.config.Server.MaxMessageBytes)

	// Server allows authentication over unencrypted connections (set to false in production)
	s.imapServer.AllowInsecureAuth = false

	tlsMode, err := imapTLSMode(s.config.IMAP.TLSMode)
	if err != nil {
		return err
	}

	// Configure TLS if available.
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

		if tlsMode == "implicit" {
			s.logger.Info("IMAP server configured with mandatory TLS (IMAPS mode)")
			// For implicit TLS (port 993), use ListenAndServeTLS
			go func() {
				s.logger.Info("IMAP server listening (implicit TLS)", zap.String("addr", addr))
				if err := s.imapServer.ListenAndServeTLS(); err != nil {
					s.logger.Error("IMAP server error", zap.Error(err))
				}
			}()
		} else if tlsMode == "starttls" {
			s.logger.Info("IMAP server configured with STARTTLS support")
			go func() {
				s.logger.Info("IMAP server listening (STARTTLS)", zap.String("addr", addr))
				if err := s.imapServer.ListenAndServe(); err != nil {
					s.logger.Error("IMAP server error", zap.Error(err))
				}
			}()
		} else {
			return fmt.Errorf("IMAP TLS mode disabled but TLS configuration was supplied")
		}
	} else {
		if tlsMode != "disabled" {
			return fmt.Errorf("IMAP TLS mode %q requires TLS certificates", tlsMode)
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

func imapTLSMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "starttls":
		return "starttls", nil
	case "implicit":
		return "implicit", nil
	case "disabled":
		return "disabled", nil
	default:
		return "", fmt.Errorf("invalid IMAP tls_mode %q (want starttls, implicit, or disabled)", mode)
	}
}

func imapLiteralLimit(maxMessageBytes int) uint32 {
	if maxMessageBytes <= 0 {
		return 0
	}
	if uint64(maxMessageBytes) > uint64(math.MaxUint32) {
		return math.MaxUint32
	}
	return uint32(maxMessageBytes)
}

// Shutdown gracefully stops the IMAP server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Stopping IMAP server...")
	if s.imapServer != nil {
		return s.imapServer.Close()
	}
	return nil
}
