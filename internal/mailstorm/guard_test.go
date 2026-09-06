package mailstorm

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func testGuard(t *testing.T, c Config) (*Guard, *time.Time) {
	t.Helper()
	c.Enabled = true
	g, e := New(c, filepath.Join(t.TempDir(), "circuits.json"))
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	g.now = func() time.Time { return now }
	return g, &now
}
func TestRepeatedStormPersistsAndEscalates(t *testing.T) {
	g, now := testGuard(t, Config{DuplicateLimit: 2, Cooldown: "1m", MaxCooldown: "4m"})
	f := Fingerprint("a", "s", []string{"b"}, []byte("loop"))
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if e := g.Admit(ctx, "user:loop", 1, f); e != nil {
			t.Fatal(e)
		}
	}
	if g.Admit(ctx, "user:loop", 1, f) == nil {
		t.Fatal("storm accepted")
	}
	restored, e := New(g.config, g.path)
	if e != nil || !restored.Blocked("user:loop") {
		t.Fatalf("circuit lost on restart: %v", e)
	}
	if e = g.Admit(ctx, "user:other", 1, f); e != nil {
		t.Fatal("unrelated sender blocked", e)
	}
	*now = now.Add(61 * time.Second)
	for i := 0; i < 2; i++ {
		if e := g.Admit(ctx, "user:loop", 1, f); e != nil {
			t.Fatal(e)
		}
	}
	if g.Admit(ctx, "user:loop", 1, f) == nil {
		t.Fatal("repeat storm accepted")
	}
	circuit := g.Status().Circuits[0]
	if circuit.Trips != 2 || circuit.Until.Sub(*now) != 2*time.Minute {
		t.Fatalf("cooldown not escalated: %+v", circuit)
	}
	if e = g.Resume("user:loop"); e != nil || g.Blocked("user:loop") {
		t.Fatal("resume failed", e)
	}
}
func TestAdaptiveBaselineTripsOnSpike(t *testing.T) {
	g, now := testGuard(t, Config{BaselineWindows: 2, AnomalyFactor: 2, AnomalyFloor: 3, TripAfter: 1, DuplicateLimit: 100})
	ctx := context.Background()
	f := [32]byte{}
	for window := 0; window < 2; window++ {
		for i := 0; i < 2; i++ {
			if e := g.Admit(ctx, "ip:app", 1, f); e != nil {
				t.Fatal(e)
			}
		}
		*now = now.Add(time.Minute)
	}
	for i := 0; i < 4; i++ {
		if e := g.Admit(ctx, "ip:app", 1, f); e != nil {
			t.Fatal(e)
		}
	}
	if g.Admit(ctx, "ip:app", 1, f) == nil || !g.Blocked("ip:app") {
		t.Fatal("baseline spike did not trip")
	}
}
func TestGlobalPauseAndMemoryBound(t *testing.T) {
	g, _ := testGuard(t, Config{MaxIdentities: 1})
	ctx := context.Background()
	if e := g.Admit(ctx, "one", 1, [32]byte{}); e != nil {
		t.Fatal(e)
	}
	if g.Admit(ctx, "two", 1, [32]byte{}) == nil {
		t.Fatal("identity memory unbounded")
	}
	if e := g.Pause("*", "incident", "operator", time.Hour); e != nil {
		t.Fatal(e)
	}
	if !g.Blocked("one") || g.Admit(ctx, "one", 1, [32]byte{}) == nil {
		t.Fatal("global stop not enforced")
	}
	if e := g.Resume("*"); e != nil || g.Blocked("one") {
		t.Fatal("global resume failed")
	}
}
func TestFingerprintIgnoresRecipientOrder(t *testing.T) {
	if Fingerprint("a", "s", []string{"x", "y"}, []byte("body")) != Fingerprint("a", "s", []string{"y", "x"}, []byte("body")) {
		t.Fatal("unstable fingerprint")
	}
}

func TestGlobalStopSurvivesFullCircuitCapacity(t *testing.T) {
	g, _ := testGuard(t, Config{MaxIdentities: 1})
	if e := g.Pause("user:one", "incident", "operator", time.Hour); e != nil {
		t.Fatal(e)
	}
	if e := g.Pause("*", "emergency", "operator", time.Hour); e != nil || !g.Blocked("user:two") {
		t.Fatal("emergency stop unavailable", e)
	}
}
func TestPauseCannotShortenExistingCircuit(t *testing.T) {
	g, now := testGuard(t, Config{})
	if e := g.Pause("user:one", "incident", "operator", time.Hour); e != nil {
		t.Fatal(e)
	}
	if e := g.Pause("user:one", "still investigating", "operator", time.Second); e != nil {
		t.Fatal(e)
	}
	*now = now.Add(time.Minute)
	if !g.Blocked("user:one") {
		t.Fatal("pause unexpectedly shortened")
	}
}
