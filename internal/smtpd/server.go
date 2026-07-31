package smtpd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-smtp"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

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
	spreadPrev    *security.SpreadPrevention
	limiter       *connectionLimiter
	messageRates  *ipMessageLimiter

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

	// Set logger on user store
	userStore.SetLogger(logger)

	// Initialize SSO provider if enabled
	if cfg.SSO.Enabled {
		ssoProvider := auth.NewSSOProvider(cfg, logger)
		if ssoProvider != nil {
			userStore.SetSSOProvider(ssoProvider)
			logger.Info("SSO authentication enabled",
				zap.String("provider", cfg.SSO.Provider),
				zap.String("directory_url", cfg.SSO.DirectoryURL))
		}
	}

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

	spreadPrev := security.NewSpreadPrevention(logger, 5*time.Minute, 50)

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
		spreadPrev:    spreadPrev,
		limiter:       newConnectionLimiter(cfg.Server.MaxConnections, cfg.Server.MaxPerIP),
		messageRates:  newIPMessageLimiter(cfg.Server.RateLimitPerIP),
	}
	s := smtp.NewServer(be)

	s.Addr = cfg.Server.Addr
	s.Domain = cfg.Server.Domain
	commandTimeout := configuredDuration(cfg.Server.Timeouts.Command, 5*time.Minute)
	s.ReadTimeout = commandTimeout
	s.WriteTimeout = commandTimeout
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
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305, // ChaCha20 for better mobile performance
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			},
			CurvePreferences: []tls.CurveID{
				tls.X25519, // Modern, fast curve
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
		spreadPrev:   spreadPrev,
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
	spreadPrev    *security.SpreadPrevention
	limiter       *connectionLimiter
	messageRates  *ipMessageLimiter
}

type ipMessageLimiter struct {
	mu      sync.Mutex
	perHour int
	byIP    map[string]*rate.Limiter
}

func newIPMessageLimiter(perHour int) *ipMessageLimiter {
	return &ipMessageLimiter{perHour: perHour, byIP: make(map[string]*rate.Limiter)}
}
func (l *ipMessageLimiter) allow(ip string) bool {
	if l == nil || l.perHour <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	limiter := l.byIP[ip]
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Limit(float64(l.perHour)/3600), l.perHour)
		l.byIP[ip] = limiter
	}
	return limiter.Allow()
}

type connectionLimiter struct {
	mu                        sync.Mutex
	total, maxTotal, maxPerIP int
	byIP                      map[string]int
}

func newConnectionLimiter(maxTotal, maxPerIP int) *connectionLimiter {
	return &connectionLimiter{maxTotal: maxTotal, maxPerIP: maxPerIP, byIP: make(map[string]int)}
}
func (l *connectionLimiter) acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.maxTotal > 0 && l.total >= l.maxTotal || l.maxPerIP > 0 && l.byIP[ip] >= l.maxPerIP {
		return false
	}
	l.total++
	l.byIP[ip]++
	return true
}
func (l *connectionLimiter) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byIP[ip] > 0 {
		l.byIP[ip]--
		l.total--
		if l.byIP[ip] == 0 {
			delete(l.byIP, ip)
		}
	}
}
func configuredDuration(raw string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

// NewSession is called after client greeting (EHLO/HELO)
func (bkd *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	ip := "unknown"
	if addr, ok := c.Conn().RemoteAddr().(*net.TCPAddr); ok {
		ip = addr.IP.String()
	}

	bkd.logger.Debug("New SMTP session started", zap.String("remote_addr", ip), zap.String("hostname", c.Hostname()))
	if bkd.limiter != nil && !bkd.limiter.acquire(ip) {
		return nil, smtp.ErrAuthRequired
	}

	res := bkd.validator.ValidateIPAndEHLO(ip, c.Hostname())
	if res == auth.ResultFail {
		if bkd.limiter != nil {
			bkd.limiter.release(ip)
		}
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
		spreadPrev:    bkd.spreadPrev,
		ip:            ip,
		ehlo:          c.Hostname(),
		authenticated: false,
		config:        bkd.config,
		limiter:       bkd.limiter,
		messageRates:  bkd.messageRates,
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
	spreadPrev    *security.SpreadPrevention
	msg           *Message // active message state
	ip            string
	ehlo          string
	authenticated bool
	username      string
	config        *config.Config
	limiter       *connectionLimiter
	messageRates  *ipMessageLimiter
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

	// Perform SPF verification for unauthenticated connections.
	// The result is always recorded — it is stamped into Authentication-Results
	// and feeds DMARC alignment. Whether an SPF non-pass *rejects* depends on the
	// configured mode: "monitor" (default) treats SPF purely as a DMARC input,
	// while "enforce" reluctantly hard-rejects. Standalone SPF rejection is opt-in
	// because forwarding and misconfigured records make it a deliverability hazard.
	spfResultStr := "none"
	if !s.authenticated && s.policyEngine != nil && s.config.Server.SPF.Enabled {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// RFC 7208 §2.4: a null reverse-path is authenticated using the HELO
		// identity. Supplying postmaster@HELO keeps SPF macro expansion valid.
		fromDomain, spfIdentity := spfIdentityForMailFrom(from, s.ehlo)

		ipAddr := net.ParseIP(s.ip)
		if ipAddr != nil && fromDomain != "" {
			spfResult, err := s.policyEngine.VerifySPF(ctx, ipAddr, fromDomain, spfIdentity)
			if err == nil {
				spfResultStr = string(spfResult)
				enforce := strings.EqualFold(s.config.Server.SPF.Mode, "enforce")
				switch spfResult {
				case security.SPFFail:
					if enforce {
						s.logger.Warn("SPF hard fail — rejecting (enforce mode)",
							zap.String("from", from), zap.String("ip", s.ip))
						return &smtp.SMTPError{
							Code:         550,
							EnhancedCode: smtp.EnhancedCode{5, 7, 1},
							Message:      "SPF validation failed",
						}
					}
					s.logger.Info("SPF hard fail — monitor mode, deferring to DMARC",
						zap.String("from", from), zap.String("ip", s.ip))
				case security.SPFSoftFail:
					if enforce && s.config.Server.SPF.RejectOnSoftfail {
						s.logger.Warn("SPF softfail — rejecting (enforce mode, reject_on_softfail)",
							zap.String("from", from), zap.String("ip", s.ip))
						return &smtp.SMTPError{
							Code:         550,
							EnhancedCode: smtp.EnhancedCode{5, 7, 1},
							Message:      "SPF validation failed (softfail)",
						}
					}
					s.logger.Info("SPF softfail — message tagged",
						zap.String("from", from), zap.String("ip", s.ip))
				}
			}
		}
	}

	extraHeaders := map[string]string{
		"X-SPF-Status": spfResultStr,
	}

	s.msg = &Message{
		From:         from,
		CreatedAt:    time.Now(),
		Tier:         TierInt, // Default to TierInt, allow policy to override
		SPFResult:    spfResultStr,
		DKIMResult:   "none",
		ExtraHeaders: extraHeaders,
	}
	return nil
}

func spfIdentityForMailFrom(from, ehlo string) (domain, identity string) {
	if domain = addressDomain(from); domain != "" {
		return domain, from
	}
	if ehlo == "" {
		return "", ""
	}
	return ehlo, "postmaster@" + ehlo
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
	if !s.messageRates.allow(s.ip) {
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "message rate limit exceeded"}
	}
	s.logger.Debug("DATA block stream reading")
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.logger.Debug("Received message data", zap.Int("length", len(b)))
	s.msg.Data = b

	// Spread Prevention Check
	if s.spreadPrev != nil && s.spreadPrev.Evaluate(b) {
		s.logger.Warn("Message quarantined by Spread Prevention (Outbreak detected)", zap.String("from", s.msg.From))
		// Quarantine or Reject
		return &smtp.SMTPError{
			Code:         550,
			EnhancedCode: smtp.EnhancedCode{5, 7, 1},
			Message:      "Message rejected by Spread Prevention Outbreak filters",
		}
	}

	// Perform DKIM verification synchronously so the result feeds into policy and
	// DMARC evaluation. We capture each signature's signing domain (d=) so DMARC
	// can check identifier alignment (RFC 7489 §3.1).
	var dkimResults []security.DKIMVerification
	if !s.authenticated && s.dkimVerifier != nil {
		verifications, err := s.dkimVerifier.VerifyWithDetails(b)
		switch {
		case err != nil:
			s.logger.Debug("DKIM verification error", zap.String("from", s.msg.From), zap.Error(err))
			s.msg.DKIMResult = "fail"
		case len(verifications) == 0:
			s.msg.DKIMResult = "none" // unsigned message
		default:
			anyPass := false
			for _, v := range verifications {
				pass := v.Err == nil
				dkimResults = append(dkimResults, security.DKIMVerification{Domain: v.Domain, Pass: pass})
				if pass {
					anyPass = true
				}
			}
			if anyPass {
				s.msg.DKIMResult = "pass"
			} else {
				s.msg.DKIMResult = "fail"
			}
		}
		s.logger.Info("DKIM verification result",
			zap.String("from", s.msg.From),
			zap.String("result", s.msg.DKIMResult))
	}

	// === DMARC EVALUATION (RFC 7489) ===
	// Inbound (unauthenticated) mail only. Evaluation always runs (so the verdict
	// is logged and stamped into Authentication-Results); whether we *act* on a
	// failure depends on the configured mode:
	//   - monitor: observe only — never reject or quarantine, whatever p= says.
	//   - enforce: honor the sender's published policy (p=reject/p=quarantine).
	// This lets a deployment observe before enforcing — the standard rollout path.
	if !s.authenticated && s.policyEngine != nil && s.config.Server.DMARC.Enabled {
		if fromHeaderDomain := headerFromDomain(b); fromHeaderDomain != "" {
			spfDomain := addressDomain(s.msg.From)
			if spfDomain == "" {
				spfDomain = s.ehlo // null reverse-path: SPF authenticates the HELO identity
			}

			dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
			dmarcResult, dmarcPolicy, dmarcPct, derr := s.policyEngine.EvaluateDMARC(
				dctx, fromHeaderDomain, spfDomain, security.SPFResult(s.msg.SPFResult), dkimResults)
			dcancel()
			if derr != nil {
				s.logger.Debug("DMARC evaluation error",
					zap.String("from_header_domain", fromHeaderDomain), zap.Error(derr))
			}
			s.msg.DMARCResult = string(dmarcResult)

			enforce := strings.EqualFold(s.config.Server.DMARC.Mode, "enforce")
			s.logger.Info("DMARC evaluation",
				zap.String("from_header_domain", fromHeaderDomain),
				zap.String("result", string(dmarcResult)),
				zap.String("policy", string(dmarcPolicy)),
				zap.Bool("enforce", enforce),
				zap.String("ip", s.ip))

			if dmarcResult == security.DMARCFail {
				if !dmarcPolicyApplies(b, dmarcPct) {
					s.logger.Info("DMARC policy skipped by pct rollout",
						zap.String("from_header_domain", fromHeaderDomain),
						zap.Int("pct", dmarcPct))
					dmarcPolicy = security.DMARCPolicyNone
				}
				switch dmarcPolicy {
				case security.DMARCPolicyReject:
					if enforce {
						s.logger.Warn("DMARC fail with p=reject — rejecting message",
							zap.String("from_header_domain", fromHeaderDomain),
							zap.String("ip", s.ip))
						return &smtp.SMTPError{
							Code:         550,
							EnhancedCode: smtp.EnhancedCode{5, 7, 1},
							Message:      "DMARC policy evaluation failed",
						}
					}
					s.logger.Info("DMARC fail with p=reject — monitor mode, message allowed",
						zap.String("from_header_domain", fromHeaderDomain),
						zap.String("ip", s.ip))
				case security.DMARCPolicyQuarantine:
					if enforce {
						folder := s.config.Server.DMARC.QuarantineFolder
						if folder == "" {
							folder = "Junk"
						}
						s.msg.Quarantine = true
						s.msg.QuarantineFolder = folder
						s.logger.Info("DMARC fail with p=quarantine — routing to quarantine folder",
							zap.String("from_header_domain", fromHeaderDomain),
							zap.String("folder", folder),
							zap.String("ip", s.ip))
					} else {
						s.logger.Info("DMARC fail with p=quarantine — monitor mode, message allowed",
							zap.String("from_header_domain", fromHeaderDomain),
							zap.String("ip", s.ip))
					}
				}
			}
		}
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

			// Populate security results from earlier checks
			emailCtx.SPFResult = policy.SPFResult(s.msg.SPFResult)
			emailCtx.DKIMResult = policy.DKIMResult(s.msg.DKIMResult)
			emailCtx.DMARCResult = policy.DMARCResult(s.msg.DMARCResult)

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

	// Stamp an RFC 7601 Authentication-Results header for inbound mail so
	// downstream filters (Sieve, clients) can see the SPF/DKIM/DMARC verdicts.
	// Added for unauthenticated mail only; submission from our own users is trusted.
	if !s.authenticated {
		s.msg.Data = prependAuthResults(s.msg.Data, s.config.Server.Domain,
			s.msg.SPFResult, s.msg.DKIMResult, s.msg.DMARCResult, s.msg.From)
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

// dmarcPolicyApplies deterministically samples a message into a DMARC pct=
// rollout. A digest keeps retries and duplicate deliveries from receiving
// inconsistent treatment while still distributing messages across 100 buckets.
func dmarcPolicyApplies(message []byte, pct int) bool {
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	digest := sha256.Sum256(message)
	bucket := (uint16(digest[0])<<8 | uint16(digest[1])) % 100
	return int(bucket) < pct
}

func (s *Session) Reset() {
	s.logger.Debug("Session reset")
	s.msg = nil
}

func (s *Session) Logout() error {
	if s.limiter != nil {
		s.limiter.release(s.ip)
		s.limiter = nil
	}
	s.logger.Debug("Session logout")
	return nil
}

// addressDomain returns the lowercased domain of an envelope address, tolerating
// optional angle brackets (e.g. "<user@example.com>"). Returns "" if there is no
// domain (e.g. the null reverse-path "<>").
func addressDomain(addr string) string {
	addr = strings.TrimSpace(addr)
	addr = strings.Trim(addr, "<>")
	at := strings.LastIndex(addr, "@")
	if at < 0 || at == len(addr)-1 {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}

// headerFromDomain extracts the domain of the RFC 5322 From header — the
// identity DMARC authenticates against. Returns "" if the header is absent or
// unparseable, or if it lists multiple From addresses (RFC 7489 only defines
// alignment for a single From domain).
func headerFromDomain(raw []byte) string {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	fromHeader := msg.Header.Get("From")
	if fromHeader == "" {
		return ""
	}
	addrs, err := mail.ParseAddressList(fromHeader)
	if err != nil || len(addrs) != 1 {
		return ""
	}
	return addressDomain(addrs[0].Address)
}

// prependAuthResults inserts an RFC 7601 Authentication-Results header at the top
// of a message's header block, recording the SPF, DKIM and DMARC verdicts.
func prependAuthResults(raw []byte, authservID, spf, dkim, dmarc, mailFrom string) []byte {
	if authservID == "" {
		authservID = "localhost"
	}
	if spf == "" {
		spf = "none"
	}
	if dkim == "" {
		dkim = "none"
	}
	if dmarc == "" {
		dmarc = "none"
	}

	header := fmt.Sprintf("Authentication-Results: %s; spf=%s smtp.mailfrom=%s; dkim=%s; dmarc=%s\r\n",
		sanitizeHeaderValue(authservID), spf, sanitizeHeaderValue(mailFrom), dkim, dmarc)

	out := make([]byte, 0, len(header)+len(raw))
	out = append(out, header...)
	out = append(out, raw...)
	return out
}

// sanitizeHeaderValue strips CR/LF (and other control characters) from a value
// that is interpolated into a header line, preventing header injection.
func sanitizeHeaderValue(v string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r < 0x20 {
			return -1
		}
		return r
	}, v)
}
