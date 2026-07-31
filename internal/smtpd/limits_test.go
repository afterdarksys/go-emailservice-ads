package smtpd

import (
	"testing"
	"time"
)

func TestConnectionLimiter(t *testing.T) {
	l := newConnectionLimiter(2, 1)
	if !l.acquire("a") || l.acquire("a") || !l.acquire("b") || l.acquire("c") {
		t.Fatal("connection limits not enforced")
	}
	l.release("a")
	if !l.acquire("a") {
		t.Fatal("released slot was not reusable")
	}
}
func TestConfiguredDuration(t *testing.T) {
	if got := configuredDuration("12s", time.Minute); got != 12*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := configuredDuration("bad", time.Minute); got != time.Minute {
		t.Fatalf("got %v", got)
	}
}

func TestIPMessageLimiter(t *testing.T) {
	l := newIPMessageLimiter(1)
	if !l.allow("192.0.2.1") { t.Fatal("first message should be allowed") }
	if l.allow("192.0.2.1") { t.Fatal("second message should be rate limited") }
	if !l.allow("192.0.2.2") { t.Fatal("other client should have its own limit") }
}
