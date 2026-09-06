// Package mailstorm provides bounded, adaptive SMTP admission control.
package mailstorm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type Config struct {
	Enabled         bool    `yaml:"enabled"`
	Window          string  `yaml:"window"`
	Messages        int     `yaml:"messages_per_window"`
	Recipients      int     `yaml:"recipients_per_window"`
	GlobalMessages  int     `yaml:"global_messages_per_window"`
	DuplicateLimit  int     `yaml:"duplicate_limit"`
	TripAfter       int     `yaml:"trip_after"`
	Cooldown        string  `yaml:"cooldown"`
	MaxCooldown     string  `yaml:"max_cooldown"`
	BaselineWindows int     `yaml:"baseline_windows"`
	AnomalyFactor   float64 `yaml:"anomaly_factor"`
	AnomalyFloor    int     `yaml:"anomaly_floor"`
	MaxIdentities   int     `yaml:"max_identities"`
}

func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Window == "" {
		c.Window = "1m"
	}
	if c.Cooldown == "" {
		c.Cooldown = "5m"
	}
	if c.MaxCooldown == "" {
		c.MaxCooldown = "1h"
	}
	if c.Messages == 0 {
		c.Messages = 120
	}
	if c.Recipients == 0 {
		c.Recipients = 1000
	}
	if c.GlobalMessages == 0 {
		c.GlobalMessages = 2000
	}
	if c.DuplicateLimit == 0 {
		c.DuplicateLimit = 20
	}
	if c.TripAfter == 0 {
		c.TripAfter = 3
	}
	if c.BaselineWindows == 0 {
		c.BaselineWindows = 5
	}
	if c.AnomalyFactor == 0 {
		c.AnomalyFactor = 4
	}
	if c.AnomalyFloor == 0 {
		c.AnomalyFloor = 20
	}
	if c.MaxIdentities == 0 {
		c.MaxIdentities = 10000
	}
	window, e := time.ParseDuration(c.Window)
	if e != nil || window < time.Second || window > time.Hour {
		return fmt.Errorf("mailstorm window must be 1s to 1h")
	}
	cool, e := time.ParseDuration(c.Cooldown)
	if e != nil || cool < time.Second {
		return fmt.Errorf("invalid mailstorm cooldown")
	}
	max, e := time.ParseDuration(c.MaxCooldown)
	if e != nil || max < cool || max > 24*time.Hour {
		return fmt.Errorf("mailstorm max cooldown must be between cooldown and 24h")
	}
	if c.Messages < 1 || c.Recipients < 1 || c.GlobalMessages < 1 || c.DuplicateLimit < 1 || c.TripAfter < 1 || c.BaselineWindows < 1 || c.AnomalyFactor < 1 || c.AnomalyFloor < 1 || c.MaxIdentities < 1 {
		return fmt.Errorf("invalid mailstorm limits")
	}
	return nil
}

type Circuit struct {
	Key    string    `json:"key"`
	Reason string    `json:"reason"`
	Until  time.Time `json:"until"`
	Trips  int       `json:"trips"`
	Actor  string    `json:"actor"`
}
type bucket struct {
	messages, recipients       *rate.Limiter
	start, last                time.Time
	count, windows, violations int
	baseline                   float64
	fingerprints               map[[32]byte]int
}
type Guard struct {
	mu                            sync.Mutex
	config                        Config
	window, cooldown, maxCooldown time.Duration
	buckets                       map[string]*bucket
	circuits                      map[string]Circuit
	global                        *rate.Limiter
	path                          string
	now                           func() time.Time
	accepted, deferred            uint64
}
type Status struct {
	Enabled           bool      `json:"enabled"`
	Accepted          uint64    `json:"accepted"`
	Deferred          uint64    `json:"deferred"`
	TrackedIdentities int       `json:"tracked_identities"`
	Circuits          []Circuit `json:"circuits"`
}

func New(c Config, path string) (*Guard, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	g := &Guard{config: c, path: path, now: time.Now, buckets: map[string]*bucket{}, circuits: map[string]Circuit{}}
	if !c.Enabled {
		return g, nil
	}
	g.window, _ = time.ParseDuration(c.Window)
	g.cooldown, _ = time.ParseDuration(c.Cooldown)
	g.maxCooldown, _ = time.ParseDuration(c.MaxCooldown)
	g.global = rate.NewLimiter(rate.Limit(float64(c.GlobalMessages)/g.window.Seconds()), c.GlobalMessages)
	if raw, err := os.ReadFile(path); err == nil {
		if err = json.Unmarshal(raw, &g.circuits); err != nil {
			return nil, fmt.Errorf("read mailstorm state: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if g.circuits == nil {
		g.circuits = map[string]Circuit{}
	}
	return g, nil
}

// Fingerprint ignores transport headers that change on each trip around a loop.
// The caller supplies subject and body; only the hash is retained in memory.
func Fingerprint(from, subject string, to []string, body []byte) [32]byte {
	recipients := append([]string(nil), to...)
	sort.Strings(recipients)
	h := sha256.New()
	json.NewEncoder(h).Encode([]any{from, subject, recipients})
	h.Write(body)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}
func (g *Guard) Status() Status {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := Status{Enabled: g.config.Enabled, Accepted: g.accepted, Deferred: g.deferred, TrackedIdentities: len(g.buckets), Circuits: []Circuit{}}
	for _, c := range g.circuits {
		if c.Until.After(g.now()) {
			s.Circuits = append(s.Circuits, c)
		}
	}
	sort.Slice(s.Circuits, func(i, j int) bool { return s.Circuits[i].Key < s.Circuits[j].Key })
	return s
}
func (g *Guard) save() error {
	if g.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(g.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(g.path), ".mailstorm-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(g.circuits)
	if err == nil {
		err = f.Sync()
	}
	f.Close()
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), g.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(g.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (g *Guard) Pause(key, reason, actor string, duration time.Duration) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.config.Enabled {
		return fmt.Errorf("mailstorm protection disabled")
	}
	if key == "" || len(key) > 512 || reason == "" || len(reason) > 512 || duration < time.Second || duration > 24*time.Hour {
		return fmt.Errorf("key, reason and duration between 1s and 24h required")
	}
	for k, c := range g.circuits {
		if !c.Until.After(g.now()) {
			delete(g.circuits, k)
		}
	}
	if _, ok := g.circuits[key]; key != "*" && !ok && len(g.circuits) >= g.config.MaxIdentities {
		return fmt.Errorf("circuit capacity reached")
	}
	until := g.now().Add(duration)
	if old := g.circuits[key]; old.Until.After(until) {
		until = old.Until
	}
	g.circuits[key] = Circuit{Key: key, Reason: reason, Actor: actor, Until: until, Trips: g.circuits[key].Trips + 1}
	return g.save()
}
func (g *Guard) Resume(key string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	old, exists := g.circuits[key]
	delete(g.circuits, key)
	if err := g.save(); err != nil {
		if exists {
			g.circuits[key] = old
		}
		return err
	}
	delete(g.buckets, key)
	return nil
}

// Admit uses a token bucket for bursts and a learned EWMA for anomalous volume.
// A tripped identity remains blocked across process restarts. Expiry permits a
// probe automatically; a recurrent storm doubles its cooldown up to the cap.
func (g *Guard) Admit(ctx context.Context, key string, recipients int, fingerprint [32]byte) error {
	if !g.config.Enabled {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	reject := func(reason string) error { g.deferred++; return fmt.Errorf("mailstorm protection: %s", reason) }
	for _, k := range []string{"*", key} {
		if c := g.circuits[k]; c.Until.After(now) {
			return reject("sender temporarily paused")
		}
	}
	b := g.buckets[key]
	if b == nil {
		if len(g.buckets) >= g.config.MaxIdentities {
			for k, b := range g.buckets {
				if now.Sub(b.last) > 2*g.maxCooldown {
					delete(g.buckets, k)
				}
			}
		}
		if len(g.buckets) >= g.config.MaxIdentities {
			return reject("identity capacity reached")
		}
		b = &bucket{messages: rate.NewLimiter(rate.Limit(float64(g.config.Messages)/g.window.Seconds()), g.config.Messages), recipients: rate.NewLimiter(rate.Limit(float64(g.config.Recipients)/g.window.Seconds()), g.config.Recipients), start: now, fingerprints: map[[32]byte]int{}}
		g.buckets[key] = b
	}
	b.last = now
	if now.Sub(b.start) >= g.window {
		if b.violations == 0 {
			if b.windows == 0 {
				b.baseline = float64(b.count)
			} else {
				b.baseline = 0.8*b.baseline + 0.2*float64(b.count)
			}
			b.windows++
		}
		b.count = 0
		b.violations = 0
		b.start = now
		b.fingerprints = map[[32]byte]int{}
	}
	reason := ""
	if recipients < 1 || b.messages.TokensAt(now) < 1 || b.recipients.TokensAt(now) < float64(recipients) {
		reason = "sender burst limit"
	}
	if b.windows >= g.config.BaselineWindows && b.count >= g.config.AnomalyFloor && float64(b.count+1) > b.baseline*g.config.AnomalyFactor {
		reason = "abnormal increase over sender baseline"
	}
	if b.fingerprints[fingerprint] >= g.config.DuplicateLimit {
		reason = "repeated message storm"
		b.violations = g.config.TripAfter
	}
	if reason != "" {
		b.violations++
		if b.violations >= g.config.TripAfter {
			old := g.circuits[key]
			cool := g.cooldown
			for i := 0; i < old.Trips && cool < g.maxCooldown; i++ {
				cool *= 2
			}
			if cool > g.maxCooldown {
				cool = g.maxCooldown
			}
			for k, c := range g.circuits {
				if now.Sub(c.Until) > 2*g.maxCooldown {
					delete(g.circuits, k)
				}
			}
			if len(g.circuits) >= g.config.MaxIdentities && old.Key == "" {
				return reject("circuit capacity reached")
			}
			g.circuits[key] = Circuit{Key: key, Reason: reason, Actor: "automatic", Until: now.Add(cool), Trips: old.Trips + 1}
			if err := g.save(); err != nil {
				return reject("circuit persistence unavailable")
			}
		}
		return reject(reason)
	}
	if !g.global.AllowN(now, 1) {
		return reject("global admission limit")
	}
	b.messages.AllowN(now, 1)
	b.recipients.AllowN(now, recipients)
	b.count++
	// Fingerprints cannot exceed the admitted burst plus refill within a window.
	if len(b.fingerprints) < 2*g.config.Messages || b.fingerprints[fingerprint] > 0 {
		b.fingerprints[fingerprint]++
	}
	g.accepted++
	return nil
}

// Identity uses authenticated users where possible; peer IPs cover applications
// and inbound senders. Envelope addresses alone cannot bypass the control.
func Identity(user, ip string) string {
	if user != "" {
		return "user:" + user
	}
	return "ip:" + ip
}
func FingerprintID(f [32]byte) string { return hex.EncodeToString(f[:]) }

// Blocked pauses queued delivery without consuming a delivery attempt.
func (g *Guard) Blocked(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	return g.circuits["*"].Until.After(now) || g.circuits[key].Until.After(now)
}
