package policy

import (
	"context"
	"net/mail"
	"os"
	"strings"
	"testing"

	"go.starlark.net/starlark"
)

func TestInternalLayerExample(t *testing.T) {
	raw, err := os.ReadFile("../../examples/policies/internal-layer.star")
	if err != nil {
		t.Fatal(err)
	}
	e, _ := newStarlarkEngine()
	for _, tc := range []struct {
		context EmailContext
		want    ActionType
	}{
		{EmailContext{VirusStatus: "infected"}, ActionReject},
		{EmailContext{IsInternal: true, RemoteIP: "203.0.113.1"}, ActionQuarantine},
		{EmailContext{IsInternal: true, RemoteIP: "10.0.0.1"}, ActionAccept},
	} {
		a, err := e.Evaluate(context.Background(), &tc.context, string(raw))
		if err != nil || a.Type != tc.want {
			t.Fatalf("example: %+v %v", a, err)
		}
	}
}

func TestFilterEntrypointAndImmutableMessage(t *testing.T) {
	e, _ := newStarlarkEngine()
	m := &EmailContext{From: "service@example.test", RemoteIP: "::ffff:10.2.3.4", To: []string{"user@example.test"}, Headers: mail.Header{"X-Tag": []string{"one", "two"}}}
	a, err := e.Evaluate(context.Background(), m, `
def filter(message):
    if cidr_contains("10.0.0.0/8", message.remote_ip) and message.headers["x-tag"] == ("one", "two"):
        print("matched internal sender")
        reject("test decision")
`)
	if err != nil || a.Type != ActionReject || len(a.Trace) != 1 {
		t.Fatalf("filter: %+v %v", a, err)
	}
	_, err = e.Evaluate(context.Background(), m, `message.headers["x-tag"] = ("changed",)`)
	if err == nil || m.Headers.Get("X-Tag") != "one" {
		t.Fatalf("mutable message: %v", err)
	}
	for _, script := range []string{`misspelled_action()`, `filter = 1`, "def filter(message):\n    return True"} {
		if _, err := e.Evaluate(context.Background(), m, script); err == nil {
			t.Fatalf("invalid script accepted: %s", script)
		}
	}
	if err := e.Validate(`misspelled_action()`); err == nil {
		t.Fatal("validation missed undefined name")
	}
}

func TestStarlarkBudgetsAndTraceIsolation(t *testing.T) {
	e, _ := newStarlarkEngine()
	a, err := e.Evaluate(context.Background(), &EmailContext{}, `
def filter(message):
    for i in range(100):
        print("x" * 1000)
    accept()
`)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, line := range a.Trace {
		total += len(line) + 1
	}
	if total != maxPolicyTraceBytes {
		t.Fatalf("trace budget: %d", total)
	}
	a, err = e.Evaluate(context.Background(), &EmailContext{}, `accept()`)
	if err != nil || len(a.Trace) != 0 {
		t.Fatal("trace leaked between evaluations")
	}
	thread := &starlark.Thread{}
	state(thread).DNSLookups = maxPolicyDNSLookups
	if _, err := policyLookupHost(thread, "example.invalid"); err == nil {
		t.Fatal("DNS budget not enforced before lookup")
	}
	if err := e.Validate(strings.Repeat("#", (1<<20)+1)); err == nil {
		t.Fatal("oversize script accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Evaluate(ctx, &EmailContext{}, `accept()`); err == nil {
		t.Fatal("cancelled evaluation succeeded")
	}
	e.maxSteps = 50
	if _, err := e.Evaluate(context.Background(), &EmailContext{}, "def filter(message):\n    for i in range(1000):\n        x = i * i"); err == nil {
		t.Fatal("filter bypassed instruction budget")
	}
}
