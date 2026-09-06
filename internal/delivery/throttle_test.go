package delivery

import (
	"testing"
	"time"
)

func TestDestinationIsolationAndAdaptiveRecovery(t *testing.T) {
	g, err := NewDestinationThrottle(ThrottleConfig{Concurrency: 2, InitialBackoff: "1s"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	g.now = func() time.Time { return now }
	release, ok := g.Acquire("slow.test")
	if !ok {
		t.Fatal("first slot denied")
	}
	second, ok := g.Acquire("slow.test")
	if !ok {
		t.Fatal("second denied")
	}
	if _, ok = g.Acquire("slow.test"); ok {
		t.Fatal("concurrency exceeded")
	}
	other, ok := g.Acquire("other.test")
	if !ok {
		t.Fatal("unrelated domain blocked")
	}
	other()
	g.Observe("slow.test", false, true)
	release()
	second()
	if _, ok = g.Acquire("slow.test"); ok {
		t.Fatal("backoff ignored")
	}
	now = now.Add(2 * time.Second)
	release, ok = g.Acquire("slow.test")
	if !ok {
		t.Fatal("recovery probe denied")
	}
	g.Observe("slow.test", true, false)
	release()
	release()
	for _, s := range g.Status() {
		if s.Domain == "slow.test" && (s.Active != 0 || s.Limit != 2 || s.Failures != 0) {
			t.Fatalf("bad recovery: %+v", s)
		}
	}
}
