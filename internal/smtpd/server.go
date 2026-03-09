package smtpd

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-smtp"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/directory"
	"github.com/afterdarksys/go-emailservice-ads/internal/dns"
	"github.com/afterdarksys/go-emailservice-ads/internal/greylisting"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"github.com/afterdarksys/go-emailservice-ads/internal/security"
)

// Server wraps the emersion/go-smtp server configuration
type Server struct {
	smtpServer *smtp.Server
	config     *config.Config
	logger     *zap.Logger
	qManager   *QueueManager
	validator  *auth.Validator
	dirClient  *directory.Client

	// Security components
	policyEngine  *security.PolicyEngine
	dkimVerifier  *security.Verifier
	greylisting   *greylisting.Greylisting
	policyManager *policy.Manager

	// Connection tracking for limits
	connections   map[string]int // IP -> connection count
	connectionsMu sync.Mutex
	totalConns    int
}

// NewServer initializes a new ESMTP Server
func NewServer(cfg *config.Config, logger *zap.Logger, qm *QueueManager, policyMgr *policy.Manager) *Server {
	v := auth.NewValidator(logger)
	dir := directory.NewClient(cfg, logger)

	// Load default users from config
	userStore := v.GetUserStore()
	for _, userCfg := range cfg.Auth.DefaultUsers {
		if err := userStore.AddUser(userCfg.Username, userCfg.Password, userCfg.Email); err != nil {
			logger.Error("Failed to add default user",
				zap.String("username", userCfg.Username),
				zap.Error(err))
		} else {
			logger.Info("Added default user", zap.String("username", userCfg.Username))
		}
	}

	// Initialize security components
	resolver := dns.NewResolver(logger)
	policyEngine := security.NewPolicyEngine(logger, resolver)
	dkimVerifier := security.NewVerifier(logger, resolver)

	// Initialize greylisting if enabled
	var greylist *greylisting.Greylisting
	if cfg.Server.EnableGreylist {
		greylist = greylisting.NewGreylisting(logger)
		// Start cleanup timer
		greylist.StartCleanupTimer(10 * time.Minute)
		logger.Info("Greylisting enabled")
	}

	be := &Backend{
		logger:        logger,
		qManager:      qm,
		validator:     v,
		dirClient:     dir,
		config:        cfg,
		policyEngine:  policyEngine,
		dkimVerifier:  dkimVerifier,
		greylisting:   greylist,
		policyManager: policyMgr,
	}
	s := smtp.NewServer(be)

	s.Addr = cfg.Server.Addr
	s.Domain = cfg.Server.Domain
	s.ReadTimeout = 10 * time.Second
	s.WriteTimeout = 10 * time.Second
	s.MaxMessageBytes = int64(cfg.Server.MaxMessageBytes)
	s.MaxRecipients = cfg.Server.MaxRecipients
	s.AllowInsecureAuth = cfg.Server.AllowInsecureAuth

	// Advanced SMTP Features
	// s.EnableXCLIENT is not available on this version of go-smtp (or requires ext).
	s.EnableSMTPUTF8 = true
	// go-smtp has native support for PIPELINING automatically when multiple extensions are active.
	// We'll also support 8BITMIME which is standard.

	if cfg.Server.TLS != nil && cfg.Server.TLS.Cert != "" && cfg.Server.TLS.Key != "" {
		cert, err := tls.LoadX509KeyPair(cfg.Server.TLS.Cert, cfg.Server.TLS.Key)
		if err != nil {
			logger.Fatal("Failed to load TLS credentials", zap.Error(err))
		}
		tlsConfig := &tls.Config{
			Certificates:             []tls.Certificate{cert},
			MinVersion:               tls.VersionTLS12,
			MaxVersion:               tls.VersionTLS13, // Explicitly allow TLS 1.3
			PreferServerCipherSuites: true,             // Prefer server cipher order
			CipherSuites: []uint16{
				// TLS 1.2 ciphers (TLS 1.3 ciphers are auto-selected)
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,   // ChaCha20 for better mobile performance
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			},
			CurvePreferences: []tls.CurveID{
				tls.X25519,    // Modern, fast curve
				tls.CurveP256,
			},
		}
		s.TLSConfig = tlsConfig
		logger.Info("TLS/STARTTLS capabilities enabled with secure cipher suites")
	} else if cfg.Server.RequireTLS {
		logger.Fatal("RequireTLS is enabled but no TLS certificates configured")
	}

	return &Server{
		smtpServer:   s,
		config:       cfg,
		logger:       logger,
		qManager:     qm,
		validator:    v,
		dirClient:    dir,
		policyEngine: policyEngine,
		dkimVerifier: dkimVerifier,
		greylisting:  greylist,
		connections:  make(map[string]int),
	}
}

// ListenAndServe starts the SMTP server
func (s *Server) ListenAndServe() error {
	s.logger.Info("Starting ESMTP listener", zap.String("addr", s.config.Server.Addr), zap.String("domain", s.config.Server.Domain))
	if s.smtpServer.TLSConfig != nil {
		return s.smtpServer.ListenAndServe()
	}
	return s.smtpServer.ListenAndServe()
}

// Shutdown gracefully stops the SMTP server and the Queue Manager
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Stopping ESMTP listener...")
	s.qManager.Shutdown()
	return s.smtpServer.Shutdown(ctx)
}

// Backend implements smtp.Backend
type Backend struct {
	logger        *zap.Logger
	qManager      *QueueManager
	validator     *auth.Validator
	dirClient     *directory.Client
	config        *config.Config
	policyEngine  *security.PolicyEngine
	dkimVerifier  *security.Verifier
	greylisting   *greylisting.Greylisting
	policyManager *policy.Manager
}

// NewSession is called after client greeting (EHLO/HELO)
func (bkd *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	ip := "unknown"
	if addr, ok := c.Conn().RemoteAddr().(*net.TCPAddr); ok {
		ip = addr.IP.String()
	}

	bkd.logger.Debug("New SMTP session started", zap.String("remote_addr", ip), zap.String("hostname", c.Hostname()))

	res := bkd.validator.ValidateIPAndEHLO(ip, c.Hostname())
	if res == auth.ResultFail {
		return nil, smtp.ErrAuthRequired // basic rejection
	}

	return &Session{
		logger:        bkd.logger,
		qManager:      bkd.qManager,
		validator:     bkd.validator,
		dirClient:     bkd.dirClient,
		policyEngine:  bkd.policyEngine,
		dkimVerifier:  bkd.dkimVerifier,
		greylisting:   bkd.greylisting,
		policyManager: bkd.policyManager,
		ip:            ip,
		ehlo:          c.Hostname(),
		authenticated: false,
		config:        bkd.config,
	}, nil
}

// Session implements smtp.Session
type Session struct {
	logger        *zap.Logger
	qManager      *QueueManager
	validator     *auth.Validator
	dirClient     *directory.Client
	policyEngine  *security.PolicyEngine
	dkimVerifier  *security.Verifier
	greylisting   *greylisting.Greylisting
	policyManager *policy.Manager
	msg           *Message // active message state
	ip            string
	ehlo          string
	authenticated bool
	username      string
	config        *config.Config
}

func (s *Session) AuthPlain(username, password string) error {
	s.logger.Debug("AuthPlain attempted", zap.String("username", username))

	// Use IP-aware authentication with account lockout protection
	user, err := s.validator.GetUserStore().AuthenticateWithIP(username, password, s.ip)
	if err != nil {
		s.logger.Warn("Authentication failed",
			zap.String("username", username),
			zap.String("ip", s.ip),
			zap.Error(err))

		// Return appropriate error with enhanced status code
		if err == auth.ErrAccountLocked || err == auth.ErrRateLimited {
			return &smtp.SMTPError{
				Code:         421,
				EnhancedCode: smtp.EnhancedCode{4, 7, 0},
				Message:      "Too many failed attempts, try again later",
			}
		}

		return &smtp.SMTPError{
			Code:         535,
			EnhancedCode: smtp.EnhancedCode{5, 7, 8},
			Message:      "Authentication credentials invalid",
		}
	}

	s.authenticated = true
	s.username = user.Username
	s.logger.Info("User authenticated successfully",
		zap.String("username", username),
		zap.String("ip", s.ip))

	return nil
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	s.logger.Debug("MAIL FROM", zap.String("from", from))

	// Enforce authentication requirement
	if s.config.Server.RequireAuth && !s.authenticated {
		s.logger.Warn("Mail rejected - authentication required",
			zap.String("from", from),
			zap.String("ip", s.ip))
		return &smtp.SMTPError{
			Code:         530,
			EnhancedCode: smtp.EnhancedCode{5, 7, 0},
			Message:      "Authentication required",
		}
	}

	// Check if authenticated user is authorized to send as this FROM address
	if s.authenticated {
		if !s.validator.AuthorizedToSendAs(s.username, from) {
			s.logger.Warn("User not authorized to send as FROM address",
				zap.String("username", s.username),
				zap.String("from", from),
				zap.String("ip", s.ip))
			return &smtp.SMTPError{
				Code:         550,
				EnhancedCode: smtp.EnhancedCode{5, 7, 1},
				Message:      "Not authorized to send from this address",
			}
		}
	}

	// Validate FROM address against whitelist (for unauthenticated)
	if !s.authenticated {
		if res := s.validator.ValidateWhitelistFrom(from); res == auth.ResultFail {
			return &smtp.SMTPError{
				Code:         550,
				EnhancedCode: smtp.EnhancedCode{5, 7, 1},
				Message:      "Sender not authorized",
			}
		}
	}

	// Perform SPF verification for unauthenticated connections
	if !s.authenticated && s.policyEngine != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Extract domain from FROM address
		fromDomain := ""
		if parts := strings.Split(from, "@"); len(parts) == 2 {
			fromDomain = parts[1]
		}

		ipAddr := net.ParseIP(s.ip)
		if ipAddr != nil && fromDomain != "" {
			spfResult, err := s.policyEngine.VerifySPF(ctx, ipAddr, fromDomain, from)
			if err == nil && spfResult == security.SPFFail {
				s.logger.Warn("SPF verification failed",
					zap.String("from", from),
					zap.String("ip", s.ip),
					zap.String("spf_result", string(spfResult)))
				return &smtp.SMTPError{
					Code:         550,
					EnhancedCode: smtp.EnhancedCode{5, 7, 1},
					Message:      "SPF validation failed",
				}
			}
		}
	}

	s.msg = &Message{
		From:      from,
		CreatedAt: time.Now(),
		Tier:      TierInt, // Default to TierInt, allow policy to override
	}
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.logger.Debug("RCPT TO", zap.String("to", to))

	// Apply greylisting if enabled (only for unauthenticated)
	if s.greylisting != nil && !s.authenticated {
		shouldGreylist, retryAfter, err := s.greylisting.Check(s.ip, s.msg.From, to)
		if err != nil {
			s.logger.Error("Greylisting check failed", zap.Error(err))
		}

		if shouldGreylist {
			s.logger.Info("Message greylisted",
				zap.String("ip", s.ip),
				zap.String("from", s.msg.From),
				zap.String("to", to),
				zap.Duration("retry_after", retryAfter))

			return &smtp.SMTPError{
				Code:         451,
				EnhancedCode: smtp.EnhancedCode{4, 7, 1},
				Message:      fmt.Sprintf("Greylisted, please retry in %s", retryAfter.Round(time.Second)),
			}
		}
	}

	s.msg.To = append(s.msg.To, to)
	return nil
}

func (s *Session) Data(r io.Reader) error {
	s.logger.Debug("DATA block stream reading")
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.logger.Debug("Received message data", zap.Int("length", len(b)))
	s.msg.Data = b

	// Perform DKIM verification for incoming messages (unauthenticated)
	if !s.authenticated && s.dkimVerifier != nil {
		// Run DKIM verification in background, don't block
		go func() {
			dkimResult, err := s.dkimVerifier.VerifyDKIM(b)
			if err != nil {
				s.logger.Debug("DKIM verification failed",
					zap.String("from", s.msg.From),
					zap.Error(err))
			} else {
				s.logger.Info("DKIM verification result",
					zap.String("from", s.msg.From),
					zap.String("result", dkimResult))
			}
		}()
	}

	// === POLICY ENGINE EVALUATION ===
	if s.policyManager != nil {
		// Create email context for policy evaluation
		emailCtx, err := policy.NewEmailContext(s.msg.From, s.msg.To, s.ip, s.ehlo, b)
		if err != nil {
			s.logger.Warn("Failed to create policy context", zap.Error(err))
			// Continue without policy evaluation
		} else {
			// Set authentication info
			emailCtx.Authenticated = s.authenticated
			emailCtx.Username = s.username

			// Set direction
			emailCtx.IsInbound = !s.authenticated
			emailCtx.IsOutbound = s.authenticated
			emailCtx.LocalDomains = s.config.Server.LocalDomains

			// TODO: Populate security results (SPF, DKIM, DMARC)
			// These would come from earlier checks in the SMTP flow
			emailCtx.SPFResult = policy.SPFNone
			emailCtx.DKIMResult = policy.DKIMNone
			emailCtx.DMARCResult = policy.DMARCNone

			// TODO: Populate IP reputation
			emailCtx.IPReputation = policy.ReputationScore{Score: 50, Source: "internal"}

			// Evaluate policies
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			action, err := s.policyManager.Evaluate(ctx, emailCtx)
			if err != nil {
				s.logger.Error("Policy evaluation failed",
					zap.String("from", s.msg.From),
					zap.Strings("to", s.msg.To),
					zap.Error(err))
				// Continue with default action
			} else if action != nil {
				// Handle policy action
				switch action.Type {
				case policy.ActionReject:
					s.logger.Info("Policy rejected message",
						zap.String("from", s.msg.From),
						zap.String("reason", action.Reason))
					return &smtp.SMTPError{
						Code:         550,
						EnhancedCode: smtp.EnhancedCode{5, 7, 1},
						Message:      action.Reason,
					}

				case policy.ActionDefer:
					s.logger.Info("Policy deferred message",
						zap.String("from", s.msg.From),
						zap.String("reason", action.Reason))
					return &smtp.SMTPError{
						Code:         451,
						EnhancedCode: smtp.EnhancedCode{4, 7, 1},
						Message:      action.Reason,
					}

				case policy.ActionDiscard:
					s.logger.Info("Policy discarded message",
						zap.String("from", s.msg.From))
					// Silently discard - return success but don't queue
					s.msg = nil
					return nil

				case policy.ActionRedirect:
					s.logger.Info("Policy redirected message",
						zap.String("from", s.msg.From),
						zap.String("original_to", strings.Join(s.msg.To, ",")),
						zap.String("redirect_to", action.Target))
					s.msg.To = []string{action.Target}

				case policy.ActionFileinto:
					// Store folder in message for later processing
					s.logger.Info("Policy filed message",
						zap.String("folder", action.Target))
					// TODO: Add folder metadata to message

				case policy.ActionAccept, policy.ActionKeep:
					// Continue normal processing
					s.logger.Debug("Policy accepted message")

				default:
					s.logger.Warn("Unknown policy action",
						zap.String("action", string(action.Type)))
				}

				// Apply header modifications
				// TODO: Implement header modifications on message data
			}
		}
	}

	// Fast dispatch to queue manager
	if err := s.qManager.Enqueue(s.msg); err != nil {
		s.logger.Error("Failed to enqueue message", zap.Error(err))
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 3, 0},
			Message:      "Temporary failure, please retry",
		}
	}

	s.msg = nil // clear for next transaction in same session (if client uses RSET or sends another MAIL FROM)
	return nil
}

func (s *Session) Reset() {
	s.logger.Debug("Session reset")
	s.msg = nil
}

func (s *Session) Logout() error {
	s.logger.Debug("Session logout")
	return nil
}
