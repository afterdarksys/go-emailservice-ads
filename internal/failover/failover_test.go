package failover

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestFencingFailureNeverStartsStandby(t *testing.T) {
	for _, fail := range []string{"fence", "verify", "ready", ""} {
		t.Run(fail, func(t *testing.T) {
			dir := t.TempDir()
			p := Plan{LeaseFile: filepath.Join(dir, "lease"), AuditFile: filepath.Join(dir, "audit"), Fence: []string{"/fence"}, VerifyFenced: []string{"/verify"}, Start: []string{"/start"}, Ready: []string{"/ready"}, Stop: []string{"/stop"}}
			var steps []string
			err := activate(context.Background(), p, func(_ context.Context, args []string) error {
				step := strings.TrimPrefix(args[0], "/")
				steps = append(steps, step)
				if step == fail {
					return fmt.Errorf("injected failure")
				}
				return nil
			})
			if (err == nil) != (fail == "") {
				t.Fatal(err)
			}
			all := strings.Join(steps, ",")
			if (fail == "fence" || fail == "verify") && strings.Contains(all, "start") {
				t.Fatal("started unfenced", all)
			}
			if fail == "ready" && !strings.HasSuffix(all, "stop") {
				t.Fatal("failed activation not stopped")
			}
		})
	}
}
func TestSingleLeaseOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := Acquire(path); err == nil {
		second.Close()
		t.Fatal("two owners")
	}
}
