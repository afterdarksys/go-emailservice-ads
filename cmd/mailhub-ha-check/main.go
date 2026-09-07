package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/ha"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	resource := flag.String("resource", "mailhub", "DRBD resource")
	volume := flag.String("volume", "/srv/mailhub", "replicated mount")
	device := flag.String("device", "/dev/drbd100", "DRBD device")
	flag.Parse()
	if !filepath.IsAbs(*volume) || !filepath.IsAbs(*device) {
		fmt.Fprintln(os.Stderr, "absolute volume and device required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	run := func(name string, args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, name, args...)
		c.WaitDelay = 100 * time.Millisecond
		var b limited
		c.Stdout = &b
		err := c.Run()
		return b.Bytes(), err
	}
	state, e := run("/usr/sbin/drbdsetup", "status", *resource, "--json")
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	mount, e := run("/usr/bin/findmnt", "--json", "--output", "TARGET,SOURCE,OPTIONS", "--target", *volume)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	proof, e := ha.DRBDProof(*resource, *volume, *device, state, mount)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(proof)
}

type limited struct{ bytes.Buffer }

func (b *limited) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		return 0, fmt.Errorf("status too large")
	}
	return b.Buffer.Write(p)
}
