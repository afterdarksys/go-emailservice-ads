// Package ha verifies externally fenced, replicated-volume ownership.
package ha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type Config struct {
	Enabled      bool     `yaml:"enabled"`
	Volume       string   `yaml:"volume"`
	CheckCommand []string `yaml:"check_command"`
}
type Proof struct {
	Volume       string `json:"volume"`
	Primary      bool   `json:"primary"`
	UpToDate     bool   `json:"up_to_date"`
	Quorum       bool   `json:"quorum"`
	Mounted      bool   `json:"mounted"`
	PeerUpToDate bool   `json:"peer_up_to_date"`
}
type Status struct {
	Enabled bool      `json:"enabled"`
	Safe    bool      `json:"safe"`
	Checked time.Time `json:"checked"`
	Proof   Proof     `json:"proof"`
	Error   string    `json:"error,omitempty"`
}
type Guard struct {
	cfg    Config
	mu     sync.RWMutex
	status Status
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if !filepath.IsAbs(c.Volume) || filepath.Clean(c.Volume) == "/" || len(c.CheckCommand) == 0 || !filepath.IsAbs(c.CheckCommand[0]) {
		return fmt.Errorf("HA requires an absolute dedicated volume and ownership-check executable")
	}
	return nil
}
func Inside(root, path string) bool {
	r, e := filepath.Abs(root)
	if e != nil {
		return false
	}
	p, e := filepath.Abs(path)
	if e != nil {
		return false
	}
	rel, e := filepath.Rel(r, p)
	return e == nil && rel != ".." && !filepath.IsAbs(rel) && !(len(rel) > 3 && rel[:3] == "../")
}
func New(c Config) (*Guard, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	return &Guard{cfg: c, status: Status{Enabled: c.Enabled}}, nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		return 0, fmt.Errorf("HA proof too large")
	}
	return b.Buffer.Write(p)
}
func (g *Guard) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.cfg.CheckCommand[0], g.cfg.CheckCommand[1:]...)
	cmd.WaitDelay = time.Second
	var out boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	e := cmd.Run()
	var p Proof
	if e == nil {
		dec := json.NewDecoder(&out)
		dec.DisallowUnknownFields()
		e = dec.Decode(&p)
		if e == nil {
			var extra interface{}
			if dec.Decode(&extra) != io.EOF {
				e = fmt.Errorf("extra HA proof")
			}
		}
	}
	if e == nil && (filepath.Clean(p.Volume) != filepath.Clean(g.cfg.Volume) || !p.Primary || !p.UpToDate || !p.Quorum || !p.Mounted) {
		e = fmt.Errorf("replicated volume is not safe for ownership")
	}
	state := Status{Enabled: true, Safe: e == nil, Checked: time.Now().UTC(), Proof: p}
	if e != nil {
		state.Error = "Ownership verification failed"
	}
	g.mu.Lock()
	g.status = state
	g.mu.Unlock()
	return e
}
func (g *Guard) Status() Status {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s := g.status
	if time.Since(s.Checked) > 5*time.Second {
		s.Safe = false
	}
	return s
}

// Monitor signals loss promptly; callers must terminate all writers and senders.
// Storage quorum and provider fencing remain mandatory during detection latency.
func (g *Guard) Monitor(ctx context.Context) <-chan error {
	out := make(chan error, 1)
	go func() {
		defer close(out)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e := g.Check(ctx); e != nil {
					if ctx.Err() == nil {
						out <- e
					}
					return
				}
			}
		}
	}()
	return out
}
