package extensions

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Webhook struct {
	Name      string `yaml:"name"`
	URL       string `yaml:"url"`
	SecretEnv string `yaml:"secret_env"`
}

func (h Webhook) Validate() error {
	u, e := url.Parse(h.URL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || h.Name == "" || h.SecretEnv == "" {
		return fmt.Errorf("webhook requires name, HTTPS URL and secret_env")
	}
	if len(os.Getenv(h.SecretEnv)) < 32 {
		return fmt.Errorf("webhook %s requires a secret of at least 32 bytes", h.Name)
	}
	return nil
}

type Event struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	Principal string    `json:"principal"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Status    int       `json:"status"`
	Outcome   string    `json:"outcome"`
}
type Outbox struct {
	db     *sql.DB
	hooks  []Webhook
	client *http.Client
}

func OpenOutbox(path string, hooks []Webhook) (*Outbox, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	db, e := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`CREATE TABLE IF NOT EXISTS events(id TEXT PRIMARY KEY, payload TEXT NOT NULL, ready INTEGER NOT NULL, created INTEGER NOT NULL); CREATE TABLE IF NOT EXISTS deliveries(event TEXT NOT NULL, destination TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, due INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL DEFAULT 'pending', PRIMARY KEY(event,destination)); UPDATE events SET ready=1 WHERE ready=0`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Outbox{db: db, hooks: hooks, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (o *Outbox) Close() error { return o.db.Close() }
func (o *Outbox) Begin(ctx context.Context, principal, method, path string) (string, error) {
	tx, e := o.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	// Retain acknowledgements for seven days; never evict undelivered events.
	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	if _, e = tx.ExecContext(ctx, `DELETE FROM events WHERE created<? AND NOT EXISTS(SELECT 1 FROM deliveries WHERE event=events.id AND state!='sent')`, cutoff); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM deliveries WHERE event NOT IN (SELECT id FROM events)`); e != nil {
		return "", e
	}
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n); e != nil {
		return "", e
	}
	if n >= 10000 {
		return "", fmt.Errorf("webhook outbox full")
	}
	event := Event{ID: uuid.NewString(), Time: time.Now().UTC(), Principal: principal, Method: method, Path: path, Outcome: "interrupted_or_unknown"}
	b, _ := json.Marshal(event)
	if _, e = tx.ExecContext(ctx, `INSERT INTO events VALUES(?,?,0,?)`, event.ID, string(b), event.Time.Unix()); e != nil {
		return "", e
	}
	for _, h := range o.hooks {
		if _, e = tx.ExecContext(ctx, `INSERT INTO deliveries(event,destination) VALUES(?,?)`, event.ID, h.Name); e != nil {
			return "", e
		}
	}
	return event.ID, tx.Commit()
}
func (o *Outbox) Finish(ctx context.Context, id string, status int) error {
	var raw string
	if e := o.db.QueryRowContext(ctx, `SELECT payload FROM events WHERE id=?`, id).Scan(&raw); e != nil {
		return e
	}
	var event Event
	if e := json.Unmarshal([]byte(raw), &event); e != nil {
		return e
	}
	event.Status = status
	event.Outcome = "completed"
	b, _ := json.Marshal(event)
	_, e := o.db.ExecContext(ctx, `UPDATE events SET payload=?,ready=1 WHERE id=?`, string(b), id)
	return e
}
func (o *Outbox) Stats(ctx context.Context) (map[string]int, error) {
	rows, e := o.db.QueryContext(ctx, `SELECT state,COUNT(*) FROM deliveries GROUP BY state`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var state string
		var count int
		if e = rows.Scan(&state, &count); e != nil {
			return nil, e
		}
		out[state] = count
	}
	return out, rows.Err()
}
func (o *Outbox) Retry(ctx context.Context) error {
	_, e := o.db.ExecContext(ctx, `UPDATE deliveries SET attempts=0,due=0,state='pending' WHERE state='dead'`)
	return e
}

// Dispatch runs in one worker. Stable event IDs support consumer deduplication;
// acknowledgement loss may redeliver an event.
func (o *Outbox) Dispatch(ctx context.Context) error {
	rows, e := o.db.QueryContext(ctx, `SELECT d.event,d.destination,d.attempts,e.payload FROM deliveries d JOIN events e ON e.id=d.event WHERE e.ready=1 AND d.state='pending' AND d.due<=? ORDER BY e.created LIMIT 32`, time.Now().Unix())
	if e != nil {
		return e
	}
	type item struct {
		id, dest, raw string
		attempt       int
	}
	var items []item
	for rows.Next() {
		var v item
		if e = rows.Scan(&v.id, &v.dest, &v.attempt, &v.raw); e != nil {
			rows.Close()
			return e
		}
		items = append(items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range items {
		if e = ctx.Err(); e != nil {
			return e
		}
		sent := false
		for _, h := range o.hooks {
			if h.Name != v.dest {
				continue
			}
			secret := os.Getenv(h.SecretEnv)
			if len(secret) < 32 {
				break
			}
			timestamp := fmt.Sprint(time.Now().Unix())
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(timestamp + "." + v.raw))
			req, err := http.NewRequestWithContext(ctx, "POST", h.URL, bytes.NewBufferString(v.raw))
			if err != nil {
				break
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Mailhub-Event-ID", v.id)
			req.Header.Set("X-Mailhub-Timestamp", timestamp)
			req.Header.Set("X-Mailhub-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
			res, err := o.client.Do(req)
			if err == nil {
				io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
				res.Body.Close()
				sent = res.StatusCode >= 200 && res.StatusCode < 300
			}
			break
		}
		state := "pending"
		attempt := v.attempt + 1
		if sent {
			state = "sent"
		} else if attempt >= 10 {
			state = "dead"
		}
		delay := time.Second * time.Duration(1<<min(attempt, 12))
		if _, e = o.db.ExecContext(ctx, `UPDATE deliveries SET attempts=?,due=?,state=? WHERE event=? AND destination=?`, attempt, time.Now().Add(delay).Unix(), state, v.id, v.dest); e != nil {
			return e
		}
	}
	return nil
}
func ValidateWebhooks(hooks []Webhook) error {
	if len(hooks) > 8 {
		return fmt.Errorf("at most eight webhooks")
	}
	seen := map[string]bool{}
	for _, h := range hooks {
		if e := h.Validate(); e != nil {
			return e
		}
		if seen[h.Name] || strings.ContainsAny(h.Name, "\r\n") {
			return fmt.Errorf("duplicate or invalid webhook name")
		}
		seen[h.Name] = true
	}
	return nil
}
