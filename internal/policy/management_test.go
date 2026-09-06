package policy

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDurablePolicyManagement(t *testing.T) {
	m, err := NewManager(&ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policies.yaml")
	if err = m.UsePersistentFile(path); err != nil {
		t.Fatal(err)
	}
	p := PolicyConfig{Name: "managed", Type: PolicyTypeStarlark, Enabled: true, Scope: PolicyScope{Type: "global"}, Script: `reject("first")`, MaxExecutionTime: time.Second}
	if err = m.SavePolicy(p, false); err != nil {
		t.Fatal(err)
	}
	restored, err := NewManager(&ManagerConfig{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	a, err := restored.TestPolicy(context.Background(), p.Name, &EmailContext{})
	if err != nil || a.Reason != "first" {
		t.Fatal(a, err)
	}
	p.Script = `reject("updated")`
	if err = m.SavePolicy(p, true); err != nil {
		t.Fatal(err)
	}
	p.Script = "syntax ! invalid"
	if err = m.SavePolicy(p, true); err == nil {
		t.Fatal("invalid update accepted")
	}
	if err = m.Reload(); err != nil {
		t.Fatal(err)
	}
	a, err = m.TestPolicy(context.Background(), p.Name, &EmailContext{})
	if err != nil || a.Reason != "updated" {
		t.Fatal(a, err)
	}
	if err = m.DeletePolicy(p.Name); err != nil {
		t.Fatal(err)
	}
	if err = m.Reload(); err != nil {
		t.Fatal(err)
	}
	if len(m.ListPolicies()) != 0 {
		t.Fatal("deleted policy returned")
	}
}
