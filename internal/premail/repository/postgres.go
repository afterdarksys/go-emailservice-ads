package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/premail/scoring"
)

// PostgresRepository implements Repository interface using PostgreSQL
type PostgresRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewPostgresRepository creates a new PostgreSQL repository
func NewPostgresRepository(connStr string, logger *zap.Logger) (*PostgresRepository, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for production workloads
	// These settings prevent connection exhaustion under high load
	db.SetMaxOpenConns(100)                 // Maximum open connections
	db.SetMaxIdleConns(25)                  // Idle connections to keep for reuse
	db.SetConnMaxLifetime(5 * time.Minute)  // Max lifetime of connections
	db.SetConnMaxIdleTime(1 * time.Minute)  // Max idle time before closing

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	repo := &PostgresRepository{
		db:     db,
		logger: logger,
	}

	// Initialize schema
	if err := repo.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	logger.Info("Connected to PostgreSQL database",
		zap.Int("max_open_conns", 100),
		zap.Int("max_idle_conns", 25))

	return repo, nil
}

// initSchema creates tables if they don't exist
func (r *PostgresRepository) initSchema() error {
	schema := `
		-- IP Characteristics table
		CREATE TABLE IF NOT EXISTS ip_characteristics (
			id BIGSERIAL PRIMARY KEY,
			ip_address INET NOT NULL UNIQUE,
			first_seen TIMESTAMP NOT NULL DEFAULT NOW(),
			last_seen TIMESTAMP NOT NULL DEFAULT NOW(),

			-- Connection metrics
			total_connections BIGINT DEFAULT 0,
			quick_disconnects BIGINT DEFAULT 0,
			pre_banner_talks BIGINT DEFAULT 0,
			avg_connection_time_ms BIGINT DEFAULT 0,

			-- Volume metrics
			messages_sent BIGINT DEFAULT 0,
			recipients_count BIGINT DEFAULT 0,
			failed_auth_attempts BIGINT DEFAULT 0,

			-- Scoring
			current_score INTEGER DEFAULT 0,
			max_score_7d INTEGER DEFAULT 0,
			violation_count BIGINT DEFAULT 0,

			-- Reputation
			reputation_class VARCHAR(20) DEFAULT 'unknown',
			is_blocklisted BOOLEAN DEFAULT FALSE,
			blocklist_expires TIMESTAMP,

			-- Metadata
			last_violation TIMESTAMP,
			notes TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_ip_score ON ip_characteristics(current_score DESC);
		CREATE INDEX IF NOT EXISTS idx_ip_blocklist ON ip_characteristics(is_blocklisted, blocklist_expires);
		CREATE INDEX IF NOT EXISTS idx_ip_last_seen ON ip_characteristics(last_seen DESC);

		-- Hourly Spammer Stats table
		CREATE TABLE IF NOT EXISTS hourly_spammer_stats (
			id BIGSERIAL PRIMARY KEY,
			hour_bucket TIMESTAMP NOT NULL,
			ip_address INET NOT NULL,
			connection_count BIGINT DEFAULT 0,
			message_count BIGINT DEFAULT 0,
			violation_count BIGINT DEFAULT 0,
			avg_score NUMERIC(5,2),
			max_score INTEGER,

			UNIQUE(hour_bucket, ip_address)
		);

		CREATE INDEX IF NOT EXISTS idx_hourly_bucket ON hourly_spammer_stats(hour_bucket DESC);
		CREATE INDEX IF NOT EXISTS idx_hourly_violations ON hourly_spammer_stats(violation_count DESC);
		CREATE INDEX IF NOT EXISTS idx_hourly_top_spammers ON hourly_spammer_stats(hour_bucket, violation_count DESC);

		-- Connection Events table
		CREATE TABLE IF NOT EXISTS connection_events (
			id BIGSERIAL PRIMARY KEY,
			timestamp TIMESTAMP NOT NULL DEFAULT NOW(),
			ip_address INET NOT NULL,
			event_type VARCHAR(50) NOT NULL,
			score INTEGER,
			action VARCHAR(20),
			details JSONB,
			trace_id UUID
		);

		CREATE INDEX IF NOT EXISTS idx_events_ip ON connection_events(ip_address, timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_events_type ON connection_events(event_type);
		CREATE INDEX IF NOT EXISTS idx_events_score ON connection_events(score DESC);
		CREATE INDEX IF NOT EXISTS idx_events_timestamp ON connection_events(timestamp DESC);
	`

	_, err := r.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	r.logger.Info("Database schema initialized")

	return nil
}

// GetIPCharacteristics retrieves IP characteristics from database
func (r *PostgresRepository) GetIPCharacteristics(ip net.IP) (*scoring.IPCharacteristics, error) {
	query := `
		SELECT
			ip_address, first_seen, last_seen,
			total_connections, quick_disconnects, pre_banner_talks, avg_connection_time_ms,
			messages_sent, recipients_count, failed_auth_attempts,
			current_score, max_score_7d, violation_count,
			reputation_class, is_blocklisted, blocklist_expires,
			last_violation, notes
		FROM ip_characteristics
		WHERE ip_address = $1
	`

	var char scoring.IPCharacteristics
	var avgConnTimeMs int64
	var blocklistExpires sql.NullTime
	var lastViolation sql.NullTime

	err := r.db.QueryRow(query, ip.String()).Scan(
		&char.IP,
		&char.FirstSeen,
		&char.LastSeen,
		&char.TotalConnections,
		&char.QuickDisconnects,
		&char.PreBannerTalks,
		&avgConnTimeMs,
		&char.MessagesSent,
		&char.RecipientsCount,
		&char.FailedAuthAttempts,
		&char.CurrentScore,
		&char.MaxScore7d,
		&char.ViolationCount,
		&char.ReputationClass,
		&char.IsBlocklisted,
		&blocklistExpires,
		&lastViolation,
		&char.Notes,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("IP not found: %s", ip.String())
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query IP characteristics: %w", err)
	}

	char.AverageConnectionTime = time.Duration(avgConnTimeMs) * time.Millisecond

	if blocklistExpires.Valid {
		char.BlocklistExpires = &blocklistExpires.Time
	}

	if lastViolation.Valid {
		char.LastViolation = &lastViolation.Time
	}

	return &char, nil
}

// UpdateIPCharacteristics updates or inserts IP characteristics
func (r *PostgresRepository) UpdateIPCharacteristics(char *scoring.IPCharacteristics) error {
	query := `
		INSERT INTO ip_characteristics (
			ip_address, first_seen, last_seen,
			total_connections, quick_disconnects, pre_banner_talks, avg_connection_time_ms,
			messages_sent, recipients_count, failed_auth_attempts,
			current_score, max_score_7d, violation_count,
			reputation_class, is_blocklisted, blocklist_expires,
			last_violation, notes
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (ip_address) DO UPDATE SET
			last_seen = EXCLUDED.last_seen,
			total_connections = EXCLUDED.total_connections,
			quick_disconnects = EXCLUDED.quick_disconnects,
			pre_banner_talks = EXCLUDED.pre_banner_talks,
			avg_connection_time_ms = EXCLUDED.avg_connection_time_ms,
			messages_sent = EXCLUDED.messages_sent,
			recipients_count = EXCLUDED.recipients_count,
			failed_auth_attempts = EXCLUDED.failed_auth_attempts,
			current_score = EXCLUDED.current_score,
			max_score_7d = EXCLUDED.max_score_7d,
			violation_count = EXCLUDED.violation_count,
			reputation_class = EXCLUDED.reputation_class,
			is_blocklisted = EXCLUDED.is_blocklisted,
			blocklist_expires = EXCLUDED.blocklist_expires,
			last_violation = EXCLUDED.last_violation,
			notes = EXCLUDED.notes
	`

	avgConnTimeMs := int64(char.AverageConnectionTime / time.Millisecond)

	_, err := r.db.Exec(query,
		char.IP.String(),
		char.FirstSeen,
		char.LastSeen,
		char.TotalConnections,
		char.QuickDisconnects,
		char.PreBannerTalks,
		avgConnTimeMs,
		char.MessagesSent,
		char.RecipientsCount,
		char.FailedAuthAttempts,
		char.CurrentScore,
		char.MaxScore7d,
		char.ViolationCount,
		char.ReputationClass,
		char.IsBlocklisted,
		char.BlocklistExpires,
		char.LastViolation,
		char.Notes,
	)

	if err != nil {
		return fmt.Errorf("failed to update IP characteristics: %w", err)
	}

	return nil
}

// GetHourlyStats retrieves hourly statistics for an IP
func (r *PostgresRepository) GetHourlyStats(ip net.IP, hour time.Time) (*scoring.HourlyStats, error) {
	query := `
		SELECT hour_bucket, ip_address, connection_count, message_count, violation_count, avg_score, max_score
		FROM hourly_spammer_stats
		WHERE ip_address = $1 AND hour_bucket = $2
	`

	var stats scoring.HourlyStats

	err := r.db.QueryRow(query, ip.String(), hour).Scan(
		&stats.HourBucket,
		&stats.IP,
		&stats.ConnectionCount,
		&stats.MessageCount,
		&stats.ViolationCount,
		&stats.AvgScore,
		&stats.MaxScore,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("hourly stats not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query hourly stats: %w", err)
	}

	return &stats, nil
}

// UpdateHourlyStats updates or inserts hourly statistics
func (r *PostgresRepository) UpdateHourlyStats(stats *scoring.HourlyStats) error {
	query := `
		INSERT INTO hourly_spammer_stats (
			hour_bucket, ip_address, connection_count, message_count, violation_count, avg_score, max_score
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (hour_bucket, ip_address) DO UPDATE SET
			connection_count = EXCLUDED.connection_count,
			message_count = EXCLUDED.message_count,
			violation_count = EXCLUDED.violation_count,
			avg_score = EXCLUDED.avg_score,
			max_score = EXCLUDED.max_score
	`

	_, err := r.db.Exec(query,
		stats.HourBucket,
		stats.IP.String(),
		stats.ConnectionCount,
		stats.MessageCount,
		stats.ViolationCount,
		stats.AvgScore,
		stats.MaxScore,
	)

	if err != nil {
		return fmt.Errorf("failed to update hourly stats: %w", err)
	}

	return nil
}

// IsInTopSpammers checks if IP is in current hour's top spammers
func (r *PostgresRepository) IsInTopSpammers(ip net.IP) (bool, error) {
	// Get current hour bucket
	hourBucket := time.Now().Truncate(time.Hour)

	query := `
		SELECT COUNT(*) FROM hourly_spammer_stats
		WHERE hour_bucket = $1 AND ip_address = $2 AND violation_count > 5
	`

	var count int
	err := r.db.QueryRow(query, hourBucket, ip.String()).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check top spammers: %w", err)
	}

	return count > 0, nil
}

// RecordConnectionEvent records a connection event
func (r *PostgresRepository) RecordConnectionEvent(ip net.IP, eventType string, score int, action scoring.Action, details map[string]interface{}) error {
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("failed to marshal details: %w", err)
	}

	query := `
		INSERT INTO connection_events (ip_address, event_type, score, action, details)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err = r.db.Exec(query, ip.String(), eventType, score, string(action), detailsJSON)
	if err != nil {
		return fmt.Errorf("failed to record connection event: %w", err)
	}

	return nil
}

// GetTopSpammers returns top N spammers for a given hour
func (r *PostgresRepository) GetTopSpammers(hour time.Time, limit int) ([]*scoring.HourlyStats, error) {
	query := `
		SELECT hour_bucket, ip_address, connection_count, message_count, violation_count, avg_score, max_score
		FROM hourly_spammer_stats
		WHERE hour_bucket = $1
		ORDER BY violation_count DESC, max_score DESC
		LIMIT $2
	`

	rows, err := r.db.Query(query, hour, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query top spammers: %w", err)
	}
	defer rows.Close()

	var stats []*scoring.HourlyStats

	for rows.Next() {
		var s scoring.HourlyStats
		var ipStr string

		err := rows.Scan(
			&s.HourBucket,
			&ipStr,
			&s.ConnectionCount,
			&s.MessageCount,
			&s.ViolationCount,
			&s.AvgScore,
			&s.MaxScore,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		s.IP = net.ParseIP(ipStr)
		stats = append(stats, &s)
	}

	return stats, nil
}

// CleanupOldData removes old records
func (r *PostgresRepository) CleanupOldData(retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// Clean hourly stats
	_, err := r.db.Exec("DELETE FROM hourly_spammer_stats WHERE hour_bucket < $1", cutoff)
	if err != nil {
		return fmt.Errorf("failed to cleanup hourly stats: %w", err)
	}

	// Clean connection events
	_, err = r.db.Exec("DELETE FROM connection_events WHERE timestamp < $1", cutoff)
	if err != nil {
		return fmt.Errorf("failed to cleanup connection events: %w", err)
	}

	r.logger.Info("Cleaned up old data", zap.Time("cutoff", cutoff))

	return nil
}

// Close closes the database connection
func (r *PostgresRepository) Close() error {
	return r.db.Close()
}
