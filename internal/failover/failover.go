// Package failover coordinates explicitly fenced active/passive activation.
package failover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Acquire holds an advisory lock for the process lifetime. Every possible owner
// must use the same lock on storage providing reliable cross-host POSIX locks.
// This supplements, and never substitutes for, provider-level fencing.
func Acquire(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("ownership lease unavailable: %w", err)
	}
	return f, nil
}

type Plan struct {
	LeaseFile    string   `json:"lease_file"`
	AuditFile    string   `json:"audit_file"`
	Fence        []string `json:"fence"`
	VerifyFenced []string `json:"verify_fenced"`
	Start        []string `json:"start"`
	Ready        []string `json:"ready"`
	Stop         []string `json:"stop"`
}

func (p Plan) Validate() error {
	if p.LeaseFile == "" || p.AuditFile == "" {
		return fmt.Errorf("shared lease and audit file required")
	}
	for _, argv := range [][]string{p.Fence, p.VerifyFenced, p.Start, p.Ready, p.Stop} {
		if len(argv) == 0 || !filepath.IsAbs(argv[0]) {
			return fmt.Errorf("absolute fencing, verification, start, ready and stop programs required")
		}
	}
	return nil
}
func Activate(ctx context.Context, p Plan) error {
	return activate(ctx, p, func(ctx context.Context, argv []string) error {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		return cmd.Run()
	})
}
func activate(ctx context.Context, p Plan, run func(context.Context, []string) error) error {
	if err := p.Validate(); err != nil {
		return err
	}
	activation, err := Acquire(p.LeaseFile + ".activation")
	if err != nil {
		return err
	}
	defer activation.Close()
	audit := func(step string) error {
		if err := os.MkdirAll(filepath.Dir(p.AuditFile), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(p.AuditFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		if err = json.NewEncoder(f).Encode(map[string]interface{}{"time": time.Now().UTC(), "step": step}); err != nil {
			return err
		}
		return f.Sync()
	}
	if err = audit("fence_requested"); err != nil {
		return err
	}
	if err = run(ctx, p.Fence); err != nil {
		return fmt.Errorf("fencing failed: %w", err)
	}
	if err = run(ctx, p.VerifyFenced); err != nil {
		return fmt.Errorf("fence verification failed: %w", err)
	}
	if err = audit("fence_verified"); err != nil {
		return err
	}
	probe, err := Acquire(p.LeaseFile)
	if err != nil {
		return err
	}
	probe.Close()
	if err = audit("activation_requested"); err != nil {
		return err
	}
	if err = run(ctx, p.Start); err == nil {
		err = run(ctx, p.Ready)
	}
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stopErr := run(stopCtx, p.Stop)
		audit("activation_failed")
		return errors.Join(err, stopErr)
	}
	return audit("activation_ready")
}
