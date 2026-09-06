package delivery

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type ThrottleConfig struct {
	Concurrency     int    `yaml:"concurrency"`
	InitialBackoff  string `yaml:"initial_backoff"`
	MaxBackoff      string `yaml:"max_backoff"`
	MaxDestinations int    `yaml:"max_destinations"`
}

func (c *ThrottleConfig) Validate() error {
	if c.Concurrency == 0 {
		c.Concurrency = 4
	}
	if c.MaxDestinations == 0 {
		c.MaxDestinations = 10000
	}
	if c.InitialBackoff == "" {
		c.InitialBackoff = "30s"
	}
	if c.MaxBackoff == "" {
		c.MaxBackoff = "1h"
	}
	initial, e := time.ParseDuration(c.InitialBackoff)
	max, e2 := time.ParseDuration(c.MaxBackoff)
	if e != nil || e2 != nil || initial < time.Second || max < initial || max > 24*time.Hour || c.Concurrency < 1 || c.Concurrency > 1000 || c.MaxDestinations < 1 {
		return fmt.Errorf("invalid destination throttle configuration")
	}
	return nil
}

type DestinationStatus struct {
	Domain   string    `json:"domain"`
	Active   int       `json:"active"`
	Limit    int       `json:"limit"`
	Failures int       `json:"failures"`
	Until    time.Time `json:"until"`
	last     time.Time
}
type DestinationThrottle struct {
	mu           sync.Mutex
	config       ThrottleConfig
	initial, max time.Duration
	states       map[string]*DestinationStatus
	now          func() time.Time
}

func NewDestinationThrottle(c ThrottleConfig) (*DestinationThrottle, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	initial, _ := time.ParseDuration(c.InitialBackoff)
	max, _ := time.ParseDuration(c.MaxBackoff)
	return &DestinationThrottle{config: c, initial: initial, max: max, states: map[string]*DestinationStatus{}, now: time.Now}, nil
}
func (t *DestinationThrottle) Acquire(domain string) (func(), bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	domain = strings.ToLower(domain)
	now := t.now()
	s := t.states[domain]
	if s == nil {
		if len(t.states) >= t.config.MaxDestinations {
			for k, v := range t.states {
				if v.Active == 0 && now.Sub(v.last) > 2*t.max {
					delete(t.states, k)
				}
			}
		}
		if len(t.states) >= t.config.MaxDestinations {
			return nil, false
		}
		s = &DestinationStatus{Domain: domain, Limit: t.config.Concurrency}
		t.states[domain] = s
	}
	s.last = now
	if s.Active >= s.Limit || now.Before(s.Until) {
		return nil, false
	}
	s.Active++
	var once sync.Once
	return func() { once.Do(func() { t.mu.Lock(); s.Active--; s.last = t.now(); t.mu.Unlock() }) }, true
}

// Observe uses multiplicative decrease on transient failures and additive
// recovery on successes. It does not delay workers inside the transport.
func (t *DestinationThrottle) Observe(domain string, success, temporary bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.states[strings.ToLower(domain)]
	if s == nil {
		return
	}
	s.last = t.now()
	if temporary {
		s.Failures++
		s.Limit /= 2
		if s.Limit < 1 {
			s.Limit = 1
		}
		delay := t.initial
		for i := 1; i < s.Failures && delay < t.max; i++ {
			delay *= 2
		}
		if delay > t.max {
			delay = t.max
		}
		s.Until = t.now().Add(delay)
	} else if success {
		// In-flight successes cannot cancel a cooldown opened by a sibling failure.
		if !t.now().Before(s.Until) {
			s.Failures = 0
			if s.Limit < t.config.Concurrency {
				s.Limit++
			}
		}
	}
}
func (t *DestinationThrottle) Status() []DestinationStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]DestinationStatus, 0, len(t.states))
	for _, s := range t.states {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out
}
