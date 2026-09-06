package filtering

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/lib/pq"
	"net"
	"time"
)

// PremailReputation reads the existing perimeter scoring database without
// altering its schema. Premail's high-is-bad score becomes high-is-good here.
type PremailReputation struct{ db *sql.DB }

func NewPremailReputation(ctx context.Context, dsn string) (*PremailReputation, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &PremailReputation{db: db}, nil
}
func (p *PremailReputation) Close() error                     { return p.db.Close() }
func (p *PremailReputation) Health(ctx context.Context) error { return p.db.PingContext(ctx) }
func (p *PremailReputation) Lookup(ctx context.Context, ip string) (Reputation, error) {
	r := Reputation{Source: "premail", Score: 50, ExpiresAt: time.Now().Add(5 * time.Minute)}
	if net.ParseIP(ip) == nil {
		return r, fmt.Errorf("invalid reputation IP")
	}
	var score int
	var seen time.Time
	var blocked bool
	var expires sql.NullTime
	err := p.db.QueryRowContext(ctx, `SELECT current_score,last_seen,is_blocklisted,blocklist_expires FROM ip_characteristics WHERE ip_address=$1`, ip).Scan(&score, &seen, &blocked, &expires)
	if err == sql.ErrNoRows {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if score < 0 || score > 100 {
		return r, fmt.Errorf("invalid stored reputation score")
	}
	if time.Since(seen) > 7*24*time.Hour {
		return r, nil
	}
	r.Known = true
	r.Score = 100 - score
	if blocked && (!expires.Valid || expires.Time.After(time.Now())) {
		r.Score = 0
	}
	return r, nil
}
