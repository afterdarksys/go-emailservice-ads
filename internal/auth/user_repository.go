package auth

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

// UserRepository provides persistence for users and their entitlements.
//
// Two backends are supported, selected by the connection string:
//   - postgres:// or postgresql:// URLs (or key=value DSNs) use PostgreSQL
//   - anything else is treated as a SQLite file path (e.g. ./data/users.db),
//     matching the mailbox store's local-storage model
type UserRepository struct {
	db     *sql.DB
	driver string // "postgres" or "sqlite"
	logger *zap.Logger
}

func isPostgresDSN(connStr string) bool {
	return strings.HasPrefix(connStr, "postgres://") ||
		strings.HasPrefix(connStr, "postgresql://") ||
		strings.Contains(connStr, "host=") ||
		strings.Contains(connStr, "dbname=")
}

// NewUserRepository creates a new user repository backed by PostgreSQL or SQLite.
func NewUserRepository(connStr string, logger *zap.Logger) (*UserRepository, error) {
	driver := "sqlite"
	dsn := connStr
	if isPostgresDSN(connStr) {
		driver = "postgres"
	} else if !strings.HasPrefix(connStr, "file:") {
		// Plain file path: ensure the parent directory exists and enable the
		// pragmas a concurrent server needs (FK cascades, WAL, busy timeout).
		if dir := filepath.Dir(connStr); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("failed to create user db directory: %w", err)
			}
		}
		dsn = "file:" + connStr + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open user database: %w", err)
	}

	// Configure connection pool. SQLite gets a single connection: it has one
	// writer, and a pool of connections just manufactures SQLITE_BUSY errors.
	if driver == "postgres" {
		db.SetMaxOpenConns(50)
		db.SetMaxIdleConns(10)
		db.SetConnMaxLifetime(5 * time.Minute)
		db.SetConnMaxIdleTime(1 * time.Minute)
	} else {
		db.SetMaxOpenConns(1)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping user database: %w", err)
	}

	repo := &UserRepository{
		db:     db,
		driver: driver,
		logger: logger,
	}

	// Initialize schema
	if err := repo.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	logger.Info("Connected to user database", zap.String("driver", driver))
	return repo, nil
}

// initSchema creates tables if they don't exist
func (r *UserRepository) initSchema() error {
	var schema string
	if r.driver == "postgres" {
		schema = `
			CREATE TABLE IF NOT EXISTS users (
				username VARCHAR(255) PRIMARY KEY,
				password_hash VARCHAR(255) NOT NULL,
				email VARCHAR(255) NOT NULL,
				enabled BOOLEAN NOT NULL DEFAULT TRUE,
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
				last_login TIMESTAMP,
				metadata JSONB
			);

			CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
			CREATE INDEX IF NOT EXISTS idx_users_enabled ON users(enabled);

			CREATE TABLE IF NOT EXISTS user_domain_entitlements (
				id BIGSERIAL PRIMARY KEY,
				username VARCHAR(255) NOT NULL REFERENCES users(username) ON DELETE CASCADE,
				domain VARCHAR(255) NOT NULL,
				granted_at TIMESTAMP NOT NULL DEFAULT NOW(),
				granted_by VARCHAR(255),
				notes TEXT,
				UNIQUE(username, domain)
			);

			CREATE INDEX IF NOT EXISTS idx_domain_entitlements_username ON user_domain_entitlements(username);
			CREATE INDEX IF NOT EXISTS idx_domain_entitlements_domain ON user_domain_entitlements(domain);

			CREATE TABLE IF NOT EXISTS user_quotas (
				username VARCHAR(255) PRIMARY KEY REFERENCES users(username) ON DELETE CASCADE,
				max_messages_per_hour INTEGER DEFAULT 100,
				max_messages_per_day INTEGER DEFAULT 1000,
				max_recipients_per_message INTEGER DEFAULT 50,
				max_message_size_bytes BIGINT DEFAULT 26214400, -- 25MB
				updated_at TIMESTAMP NOT NULL DEFAULT NOW()
			);
		`
	} else {
		schema = `
			CREATE TABLE IF NOT EXISTS users (
				username TEXT PRIMARY KEY,
				password_hash TEXT NOT NULL,
				email TEXT NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 1,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_login TIMESTAMP,
				metadata TEXT
			);

			CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
			CREATE INDEX IF NOT EXISTS idx_users_enabled ON users(enabled);

			CREATE TABLE IF NOT EXISTS user_domain_entitlements (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				username TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
				domain TEXT NOT NULL,
				granted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				granted_by TEXT,
				notes TEXT,
				UNIQUE(username, domain)
			);

			CREATE INDEX IF NOT EXISTS idx_domain_entitlements_username ON user_domain_entitlements(username);
			CREATE INDEX IF NOT EXISTS idx_domain_entitlements_domain ON user_domain_entitlements(domain);

			CREATE TABLE IF NOT EXISTS user_quotas (
				username TEXT PRIMARY KEY REFERENCES users(username) ON DELETE CASCADE,
				max_messages_per_hour INTEGER DEFAULT 100,
				max_messages_per_day INTEGER DEFAULT 1000,
				max_recipients_per_message INTEGER DEFAULT 50,
				max_message_size_bytes INTEGER DEFAULT 26214400, -- 25MB
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
		`
	}

	if _, err := r.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	if _, err := r.db.Exec(`CREATE TABLE IF NOT EXISTS scim_identities (
 id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE REFERENCES users(username) ON DELETE CASCADE, external_id TEXT NOT NULL DEFAULT '')`); err != nil {
		return err
	}
	r.logger.Info("User database schema initialized")
	return nil
}

// SaveUser creates or updates a user
func (r *UserRepository) SaveUser(ctx context.Context, user *User) error {
	// Timestamps are computed in Go so the statement is portable across
	// PostgreSQL and SQLite.
	query := `
		INSERT INTO users (username, password_hash, email, enabled, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (username) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			email = EXCLUDED.email,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
	`

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, query, user.Username, user.PasswordHash, user.Email, user.Enabled, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to save user: %w", err)
	}

	if user.SCIMID != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO scim_identities(id,username,external_id) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET external_id=EXCLUDED.external_id`, user.SCIMID, user.Username, user.ExternalID); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.logger.Info("Saved user to database", zap.String("username", user.Username))
	return nil
}

// GetUser retrieves a user by username
func (r *UserRepository) GetUser(ctx context.Context, username string) (*User, error) {
	query := `
		SELECT u.username, u.password_hash, u.email, u.enabled, COALESCE(i.id,''), COALESCE(i.external_id,'')
		FROM users u LEFT JOIN scim_identities i ON i.username=u.username
		WHERE u.username = $1
	`

	var user User
	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&user.Username,
		&user.PasswordHash,
		&user.Email,
		&user.Enabled, &user.SCIMID, &user.ExternalID,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// ListUsers returns all users
func (r *UserRepository) ListUsers(ctx context.Context) ([]*User, error) {
	query := `
		SELECT u.username, u.password_hash, u.email, u.enabled, COALESCE(i.id,''), COALESCE(i.external_id,'')
		FROM users u LEFT JOIN scim_identities i ON i.username=u.username
		ORDER BY u.username
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.Username, &user.PasswordHash, &user.Email, &user.Enabled, &user.SCIMID, &user.ExternalID); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	return users, rows.Err()
}

// DeleteUser removes a user and all their entitlements
func (r *UserRepository) DeleteUser(ctx context.Context, username string) error {
	query := `DELETE FROM users WHERE username = $1`

	result, err := r.db.ExecContext(ctx, query, username)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return ErrUserNotFound
	}

	r.logger.Info("Deleted user from database", zap.String("username", username))
	return nil
}

// GrantDomainEntitlement grants a user permission to send from a domain
func (r *UserRepository) GrantDomainEntitlement(ctx context.Context, username, domain, grantedBy, notes string) error {
	query := `
		INSERT INTO user_domain_entitlements (username, domain, granted_by, notes, granted_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (username, domain) DO UPDATE SET
			granted_by = EXCLUDED.granted_by,
			notes = EXCLUDED.notes,
			granted_at = EXCLUDED.granted_at
	`

	_, err := r.db.ExecContext(ctx, query, username, domain, grantedBy, notes, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to grant domain entitlement: %w", err)
	}

	r.logger.Info("Granted domain entitlement",
		zap.String("username", username),
		zap.String("domain", domain),
		zap.String("granted_by", grantedBy))

	return nil
}

// RevokeDomainEntitlement removes a user's permission to send from a domain
func (r *UserRepository) RevokeDomainEntitlement(ctx context.Context, username, domain string) error {
	query := `DELETE FROM user_domain_entitlements WHERE username = $1 AND domain = $2`

	result, err := r.db.ExecContext(ctx, query, username, domain)
	if err != nil {
		return fmt.Errorf("failed to revoke domain entitlement: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("entitlement not found")
	}

	r.logger.Info("Revoked domain entitlement",
		zap.String("username", username),
		zap.String("domain", domain))

	return nil
}

// GetUserDomainEntitlements returns all domains a user is entitled to send from
func (r *UserRepository) GetUserDomainEntitlements(ctx context.Context, username string) ([]string, error) {
	query := `
		SELECT domain
		FROM user_domain_entitlements
		WHERE username = $1
		ORDER BY domain
	`

	rows, err := r.db.QueryContext(ctx, query, username)
	if err != nil {
		return nil, fmt.Errorf("failed to get domain entitlements: %w", err)
	}
	defer rows.Close()

	var domains []string
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, fmt.Errorf("failed to scan domain: %w", err)
		}
		domains = append(domains, domain)
	}

	return domains, rows.Err()
}

// ListDomainEntitlements returns all domain entitlements with user information
func (r *UserRepository) ListDomainEntitlements(ctx context.Context) ([]DomainEntitlement, error) {
	query := `
		SELECT username, domain, granted_at, granted_by, notes
		FROM user_domain_entitlements
		ORDER BY username, domain
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list domain entitlements: %w", err)
	}
	defer rows.Close()

	var entitlements []DomainEntitlement
	for rows.Next() {
		var ent DomainEntitlement
		var grantedBy sql.NullString
		var notes sql.NullString

		if err := rows.Scan(&ent.Username, &ent.Domain, &ent.GrantedAt, &grantedBy, &notes); err != nil {
			return nil, fmt.Errorf("failed to scan entitlement: %w", err)
		}

		if grantedBy.Valid {
			ent.GrantedBy = grantedBy.String
		}
		if notes.Valid {
			ent.Notes = notes.String
		}

		entitlements = append(entitlements, ent)
	}

	return entitlements, rows.Err()
}

// SetUserQuota sets resource limits for a user
func (r *UserRepository) SetUserQuota(ctx context.Context, username string, quota *UserQuota) error {
	query := `
		INSERT INTO user_quotas (username, max_messages_per_hour, max_messages_per_day, max_recipients_per_message, max_message_size_bytes, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (username) DO UPDATE SET
			max_messages_per_hour = EXCLUDED.max_messages_per_hour,
			max_messages_per_day = EXCLUDED.max_messages_per_day,
			max_recipients_per_message = EXCLUDED.max_recipients_per_message,
			max_message_size_bytes = EXCLUDED.max_message_size_bytes,
			updated_at = EXCLUDED.updated_at
	`

	_, err := r.db.ExecContext(ctx, query, username, quota.MaxMessagesPerHour, quota.MaxMessagesPerDay, quota.MaxRecipientsPerMessage, quota.MaxMessageSizeBytes, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to set user quota: %w", err)
	}

	r.logger.Info("Set user quota", zap.String("username", username))
	return nil
}

// GetUserQuota retrieves resource limits for a user
func (r *UserRepository) GetUserQuota(ctx context.Context, username string) (*UserQuota, error) {
	query := `
		SELECT max_messages_per_hour, max_messages_per_day, max_recipients_per_message, max_message_size_bytes
		FROM user_quotas
		WHERE u.username = $1
	`

	var quota UserQuota
	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&quota.MaxMessagesPerHour,
		&quota.MaxMessagesPerDay,
		&quota.MaxRecipientsPerMessage,
		&quota.MaxMessageSizeBytes,
	)

	if err == sql.ErrNoRows {
		// Return default quota
		return &UserQuota{
			MaxMessagesPerHour:      100,
			MaxMessagesPerDay:       1000,
			MaxRecipientsPerMessage: 50,
			MaxMessageSizeBytes:     26214400, // 25MB
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user quota: %w", err)
	}

	return &quota, nil
}

// UpdateLastLogin updates the user's last login timestamp
func (r *UserRepository) UpdateLastLogin(ctx context.Context, username string) error {
	query := `UPDATE users SET last_login = $2 WHERE username = $1`

	_, err := r.db.ExecContext(ctx, query, username, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to update last login: %w", err)
	}

	return nil
}

// LoadAllUsers loads all users from database into memory (for UserStore initialization)
func (r *UserRepository) LoadAllUsers(ctx context.Context) (map[string]*User, error) {
	users, err := r.ListUsers(ctx)
	if err != nil {
		return nil, err
	}

	userMap := make(map[string]*User)
	for _, user := range users {
		userMap[user.Username] = user
	}

	r.logger.Info("Loaded users from database", zap.Int("count", len(userMap)))
	return userMap, nil
}

// LoadAllDomainEntitlements loads all domain entitlements from database into
// memory. Rows are scanned individually — no engine-specific aggregation — so
// the same query works on PostgreSQL and SQLite.
func (r *UserRepository) LoadAllDomainEntitlements(ctx context.Context) (map[string][]string, error) {
	query := `
		SELECT username, domain
		FROM user_domain_entitlements
		ORDER BY username, domain
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to load domain entitlements: %w", err)
	}
	defer rows.Close()

	domainMap := make(map[string][]string)
	for rows.Next() {
		var username, domain string
		if err := rows.Scan(&username, &domain); err != nil {
			return nil, fmt.Errorf("failed to scan domain entitlements: %w", err)
		}
		domainMap[username] = append(domainMap[username], domain)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to load domain entitlements: %w", err)
	}

	r.logger.Info("Loaded domain entitlements from database", zap.Int("users", len(domainMap)))
	return domainMap, nil
}

// Close closes the database connection
func (r *UserRepository) Close() error {
	return r.db.Close()
}

// DomainEntitlement represents a domain access grant
type DomainEntitlement struct {
	Username  string    `json:"username"`
	Domain    string    `json:"domain"`
	GrantedAt time.Time `json:"granted_at"`
	GrantedBy string    `json:"granted_by,omitempty"`
	Notes     string    `json:"notes,omitempty"`
}

// UserQuota represents resource limits for a user
type UserQuota struct {
	MaxMessagesPerHour      int   `json:"max_messages_per_hour"`
	MaxMessagesPerDay       int   `json:"max_messages_per_day"`
	MaxRecipientsPerMessage int   `json:"max_recipients_per_message"`
	MaxMessageSizeBytes     int64 `json:"max_message_size_bytes"`
}
