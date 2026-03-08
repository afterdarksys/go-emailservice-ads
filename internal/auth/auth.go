package auth

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// AuthResult enum for validation checks
type AuthResult int

const (
	ResultNeutral AuthResult = iota
	ResultPass
	ResultFail
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserNotFound       = errors.New("user not found")
	ErrAccountLocked      = errors.New("account temporarily locked due to too many failed attempts")
	ErrRateLimited        = errors.New("too many authentication attempts, please try again later")
)

// User represents an authenticated user
type User struct {
	Username     string
	PasswordHash string
	Email        string
	Enabled      bool
}

// failureRecord tracks failed authentication attempts
type failureRecord struct {
	count         int
	firstAttempt  time.Time
	lastAttempt   time.Time
	lockedUntil   time.Time
}

// UserStore manages user authentication with account lockout protection
type UserStore struct {
	users map[string]*User
	mu    sync.RWMutex

	// Account lockout tracking
	failuresByUsername map[string]*failureRecord
	failuresByIP       map[string]*failureRecord
	failuresMu         sync.RWMutex

	// Lockout configuration
	maxFailures      int           // Max failures before lockout (default 5)
	lockoutDuration  time.Duration // How long to lock account (default 15 minutes)
	failureWindow    time.Duration // Time window for failure tracking (default 1 hour)
}

// NewUserStore creates a new user store with account lockout protection
func NewUserStore() *UserStore {
	return &UserStore{
		users:              make(map[string]*User),
		failuresByUsername: make(map[string]*failureRecord),
		failuresByIP:       make(map[string]*failureRecord),
		maxFailures:        5,
		lockoutDuration:    15 * time.Minute,
		failureWindow:      1 * time.Hour,
	}
}

// AddUser adds a user with a plaintext password (will be hashed)
func (s *UserStore) AddUser(username, password, email string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.users[username] = &User{
		Username:     username,
		PasswordHash: string(hash),
		Email:        email,
		Enabled:      true,
	}
	return nil
}

// Authenticate verifies username and password with account lockout protection
func (s *UserStore) Authenticate(username, password string) (*User, error) {
	return s.AuthenticateWithIP(username, password, "")
}

// AuthenticateWithIP verifies username and password with IP-based rate limiting
func (s *UserStore) AuthenticateWithIP(username, password, ip string) (*User, error) {
	now := time.Now()

	// Check if account is locked by username
	if locked, until := s.isLocked(username, now); locked {
		delay := until.Sub(now)
		return nil, fmt.Errorf("%w (locked for %s)", ErrAccountLocked, delay.Round(time.Second))
	}

	// Check if IP is rate limited
	if ip != "" {
		if locked, until := s.isIPLocked(ip, now); locked {
			delay := until.Sub(now)
			return nil, fmt.Errorf("%w (IP locked for %s)", ErrRateLimited, delay.Round(time.Second))
		}
	}

	// Perform authentication
	s.mu.RLock()
	user, exists := s.users[username]
	s.mu.RUnlock()

	if !exists {
		// Record failure even for non-existent users (prevent enumeration timing attacks)
		s.recordFailure(username, ip, now)

		// Add delay for non-existent users to prevent enumeration
		time.Sleep(100 * time.Millisecond)
		return nil, ErrUserNotFound
	}

	if !user.Enabled {
		s.recordFailure(username, ip, now)
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		s.recordFailure(username, ip, now)
		return nil, ErrInvalidCredentials
	}

	// Successful authentication - clear failure records
	s.clearFailures(username, ip)

	return user, nil
}

// isLocked checks if a username is locked
func (s *UserStore) isLocked(username string, now time.Time) (bool, time.Time) {
	s.failuresMu.RLock()
	defer s.failuresMu.RUnlock()

	record, exists := s.failuresByUsername[username]
	if !exists {
		return false, time.Time{}
	}

	// Check if locked
	if now.Before(record.lockedUntil) {
		return true, record.lockedUntil
	}

	// Check if we're in the failure window with too many attempts
	if now.Sub(record.firstAttempt) <= s.failureWindow && record.count >= s.maxFailures {
		return true, record.lockedUntil
	}

	return false, time.Time{}
}

// isIPLocked checks if an IP is rate limited
func (s *UserStore) isIPLocked(ip string, now time.Time) (bool, time.Time) {
	if ip == "" {
		return false, time.Time{}
	}

	s.failuresMu.RLock()
	defer s.failuresMu.RUnlock()

	record, exists := s.failuresByIP[ip]
	if !exists {
		return false, time.Time{}
	}

	// Check if locked
	if now.Before(record.lockedUntil) {
		return true, record.lockedUntil
	}

	// IP rate limiting: higher threshold (3x username limit)
	maxIPFailures := s.maxFailures * 3
	if now.Sub(record.firstAttempt) <= s.failureWindow && record.count >= maxIPFailures {
		return true, record.lockedUntil
	}

	return false, time.Time{}
}

// recordFailure records an authentication failure with exponential backoff
func (s *UserStore) recordFailure(username, ip string, now time.Time) {
	s.failuresMu.Lock()
	defer s.failuresMu.Unlock()

	// Record failure by username
	if record, exists := s.failuresByUsername[username]; exists {
		// Reset if outside failure window
		if now.Sub(record.firstAttempt) > s.failureWindow {
			record.count = 1
			record.firstAttempt = now
			record.lastAttempt = now
			record.lockedUntil = time.Time{}
		} else {
			record.count++
			record.lastAttempt = now

			// Apply exponential backoff: 2^(failures-maxFailures) minutes
			if record.count >= s.maxFailures {
				exponent := record.count - s.maxFailures
				lockDuration := s.lockoutDuration * time.Duration(1<<uint(exponent))

				// Cap at 24 hours
				if lockDuration > 24*time.Hour {
					lockDuration = 24 * time.Hour
				}

				record.lockedUntil = now.Add(lockDuration)
			}
		}
	} else {
		s.failuresByUsername[username] = &failureRecord{
			count:        1,
			firstAttempt: now,
			lastAttempt:  now,
			lockedUntil:  time.Time{},
		}
	}

	// Record failure by IP (if provided)
	if ip != "" {
		if record, exists := s.failuresByIP[ip]; exists {
			if now.Sub(record.firstAttempt) > s.failureWindow {
				record.count = 1
				record.firstAttempt = now
				record.lastAttempt = now
				record.lockedUntil = time.Time{}
			} else {
				record.count++
				record.lastAttempt = now

				// IP lockout with higher threshold
				maxIPFailures := s.maxFailures * 3
				if record.count >= maxIPFailures {
					exponent := (record.count - maxIPFailures) / 3
					lockDuration := s.lockoutDuration * time.Duration(1<<uint(exponent))

					if lockDuration > 24*time.Hour {
						lockDuration = 24 * time.Hour
					}

					record.lockedUntil = now.Add(lockDuration)
				}
			}
		} else {
			s.failuresByIP[ip] = &failureRecord{
				count:        1,
				firstAttempt: now,
				lastAttempt:  now,
				lockedUntil:  time.Time{},
			}
		}
	}
}

// clearFailures clears failure records after successful authentication
func (s *UserStore) clearFailures(username, ip string) {
	s.failuresMu.Lock()
	defer s.failuresMu.Unlock()

	delete(s.failuresByUsername, username)
	if ip != "" {
		delete(s.failuresByIP, ip)
	}
}

// CleanupExpiredLocks removes expired lockout records
func (s *UserStore) CleanupExpiredLocks() {
	s.failuresMu.Lock()
	defer s.failuresMu.Unlock()

	now := time.Now()

	// Clean username failures
	for username, record := range s.failuresByUsername {
		if now.Sub(record.lastAttempt) > s.failureWindow && now.After(record.lockedUntil) {
			delete(s.failuresByUsername, username)
		}
	}

	// Clean IP failures
	for ip, record := range s.failuresByIP {
		if now.Sub(record.lastAttempt) > s.failureWindow && now.After(record.lockedUntil) {
			delete(s.failuresByIP, ip)
		}
	}
}

// GetLockoutStats returns lockout statistics
func (s *UserStore) GetLockoutStats() map[string]int {
	s.failuresMu.RLock()
	defer s.failuresMu.RUnlock()

	now := time.Now()
	lockedUsers := 0
	lockedIPs := 0

	for _, record := range s.failuresByUsername {
		if now.Before(record.lockedUntil) {
			lockedUsers++
		}
	}

	for _, record := range s.failuresByIP {
		if now.Before(record.lockedUntil) {
			lockedIPs++
		}
	}

	return map[string]int{
		"locked_users":    lockedUsers,
		"locked_ips":      lockedIPs,
		"failed_users":    len(s.failuresByUsername),
		"failed_ips":      len(s.failuresByIP),
	}
}

// GetUser retrieves a user by username
func (s *UserStore) GetUser(username string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, exists := s.users[username]
	return user, exists
}

// Validator handles IP, EHLO, and Sender validation
type Validator struct {
	logger    *zap.Logger
	userStore *UserStore

	// Domain ownership mapping: username -> allowed domains
	domainOwnership   map[string][]string
	domainOwnershipMu sync.RWMutex
}

func NewValidator(logger *zap.Logger) *Validator {
	return &Validator{
		logger:          logger,
		userStore:       NewUserStore(),
		domainOwnership: make(map[string][]string),
	}
}

// GetUserStore returns the internal user store
func (v *Validator) GetUserStore() *UserStore {
	return v.userStore
}

// ValidateIPAndEHLO checks if the sending IP correlates with the EHLO name.
// This is a basic stub that would typically involve DNS PTR checks.
func (v *Validator) ValidateIPAndEHLO(ip, ehlo string) AuthResult {
	v.logger.Debug("Validating IP and EHLO", zap.String("ip", ip), zap.String("ehlo", ehlo))
	// TODO: Implement DNS PTR/A record correlation
	// For now, accept localhost as pass
	if ip == "127.0.0.1" || ip == "::1" {
		return ResultPass
	}
	return ResultNeutral
}

// ValidateWhitelistFrom checks if the FROM address is in an approved explicit whitelist.
// High performance requirement: this could be backed by a highly concurrent map/Redis/Bloom filter.
func (v *Validator) ValidateWhitelistFrom(from string) AuthResult {
	v.logger.Debug("Checking Whitelist FROM", zap.String("from", from))
	// TODO: Implement actual highly-concurrent lookup (e.g., against Redis or a local synced cache)
	if strings.HasSuffix(strings.ToLower(from), "@msgs.global") {
		return ResultPass
	}
	return ResultNeutral
}

// Authenticate verifies username and password credentials
func (v *Validator) Authenticate(username, password string) (*User, error) {
	v.logger.Debug("Authenticating user", zap.String("username", username))
	return v.userStore.Authenticate(username, password)
}

// SASLauthd bindings would go here.
// Typically you would connect to the saslauthd unix socket (/var/run/saslauthd/mux).
func (v *Validator) AuthenticateSASL(username, password string) bool {
	v.logger.Debug("Attempting SASL authentication via saslauthd socket", zap.String("username", username))
	// TODO: UNIX socket dialing and saslauthd protocol implementation
	// For now, use the internal user store
	_, err := v.Authenticate(username, password)
	return err == nil
}

// AuthorizedToSendAs checks if an authenticated user is allowed to send as the given FROM address
// This prevents authenticated users from spoofing other users' addresses
func (v *Validator) AuthorizedToSendAs(username, fromAddress string) bool {
	if username == "" {
		// Unauthenticated users - no authorization check
		return false
	}

	// Extract domain from FROM address
	parts := strings.Split(fromAddress, "@")
	if len(parts) != 2 {
		v.logger.Warn("Invalid FROM address format", zap.String("from", fromAddress))
		return false
	}

	fromUser := parts[0]
	fromDomain := strings.ToLower(parts[1])

	// Check if user owns this domain
	v.domainOwnershipMu.RLock()
	allowedDomains, hasDomains := v.domainOwnership[username]
	v.domainOwnershipMu.RUnlock()

	if hasDomains {
		for _, domain := range allowedDomains {
			if strings.EqualFold(domain, fromDomain) {
				// User owns the domain, allow it
				v.logger.Debug("User authorized for domain",
					zap.String("username", username),
					zap.String("domain", fromDomain))
				return true
			}
		}
	}

	// Default policy: user can send as their own email address
	// Construct expected email from username
	user, exists := v.userStore.GetUser(username)
	if !exists {
		v.logger.Warn("User not found for authorization check", zap.String("username", username))
		return false
	}

	// Allow if FROM matches user's registered email
	if strings.EqualFold(user.Email, fromAddress) {
		v.logger.Debug("User authorized - matches registered email",
			zap.String("username", username),
			zap.String("from", fromAddress))
		return true
	}

	// Allow if FROM uses the same username (e.g., user@domain matches user)
	if strings.EqualFold(fromUser, username) {
		v.logger.Debug("User authorized - username matches",
			zap.String("username", username),
			zap.String("from", fromAddress))
		return true
	}

	v.logger.Warn("User not authorized to send as FROM address",
		zap.String("username", username),
		zap.String("from", fromAddress),
		zap.String("registered_email", user.Email))

	return false
}

// GrantDomainAccess allows a user to send from any address in the specified domain
func (v *Validator) GrantDomainAccess(username, domain string) {
	v.domainOwnershipMu.Lock()
	defer v.domainOwnershipMu.Unlock()

	domain = strings.ToLower(domain)
	if domains, exists := v.domainOwnership[username]; exists {
		// Check if already granted
		for _, d := range domains {
			if d == domain {
				return
			}
		}
		v.domainOwnership[username] = append(domains, domain)
	} else {
		v.domainOwnership[username] = []string{domain}
	}

	v.logger.Info("Granted domain access",
		zap.String("username", username),
		zap.String("domain", domain))
}

// RevokeDomainAccess removes a user's permission to send from a domain
func (v *Validator) RevokeDomainAccess(username, domain string) {
	v.domainOwnershipMu.Lock()
	defer v.domainOwnershipMu.Unlock()

	domain = strings.ToLower(domain)
	if domains, exists := v.domainOwnership[username]; exists {
		newDomains := make([]string, 0, len(domains))
		for _, d := range domains {
			if d != domain {
				newDomains = append(newDomains, d)
			}
		}
		v.domainOwnership[username] = newDomains
	}

	v.logger.Info("Revoked domain access",
		zap.String("username", username),
		zap.String("domain", domain))
}
