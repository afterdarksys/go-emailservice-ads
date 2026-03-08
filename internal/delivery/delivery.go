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
)

// DeliveryResult represents the outcome of a delivery attempt
type DeliveryResult struct {
	Success      bool
	SMTPCode     int
	Message      string
	IsPermanent  bool
	RemoteHost   string
	DeliveredAt  time.Time
}

// MailDelivery handles outbound SMTP mail delivery
// RFC 5321 - Simple Mail Transfer Protocol
type MailDelivery struct {
	logger      *zap.Logger
	resolver    *dns.Resolver
	hostname    string

	// Connection pooling
	pools       map[string]*connectionPool
	poolsMu     sync.RWMutex

	// TLS configuration for outbound STARTTLS (RFC 3207)
	tlsConfig   *tls.Config

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
	client      *smtp.Client
	host        string
	createdAt   time.Time
	lastUsed    time.Time
}

// NewMailDelivery creates a new outbound mail delivery handler
func NewMailDelivery(logger *zap.Logger, resolver *dns.Resolver, hostname string) *MailDelivery {
	return &MailDelivery{
		logger:         logger,
		resolver:       resolver,
		hostname:       hostname,
		pools:          make(map[string]*connectionPool),
		tlsConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: false, // SECURITY: Always verify certificates
			ServerName:         "", // Will be set per connection
		},
		connectTimeout: 30 * time.Second,
		dataTimeout:    5 * time.Minute,
	}
}

// Deliver sends a message to external recipients
// RFC 5321 Section 3.3 - Mail Transactions
func (d *MailDelivery) Deliver(ctx context.Context, from string, to []string, data []byte) (*DeliveryResult, error) {
	if len(to) == 0 {
		return nil, fmt.Errorf("no recipients specified")
	}

	// Group recipients by domain for efficient delivery
	recipientsByDomain := d.groupByDomain(to)

	var lastResult *DeliveryResult
	var lastError error

	// Deliver to each domain
	for domain, recipients := range recipientsByDomain {
		result, err := d.deliverToDomain(ctx, domain, from, recipients, data)
		if err != nil {
			d.logger.Error("Delivery failed for domain",
				zap.String("domain", domain),
				zap.Int("recipients", len(recipients)),
				zap.Error(err))
			lastError = err
			lastResult = result
			continue
		}

		d.logger.Info("Delivery successful",
			zap.String("domain", domain),
			zap.Int("recipients", len(recipients)),
			zap.String("mx_host", result.RemoteHost))
		lastResult = result
	}

	// If all deliveries failed, return the last error
	if lastError != nil && lastResult != nil && !lastResult.Success {
		return lastResult, lastError
	}

	return lastResult, nil
}

// deliverToDomain handles delivery to a specific domain
func (d *MailDelivery) deliverToDomain(ctx context.Context, domain, from string, recipients []string, data []byte) (*DeliveryResult, error) {
	// RFC 5321 Section 5 - Address Resolution and Mail Handling
	// Step 1: Perform MX lookup
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
		result, err := d.deliverToMX(ctx, mx.Host, from, recipients, data)
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

	// Get or create connection
	client, err := d.getConnection(ctx, mxHost)
	if err != nil {
		return &DeliveryResult{
			Success:     false,
			SMTPCode:    421,
			Message:     fmt.Sprintf("Connection failed: %v", err),
			IsPermanent: false,
			RemoteHost:  mxHost,
		}, err
	}
	defer d.returnConnection(mxHost, client)

	// RFC 5321 Section 3.3 - Mail Transaction
	// MAIL FROM
	if err := client.client.Mail(from); err != nil {
		code, isPermanent := parseSMTPError(err)
		return &DeliveryResult{
			Success:     false,
			SMTPCode:    code,
			Message:     err.Error(),
			IsPermanent: isPermanent,
			RemoteHost:  mxHost,
		}, err
	}

	// RCPT TO for each recipient
	for _, rcpt := range recipients {
		if err := client.client.Rcpt(rcpt); err != nil {
			code, isPermanent := parseSMTPError(err)
			d.logger.Warn("RCPT TO failed",
				zap.String("recipient", rcpt),
				zap.String("mx_host", mxHost),
				zap.Error(err))

			// Continue with other recipients even if one fails
			if isPermanent {
				continue
			}
			return &DeliveryResult{
				Success:     false,
				SMTPCode:    code,
				Message:     err.Error(),
				IsPermanent: isPermanent,
				RemoteHost:  mxHost,
			}, err
		}
	}

	// DATA
	w, err := client.client.Data()
	if err != nil {
		code, isPermanent := parseSMTPError(err)
		return &DeliveryResult{
			Success:     false,
			SMTPCode:    code,
			Message:     err.Error(),
			IsPermanent: isPermanent,
			RemoteHost:  mxHost,
		}, err
	}

	if _, err := w.Write(data); err != nil {
		w.Close()
		return &DeliveryResult{
			Success:     false,
			SMTPCode:    451,
			Message:     fmt.Sprintf("Data write failed: %v", err),
			IsPermanent: false,
			RemoteHost:  mxHost,
		}, err
	}

	if err := w.Close(); err != nil {
		code, isPermanent := parseSMTPError(err)
		return &DeliveryResult{
			Success:     false,
			SMTPCode:    code,
			Message:     err.Error(),
			IsPermanent: isPermanent,
			RemoteHost:  mxHost,
		}, err
	}

	return &DeliveryResult{
		Success:     true,
		SMTPCode:    250,
		Message:     "Message accepted",
		IsPermanent: false,
		RemoteHost:  mxHost,
		DeliveredAt: time.Now(),
	}, nil
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
func (d *MailDelivery) dialSMTP(ctx context.Context, mxHost string) (*smtpConnection, error) {
	// Connect to SMTP port 25
	dialer := &net.Dialer{
		Timeout: d.connectTimeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(mxHost, "25"))
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}

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

	// Attempt opportunistic STARTTLS (RFC 3207)
	// We don't fail if STARTTLS is not supported, but we try if available
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := d.tlsConfig.Clone()
		tlsConfig.ServerName = mxHost

		if err := client.StartTLS(tlsConfig); err != nil {
			d.logger.Warn("STARTTLS failed, continuing without TLS",
				zap.String("mx_host", mxHost),
				zap.Error(err))
		} else {
			d.logger.Debug("STARTTLS successful",
				zap.String("mx_host", mxHost))
		}
	}

	return &smtpConnection{
		client:    client,
		host:      mxHost,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
	}, nil
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
