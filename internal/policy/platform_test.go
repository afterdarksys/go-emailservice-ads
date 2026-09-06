package policy

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPolicyActionsAndHeaders(t *testing.T) {
	m, e := NewManager(&ManagerConfig{Logger: zap.NewNop()})
	if e != nil {
		t.Fatal(e)
	}
	for i, script := range []string{`add_header("X-Checked", "yes")`, `reject("blocked")`} {
		if e = m.AddPolicy(&PolicyConfig{Name: fmt.Sprint(i), Type: PolicyTypeStarlark, Enabled: true, Priority: i, Script: script, Scope: PolicyScope{Type: "global"}, MaxExecutionTime: time.Second}); e != nil {
			t.Fatal(e)
		}
	}
	a, e := m.Evaluate(context.Background(), &EmailContext{})
	if e != nil || a.Type != ActionReject || len(a.Headers) != 1 {
		t.Fatalf("action=%+v error=%v", a, e)
	}
	m.RemovePolicy("1")
	if e = m.AddPolicy(&PolicyConfig{Name: "1", Type: PolicyTypeStarlark, Enabled: true, Priority: 1, Script: `fail("error")`, Scope: PolicyScope{Type: "global"}, MaxExecutionTime: time.Second}); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Evaluate(context.Background(), &EmailContext{}); e == nil {
		t.Fatal("evaluation error swallowed")
	}
}
func TestScriptPathAndAtomicReload(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "policy.star")
	path := filepath.Join(dir, "policies.yaml")
	os.WriteFile(script, []byte(`reject("loaded")`), 0600)
	os.WriteFile(path, []byte("policies:\n  - name: loaded\n    enabled: true\n    type: starlark\n    scope: {type: global}\n    script_path: "+script+"\n"), 0600)
	m, e := NewManager(&ManagerConfig{Logger: zap.NewNop(), ConfigPath: path})
	if e != nil {
		t.Fatal(e)
	}
	a, e := m.Evaluate(context.Background(), &EmailContext{})
	if e != nil || a.Reason != "loaded" {
		t.Fatalf("%+v %v", a, e)
	}
	os.WriteFile(script, []byte("syntax ! invalid"), 0600)
	if e = m.Reload(); e == nil {
		t.Fatal("invalid required script accepted")
	}
	a, e = m.Evaluate(context.Background(), &EmailContext{})
	if e != nil || a.Reason != "loaded" {
		t.Fatal("failed reload replaced working policy")
	}
}
func TestStarlarkConcurrentStateIsolation(t *testing.T) {
	e, _ := newStarlarkEngine()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprint(i)
			a, err := e.Evaluate(context.Background(), &EmailContext{}, fmt.Sprintf("reject(%q)", want))
			if err != nil || a.Reason != want {
				t.Errorf("cross-session action: %+v %v", a, err)
			}
		}(i)
	}
	wg.Wait()
}
