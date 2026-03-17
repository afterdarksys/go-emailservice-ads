package directory

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-ldap/ldap/v3"
	"go.uber.org/zap"
)

// LDAPConfig holds LDAP connection configuration
type LDAPConfig struct {
	ServerURL      string
	BaseDN         string
	BindDN         string
	BindPW         string
	UseTLS         bool
	MaxConnections int // Maximum number of connections in pool
}

// ldapConnectionPool manages a pool of LDAP connections
type ldapConnectionPool struct {
	config    *LDAPConfig
	pool      chan *ldap.Conn
	mu        sync.Mutex
	logger    *zap.Logger
	connCount int
}

// NextMailHopResolver resolves next mail hop from LDAP
type NextMailHopResolver struct {
	config *LDAPConfig
	pool   *ldapConnectionPool
	logger *zap.Logger
}

// newLDAPConnectionPool creates a new LDAP connection pool
func newLDAPConnectionPool(config *LDAPConfig, logger *zap.Logger) (*ldapConnectionPool, error) {
	maxConn := config.MaxConnections
	if maxConn <= 0 {
		maxConn = 10 // Default pool size
	}

	pool := &ldapConnectionPool{
		config:    config,
		pool:      make(chan *ldap.Conn, maxConn),
		logger:    logger,
		connCount: 0,
	}

	// Pre-populate pool with initial connections
	for i := 0; i < 2; i++ {
		conn, err := pool.createConnection()
		if err != nil {
			logger.Warn("Failed to create initial LDAP connection", zap.Error(err))
			continue
		}
		pool.pool <- conn
		pool.connCount++
	}

	return pool, nil
}

// createConnection creates a new LDAP connection
func (p *ldapConnectionPool) createConnection() (*ldap.Conn, error) {
	conn, err := ldap.DialURL(p.config.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to dial LDAP: %w", err)
	}

	// Bind with service account
	if p.config.BindDN != "" {
		err = conn.Bind(p.config.BindDN, p.config.BindPW)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to bind to LDAP: %w", err)
		}
	}

	return conn, nil
}

// getConnection gets a connection from the pool or creates a new one
func (p *ldapConnectionPool) getConnection() (*ldap.Conn, error) {
	select {
	case conn := <-p.pool:
		// Test connection is still alive
		_, err := conn.Search(ldap.NewSearchRequest(
			"",
			ldap.ScopeBaseObject,
			ldap.NeverDerefAliases,
			0, 0, false,
			"(objectClass=*)",
			[]string{"1.1"},
			nil,
		))
		if err != nil {
			// Connection is dead, create a new one
			p.logger.Debug("LDAP connection test failed, creating new connection")
			conn.Close()
			return p.createConnection()
		}
		return conn, nil
	default:
		// Pool is empty, check if we can create more connections
		p.mu.Lock()
		if p.connCount < p.config.MaxConnections {
			p.connCount++
			p.mu.Unlock()
			return p.createConnection()
		}
		p.mu.Unlock()

		// Pool is at max, wait for available connection
		conn := <-p.pool
		return conn, nil
	}
}

// returnConnection returns a connection to the pool
func (p *ldapConnectionPool) returnConnection(conn *ldap.Conn) {
	if conn == nil {
		return
	}

	select {
	case p.pool <- conn:
		// Successfully returned to pool
	default:
		// Pool is full, close the connection
		conn.Close()
		p.mu.Lock()
		p.connCount--
		p.mu.Unlock()
	}
}

// close closes all connections in the pool
func (p *ldapConnectionPool) close() {
	close(p.pool)
	for conn := range p.pool {
		conn.Close()
	}
}

// NewNextMailHopResolver creates a new next mail hop resolver with connection pooling
func NewNextMailHopResolver(config *LDAPConfig, logger *zap.Logger) (*NextMailHopResolver, error) {
	pool, err := newLDAPConnectionPool(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create LDAP connection pool: %w", err)
	}

	return &NextMailHopResolver{
		config: config,
		pool:   pool,
		logger: logger,
	}, nil
}

// Close closes the LDAP connection pool
func (r *NextMailHopResolver) Close() error {
	if r.pool != nil {
		r.pool.close()
	}
	return nil
}

// ResolveNextHop resolves the next mail hop for an email address from LDAP
func (r *NextMailHopResolver) ResolveNextHop(emailAddress string) (string, error) {
	// Parse email address
	parts := strings.Split(emailAddress, "@")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid email address: %s", emailAddress)
	}

	// Get connection from pool
	conn, err := r.pool.getConnection()
	if err != nil {
		return "", fmt.Errorf("failed to get LDAP connection: %w", err)
	}
	defer r.pool.returnConnection(conn)

	// Search for user in LDAP
	searchRequest := ldap.NewSearchRequest(
		r.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0, // No size limit
		0, // No time limit
		false,
		fmt.Sprintf("(&(objectClass=inetOrgPerson)(mail=%s))", ldap.EscapeFilter(emailAddress)),
		[]string{"nextmailhop", "mail", "mailAlternateAddress", "mailHost", "mailRoutingAddress"},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return "", fmt.Errorf("LDAP search failed: %w", err)
	}

	if len(result.Entries) == 0 {
		r.logger.Debug("No LDAP entry found for email",
			zap.String("email", emailAddress))
		return "", fmt.Errorf("user not found: %s", emailAddress)
	}

	entry := result.Entries[0]

	// Check for nextmailhop attribute (Postfix-style)
	if nextHop := entry.GetAttributeValue("nextmailhop"); nextHop != "" {
		r.logger.Info("Resolved next mail hop from LDAP",
			zap.String("email", emailAddress),
			zap.String("next_hop", nextHop))
		return nextHop, nil
	}

	// Check for mailRoutingAddress (alternative attribute)
	if routingAddr := entry.GetAttributeValue("mailRoutingAddress"); routingAddr != "" {
		r.logger.Info("Resolved mail routing from LDAP",
			zap.String("email", emailAddress),
			zap.String("routing_address", routingAddr))
		return routingAddr, nil
	}

	// Check for mailHost (server-based routing)
	if mailHost := entry.GetAttributeValue("mailHost"); mailHost != "" {
		// Convert mailHost to transport format: smtp:[mailHost]
		nextHop := fmt.Sprintf("smtp:[%s]", mailHost)
		r.logger.Info("Resolved mail host from LDAP",
			zap.String("email", emailAddress),
			zap.String("next_hop", nextHop))
		return nextHop, nil
	}

	// No routing information found
	r.logger.Debug("No routing information in LDAP for email",
		zap.String("email", emailAddress))
	return "", fmt.Errorf("no routing information for: %s", emailAddress)
}

// ResolveNextHopBulk resolves next hops for multiple addresses
func (r *NextMailHopResolver) ResolveNextHopBulk(addresses []string) (map[string]string, error) {
	results := make(map[string]string)

	for _, addr := range addresses {
		if nextHop, err := r.ResolveNextHop(addr); err == nil {
			results[addr] = nextHop
		}
	}

	return results, nil
}

// SetNextHop sets the nextmailhop attribute for a user
func (r *NextMailHopResolver) SetNextHop(emailAddress, nextHop string) error {
	// Get connection from pool
	conn, err := r.pool.getConnection()
	if err != nil {
		return fmt.Errorf("failed to get LDAP connection: %w", err)
	}
	defer r.pool.returnConnection(conn)

	// Search for user DN
	searchRequest := ldap.NewSearchRequest(
		r.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		fmt.Sprintf("(&(objectClass=inetOrgPerson)(mail=%s))", ldap.EscapeFilter(emailAddress)),
		[]string{"dn"},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return fmt.Errorf("LDAP search failed: %w", err)
	}

	if len(result.Entries) == 0 {
		return fmt.Errorf("user not found: %s", emailAddress)
	}

	userDN := result.Entries[0].DN

	// Modify nextmailhop attribute
	modifyRequest := ldap.NewModifyRequest(userDN, nil)
	modifyRequest.Replace("nextmailhop", []string{nextHop})

	if err := conn.Modify(modifyRequest); err != nil {
		return fmt.Errorf("failed to set nextmailhop: %w", err)
	}

	r.logger.Info("Set next mail hop in LDAP",
		zap.String("email", emailAddress),
		zap.String("next_hop", nextHop))

	return nil
}

// RemoveNextHop removes the nextmailhop attribute from a user
func (r *NextMailHopResolver) RemoveNextHop(emailAddress string) error {
	// Get connection from pool
	conn, err := r.pool.getConnection()
	if err != nil {
		return fmt.Errorf("failed to get LDAP connection: %w", err)
	}
	defer r.pool.returnConnection(conn)

	// Search for user DN
	searchRequest := ldap.NewSearchRequest(
		r.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		fmt.Sprintf("(&(objectClass=inetOrgPerson)(mail=%s))", ldap.EscapeFilter(emailAddress)),
		[]string{"dn"},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return fmt.Errorf("LDAP search failed: %w", err)
	}

	if len(result.Entries) == 0 {
		return fmt.Errorf("user not found: %s", emailAddress)
	}

	userDN := result.Entries[0].DN

	// Delete nextmailhop attribute
	modifyRequest := ldap.NewModifyRequest(userDN, nil)
	modifyRequest.Delete("nextmailhop", []string{})

	if err := conn.Modify(modifyRequest); err != nil {
		return fmt.Errorf("failed to remove nextmailhop: %w", err)
	}

	r.logger.Info("Removed next mail hop from LDAP",
		zap.String("email", emailAddress))

	return nil
}

// ListUsersWithNextHop lists all users with nextmailhop attribute
func (r *NextMailHopResolver) ListUsersWithNextHop() (map[string]string, error) {
	// Get connection from pool
	conn, err := r.pool.getConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get LDAP connection: %w", err)
	}
	defer r.pool.returnConnection(conn)

	searchRequest := ldap.NewSearchRequest(
		r.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		"(&(objectClass=inetOrgPerson)(nextmailhop=*))",
		[]string{"mail", "nextmailhop"},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("LDAP search failed: %w", err)
	}

	users := make(map[string]string)
	for _, entry := range result.Entries {
		email := entry.GetAttributeValue("mail")
		nextHop := entry.GetAttributeValue("nextmailhop")
		if email != "" && nextHop != "" {
			users[email] = nextHop
		}
	}

	return users, nil
}

// ValidateNextHop validates a nextmailhop value format
func (r *NextMailHopResolver) ValidateNextHop(nextHop string) error {
	if nextHop == "" {
		return fmt.Errorf("nextmailhop cannot be empty")
	}

	// Valid formats:
	// 1. transport:nexthop (e.g., smtp:mail.example.com)
	// 2. transport:[nexthop] (e.g., smtp:[mail.example.com])
	// 3. transport:[nexthop]:port (e.g., smtp:[mail.example.com]:25)
	// 4. Just a hostname (e.g., mail.example.com)

	// Check for transport prefix
	if strings.Contains(nextHop, ":") {
		parts := strings.SplitN(nextHop, ":", 2)
		transport := parts[0]

		// Valid transports
		validTransports := map[string]bool{
			"smtp":   true,
			"lmtp":   true,
			"relay":  true,
			"local":  true,
			"virtual": true,
			"error":  true,
		}

		if !validTransports[transport] {
			return fmt.Errorf("invalid transport: %s", transport)
		}
	}

	return nil
}

// BulkSetNextHop sets nextmailhop for multiple users
func (r *NextMailHopResolver) BulkSetNextHop(mappings map[string]string) error {
	errors := make([]string, 0)

	for email, nextHop := range mappings {
		if err := r.SetNextHop(email, nextHop); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", email, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("bulk set failed for %d users: %s", len(errors), strings.Join(errors, "; "))
	}

	return nil
}

// GetUserRoutingInfo gets complete routing information for a user
type UserRoutingInfo struct {
	Email              string `json:"email"`
	NextMailHop        string `json:"next_mail_hop,omitempty"`
	MailRoutingAddress string `json:"mail_routing_address,omitempty"`
	MailHost           string `json:"mail_host,omitempty"`
	PrimaryMail        string `json:"primary_mail"`
	AlternateAddresses []string `json:"alternate_addresses,omitempty"`
}

func (r *NextMailHopResolver) GetUserRoutingInfo(emailAddress string) (*UserRoutingInfo, error) {
	// Get connection from pool
	conn, err := r.pool.getConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get LDAP connection: %w", err)
	}
	defer r.pool.returnConnection(conn)

	searchRequest := ldap.NewSearchRequest(
		r.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		fmt.Sprintf("(&(objectClass=inetOrgPerson)(mail=%s))", ldap.EscapeFilter(emailAddress)),
		[]string{"mail", "nextmailhop", "mailRoutingAddress", "mailHost", "mailAlternateAddress"},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("LDAP search failed: %w", err)
	}

	if len(result.Entries) == 0 {
		return nil, fmt.Errorf("user not found: %s", emailAddress)
	}

	entry := result.Entries[0]

	info := &UserRoutingInfo{
		Email:              entry.GetAttributeValue("mail"),
		NextMailHop:        entry.GetAttributeValue("nextmailhop"),
		MailRoutingAddress: entry.GetAttributeValue("mailRoutingAddress"),
		MailHost:           entry.GetAttributeValue("mailHost"),
		PrimaryMail:        entry.GetAttributeValue("mail"),
		AlternateAddresses: entry.GetAttributeValues("mailAlternateAddress"),
	}

	return info, nil
}
