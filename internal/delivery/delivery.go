package delivery

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/dns"
	"github.com/afterdarksys/go-emailservice-ads/internal/security"
	"github.com/afterdarksys/go-emailservice-ads/internal/security/dane"
)

// DeliveryResult represents the outcome of a delivery attempt
type DeliveryResult struct {
	Success     bool
	SMTPCode    int
	Message     string
	IsPermanent bool
	RemoteHost  string
	DeliveredAt time.Time
	DANEUsed    bool // Whether DANE was available and used
	DANEValid   bool // Whether DANE validation succeeded
	Recipients  []RecipientResult
}

// RecipientResult records the individual recipient outcome of an SMTP
// transaction, allowing callers to distinguish accepted mail from permanent
// and temporary RCPT failures.
type RecipientResult struct {
	Recipient   string
	Success     bool
	SMTPCode    int
	IsPermanent bool
	Message     string
}

// MailDelivery handles outbound SMTP mail delivery
// RFC 5321 - Simple Mail Transfer Protocol
type MailDelivery struct {
	reports  *security.DurableTLSReports
	sts      *security.MTASTSManager
	routes   []Route
	logger   *zap.Logger
	resolver *dns.Resolver
	hostname string

	// Connection pooling
	pools   map[string]*connectionPool
	poolsMu sync.RWMutex

	// DANE validator for DNS-Based Authentication (RFC 7672)
	daneValidator *dane.DANEValidator
	daneEnabled   bool

	// Timeouts
	connectTimeout time.Duration
	dataTimeout    time.Duration
}

// connectionPool manages reusable connections to a specific MX host
type connectionPool struct {
	host        string
	connections chan *smtpConnection
	mu          sync.Mutex
	maxIdle     int
	maxOpen     int
	openCount   int
}

// smtpConnection wraps an SMTP client with metadata
type smtpConnection struct {
	client    *smtp.Client
	host      string
	createdAt time.Time
	lastUsed  time.Time
}

// NewMailDelivery creates a new outbound mail delivery handler
func NewMailDelivery(logger *zap.Logger, resolver *dns.Resolver, hostname string) *MailDelivery {
	return &MailDelivery{
		logger:   logger,
		resolver: resolver,
		hostname: hostname,
		pools:    make(map[string]*connectionPool),
		// Per-connection TLS configs are built in dialSMTP: DANE-authenticated
		// when TLSA records exist, otherwise encrypt-only opportunistic TLS.
		daneEnabled:    false, // Will be enabled via SetDANEValidator
		connectTimeout: 30 * time.Second,
		dataTimeout:    5 * time.Minute,
	}
}

// SetDANEValidator configures DANE validation for outbound connections
func (d *MailDelivery) SetDANEValidator(validator *dane.DANEValidator) {
	d.daneValidator = validator
	d.daneEnabled = validator != nil

	if d.daneEnabled {
		d.logger.Info("DANE validation enabled for outbound SMTP")
	}
}

// Deliver sends a message to external recipients
// RFC 5321 Section 3.3 - Mail Transactions
func (d *MailDelivery) Deliver(ctx context.Context, from string, to []string, data []byte) (*DeliveryResult, error) {
	if len(to) == 0 {
		return nil, fmt.Errorf("no recipients specified")
	}

	aggregate := &DeliveryResult{Success: true, SMTPCode: 250}
	var failures []string
	for domain, recipients := range d.groupByDomain(to) {
		result, err := d.deliverToDomain(ctx, domain, from, recipients, data)
		if result == nil {
			result = &DeliveryResult{SMTPCode: 451, Message: "Delivery unavailable"}
		}
		if len(result.Recipients) == 0 {
			for _, rcpt := range recipients {
				result.Recipients = append(result.Recipients, RecipientResult{Recipient: rcpt, Success: result.Success, SMTPCode: result.SMTPCode, IsPermanent: result.IsPermanent, Message: result.Message})
			}
		}
		aggregate.Recipients = append(aggregate.Recipients, result.Recipients...)
		aggregate.RemoteHost = result.RemoteHost
		if err != nil {
			failures = append(failures, err.Error())
		}
	}
	aggregate.IsPermanent = true
	for _, r := range aggregate.Recipients {
		if !r.Success {
			aggregate.Success = false
			if !r.IsPermanent {
				aggregate.IsPermanent = false
			}
		}
	}
	if !aggregate.Success {
		aggregate.SMTPCode = 451
		if aggregate.IsPermanent {
			aggregate.SMTPCode = 550
		}
		aggregate.Message = "Delivery incomplete"
		return aggregate, fmt.Errorf("delivery incomplete: %s", strings.Join(failures, "; "))
	}
	aggregate.IsPermanent = false
	return aggregate, nil
}

// deliverToDomain handles delivery to a specific domain
func (d *MailDelivery) deliverToDomain(ctx context.Context, domain, from string, recipients []string, data []byte) (*DeliveryResult, error) {
	// RFC 5321 Section 5 - Address Resolution and Mail Handling
	// Step 1: Perform MX lookup
	if hops := d.route(domain); len(hops) > 0 {
		return d.deliverRoute(ctx, hops, from, recipients, data)
	}
	mxRecords, err := d.resolver.LookupMX(ctx, domain)
	if err != nil {
		d.logger.Warn("MX lookup failed, trying A record",
			zap.String("domain", domain),
			zap.Error(err))

		// Fallback to A record lookup (RFC 5321 Section 5.1)
		mxRecords = []*net.MX{{Host: domain, Pref: 10}}
	}

	if len(mxRecords) == 0 {
		return &DeliveryResult{
			Success:     false,
			SMTPCode:    550,
			Message:     "No MX records found",
			IsPermanent: true,
		}, fmt.Errorf("no MX records for domain: %s", domain)
	}

	// Sort MX records by preference (lower is higher priority)
	d.sortMXRecords(mxRecords)

	// Try each MX host in order of preference
	var lastErr error
	for _, mx := range mxRecords {
		var result *DeliveryResult
		var err error
		enforce := false
		if d.sts != nil {
			enforce, err = d.sts.ShouldEnforceTLS(ctx, domain, mx.Host)
		}
		if err == nil {
			if enforce {
				result, err = d.deliverRoute(ctx, []NextHop{{Address: net.JoinHostPort(strings.TrimSuffix(mx.Host, "."), "25"), RequireTLS: true, reportType: "sts"}}, from, recipients, data)
			} else {
				result, err = d.deliverToMX(ctx, mx.Host, from, recipients, data)
			}
		}

		if err == nil && result.Success {
			return result, nil
		}

		d.logger.Warn("MX delivery failed, trying next",
			zap.String("mx_host", mx.Host),
			zap.Uint16("preference", mx.Pref),
			zap.Error(err))
		lastErr = err

		// If permanent error, don't try other MX hosts
		if result != nil && result.IsPermanent {
			return result, lastErr
		}
	}

	return &DeliveryResult{
		Success:     false,
		SMTPCode:    450,
		Message:     "All MX hosts failed",
		IsPermanent: false,
	}, fmt.Errorf("all MX hosts failed for domain %s: %w", domain, lastErr)
}

// deliverToMX performs actual SMTP delivery to a specific MX host
func (d *MailDelivery) deliverToMX(ctx context.Context, mxHost, from string, recipients []string, data []byte) (*DeliveryResult, error) {
	// Remove trailing dot from MX host
	mxHost = strings.TrimSuffix(mxHost, ".")

	client, err := d.dialSMTP(ctx, mxHost)
	if d.reports != nil && len(recipients) > 0 {
		domain := strings.Split(recipients[0], "@")
		if len(domain) == 2 && (err == nil || strings.Contains(err.Error(), "TLS") || strings.Contains(err.Error(), "DANE")) {
			if e := d.reports.Record(domain[1], mxHost, "no-policy-found", err == nil); e != nil {
				d.logger.Error("TLS report persistence failed", zap.Error(e))
			}
		}
	}
	if err != nil {
		return &DeliveryResult{SMTPCode: 451, Message: err.Error(), RemoteHost: mxHost}, err
	}
	defer client.client.Close()
	return smtpTransaction(client.client, mxHost, from, recipients, data)
}

// getConnection retrieves or creates a connection to the MX host
func (d *MailDelivery) getConnection(ctx context.Context, mxHost string) (*smtpConnection, error) {
	pool := d.getPool(mxHost)

	// Try to get an idle connection first
	select {
	case conn := <-pool.connections:
		// Check if connection is still valid
		if time.Since(conn.lastUsed) < 5*time.Minute {
			if err := conn.client.Noop(); err == nil {
				conn.lastUsed = time.Now()
				return conn, nil
			}
		}
		// Connection is stale, close it
		conn.client.Close()
		pool.mu.Lock()
		pool.openCount--
		pool.mu.Unlock()
	default:
		// No idle connections available
	}

	// Create new connection
	pool.mu.Lock()
	if pool.openCount >= pool.maxOpen {
		pool.mu.Unlock()
		// Wait for an available connection
		select {
		case conn := <-pool.connections:
			conn.lastUsed = time.Now()
			return conn, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(30 * time.Second):
			return nil, fmt.Errorf("connection pool timeout")
		}
	}
	pool.openCount++
	pool.mu.Unlock()

	// Establish new SMTP connection
	conn, err := d.dialSMTP(ctx, mxHost)
	if err != nil {
		pool.mu.Lock()
		pool.openCount--
		pool.mu.Unlock()
		return nil, err
	}

	return conn, nil
}

// dialSMTP establishes an SMTP connection with STARTTLS support
// RFC 3207 - SMTP Service Extension for Secure SMTP over Transport Layer Security
// RFC 7672 - SMTP Security via Opportunistic DANE TLS Authentication
func (d *MailDelivery) dialSMTP(ctx context.Context, mxHost string) (*smtpConnection, error) {
	// Connect to SMTP port 25
	dialer := &net.Dialer{
		Timeout: d.connectTimeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(mxHost, "25"))
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	deadline := time.Now().Add(d.dataTimeout)
	if t, ok := ctx.Deadline(); ok && t.Before(deadline) {
		deadline = t
	}
	conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, mxHost)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SMTP handshake failed: %w", err)
	}

	// Send EHLO with our hostname
	if err := client.Hello(d.hostname); err != nil {
		client.Close()
		return nil, fmt.Errorf("EHLO failed: %w", err)
	}

	// RFC 7672: determine whether DANE TLSA records exist for this MX before
	// deciding the TLS security level. If TLSA records are published, TLS is
	// MANDATORY and authenticated — we must never fall back to cleartext, since
	// doing so would let an active attacker strip TLS (a DANE downgrade attack).
	daneMandatory := false
	if d.daneEnabled && d.daneValidator != nil {
		d.logger.Debug("Checking DANE availability", zap.String("mx_host", mxHost))
		tlsaResult, derr := d.daneValidator.CheckDANEAvailability(ctx, mxHost, 25)
		mandatory, decisionErr := daneTLSRequirement(tlsaResult, derr)
		if decisionErr != nil {
			client.Close()
			return nil, fmt.Errorf("cannot determine DANE policy for %s: %w", mxHost, decisionErr)
		}
		if mandatory {
			daneMandatory = true
			d.logger.Info("DANE records found, TLS is mandatory for this MX",
				zap.String("mx_host", mxHost),
				zap.Int("tlsa_records", len(tlsaResult.Records)),
				zap.Bool("dnssec_valid", tlsaResult.DNSSECValid))
		}
	}

	starttlsOffered, _ := client.Extension("STARTTLS")

	if daneMandatory {
		// DANE TLSA records require authenticated TLS. A server that does not
		// offer STARTTLS, or a handshake that fails DANE verification, MUST cause
		// the delivery attempt to fail rather than continue in the clear.
		if !starttlsOffered {
			client.Close()
			return nil, fmt.Errorf("DANE required for %s but server does not offer STARTTLS", mxHost)
		}
		tlsConfig := d.daneValidator.GetTLSConfig(mxHost, 25)
		if err := client.StartTLS(tlsConfig); err != nil {
			client.Close()
			return nil, fmt.Errorf("DANE-authenticated STARTTLS to %s failed: %w", mxHost, err)
		}
		d.logger.Debug("DANE-authenticated STARTTLS successful", zap.String("mx_host", mxHost))
	} else if starttlsOffered {
		// Opportunistic TLS (RFC 7672 §1.3): the goal is encryption against a
		// passive attacker. MX hostnames rarely carry a PKIX-valid certificate,
		// so we must NOT verify the certificate — verifying would cause most
		// handshakes to fail and silently downgrade to cleartext, which is
		// strictly worse than unauthenticated encryption. Authentication is only
		// provided by DANE (above) or MTA-STS.
		tlsConfig := &tls.Config{
			ServerName:         mxHost,
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // opportunistic: encrypt-don't-authenticate (RFC 7672)
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			// Opportunistic only: a failed handshake may fall back to cleartext.
			d.logger.Warn("Opportunistic STARTTLS failed",
				zap.String("mx_host", mxHost),
				zap.Error(err))
			client.Close()
			return nil, fmt.Errorf("STARTTLS failed: %w", err)
		} else {
			d.logger.Debug("Opportunistic STARTTLS successful", zap.String("mx_host", mxHost))
		}
	}

	return &smtpConnection{
		client:    client,
		host:      mxHost,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
	}, nil
}

// daneTLSRequirement determines whether SMTP can safely use opportunistic TLS.
// RFC 7672 requires a delivery attempt to fail closed when the TLSA lookup or
// DNSSEC validation is indeterminate. Otherwise, an attacker can turn a DANE
// lookup failure into a cleartext delivery downgrade.
func daneTLSRequirement(result *dane.TLSALookupResult, lookupErr error) (bool, error) {
	if lookupErr != nil {
		return false, lookupErr
	}
	if result == nil {
		return false, fmt.Errorf("empty TLSA lookup result")
	}
	if result.DNSSECBogus {
		if result.ErrorReason != "" {
			return false, fmt.Errorf("DNSSEC validation is bogus: %s", result.ErrorReason)
		}
		return false, fmt.Errorf("DNSSEC validation is bogus")
	}

	// Authenticated TLSA records make TLS mandatory. Insecure DNS has no DANE
	// authority, so it is safe to continue with ordinary opportunistic TLS.
	return result.DNSSECValid && len(result.Records) > 0, nil
}

// returnConnection returns a connection to the pool
func (d *MailDelivery) returnConnection(mxHost string, conn *smtpConnection) {
	pool := d.getPool(mxHost)

	// Reset the connection
	conn.client.Reset()
	conn.lastUsed = time.Now()

	// Try to return to pool
	select {
	case pool.connections <- conn:
		// Successfully returned to pool
	default:
		// Pool is full, close the connection
		conn.client.Close()
		pool.mu.Lock()
		pool.openCount--
		pool.mu.Unlock()
	}
}

// getPool retrieves or creates a connection pool for an MX host
func (d *MailDelivery) getPool(mxHost string) *connectionPool {
	d.poolsMu.RLock()
	pool, exists := d.pools[mxHost]
	d.poolsMu.RUnlock()

	if exists {
		return pool
	}

	d.poolsMu.Lock()
	defer d.poolsMu.Unlock()

	// Double-check after acquiring write lock
	if pool, exists := d.pools[mxHost]; exists {
		return pool
	}

	pool = &connectionPool{
		host:        mxHost,
		connections: make(chan *smtpConnection, 5), // Max 5 idle connections per host
		maxIdle:     5,
		maxOpen:     20, // Max 20 concurrent connections per host
		openCount:   0,
	}

	d.pools[mxHost] = pool
	return pool
}

// groupByDomain groups recipients by their domain
func (d *MailDelivery) groupByDomain(recipients []string) map[string][]string {
	groups := make(map[string][]string)

	for _, rcpt := range recipients {
		// Extract domain from email address
		parts := strings.Split(rcpt, "@")
		if len(parts) != 2 {
			d.logger.Warn("Invalid recipient address", zap.String("recipient", rcpt))
			continue
		}

		domain := strings.ToLower(parts[1])
		groups[domain] = append(groups[domain], rcpt)
	}

	return groups
}

// sortMXRecords sorts MX records by preference (lower preference = higher priority)
func (d *MailDelivery) sortMXRecords(mxRecords []*net.MX) {
	// Simple bubble sort (fine for small arrays)
	for i := 0; i < len(mxRecords)-1; i++ {
		for j := 0; j < len(mxRecords)-i-1; j++ {
			if mxRecords[j].Pref > mxRecords[j+1].Pref {
				mxRecords[j], mxRecords[j+1] = mxRecords[j+1], mxRecords[j]
			}
		}
	}
}

// parseSMTPError extracts SMTP code and determines if error is permanent
// RFC 5321 Section 4.2 - SMTP Replies
func parseSMTPError(err error) (code int, isPermanent bool) {
	if err == nil {
		return 250, false
	}

	// Try to extract SMTP error code
	errStr := err.Error()

	// Look for standard SMTP error format: "XXX message"
	if len(errStr) >= 3 {
		var c int
		if _, scanErr := fmt.Sscanf(errStr[:3], "%d", &c); scanErr == nil {
			code = c
		}
	}

	// Default to temporary error if we can't parse
	if code == 0 {
		code = 450
	}

	// RFC 5321: 5xx codes are permanent, 4xx are temporary
	isPermanent = code >= 500 && code < 600

	return code, isPermanent
}

// Shutdown gracefully closes all connection pools
func (d *MailDelivery) Shutdown() error {
	d.poolsMu.Lock()
	defer d.poolsMu.Unlock()

	for host, pool := range d.pools {
		close(pool.connections)

		// Drain and close all connections
		for conn := range pool.connections {
			if err := conn.client.Quit(); err != nil {
				conn.client.Close()
			}
		}

		d.logger.Info("Closed connection pool", zap.String("mx_host", host))
	}

	d.pools = make(map[string]*connectionPool)
	return nil
}

// VerifyConnection tests connectivity to an MX host (used for health checks)
func (d *MailDelivery) VerifyConnection(ctx context.Context, domain string) error {
	mxRecords, err := d.resolver.LookupMX(ctx, domain)
	if err != nil || len(mxRecords) == 0 {
		return fmt.Errorf("no MX records for domain: %s", domain)
	}

	// Try to connect to the first MX
	conn, err := d.dialSMTP(ctx, strings.TrimSuffix(mxRecords[0].Host, "."))
	if err != nil {
		return err
	}

	conn.client.Quit()
	return nil
}

func (d *MailDelivery) SetMTASTS(m *security.MTASTSManager) { d.sts = m }

func (d *MailDelivery) SetTLSReports(r *security.DurableTLSReports) { d.reports = r }
