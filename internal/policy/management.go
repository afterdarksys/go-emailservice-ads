package policy

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// UsePersistentFile seeds a writable management file once from bootstrap policy.
// Subsequently this file is the authoritative source for API edits and reloads.
func (m *Manager) UsePersistentFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		if err = m.LoadPolicies(path); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		if err = m.writePolicies(path, m.ListPolicies()); err != nil {
			return err
		}
	} else {
		return err
	}
	m.configPath = path
	return nil
}
func (m *Manager) writePolicies(path string, policies []*PolicyConfig) error {
	if path == "" {
		return fmt.Errorf("persistent policy file not configured")
	}
	snapshot := PoliciesConfig{}
	for _, p := range policies {
		copy := *p
		copy.ScriptPath = ""
		snapshot.Policies = append(snapshot.Policies, copy)
	}
	raw, err := yaml.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".policies-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	f.Close()
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (m *Manager) SavePolicy(p PolicyConfig, replace bool) error {
	if p.Name == "" || len(p.Name) > 128 || strings.ContainsAny(p.Name, "/\\\r\n") {
		return fmt.Errorf("invalid policy name")
	}
	if p.ScriptPath != "" {
		return fmt.Errorf("API policies require inline script")
	}
	if len(p.Script) > 1<<20 {
		return fmt.Errorf("script too large")
	}
	if p.MaxExecutionTime == 0 {
		p.MaxExecutionTime = 10 * time.Second
	}
	if p.MaxExecutionTime < 0 || p.MaxExecutionTime > 30*time.Second {
		return fmt.Errorf("execution timeout must be at most 30s")
	}
	engine, err := m.getEngine(p.Type)
	if err != nil {
		return err
	}
	if err = engine.Validate(p.Script); err != nil {
		return err
	}
	m.managementMu.Lock()
	defer m.managementMu.Unlock()
	m.policiesMu.Lock()
	defer m.policiesMu.Unlock()
	next := append([]*PolicyConfig(nil), m.policies...)
	index := -1
	for i, old := range next {
		if old.Name == p.Name {
			index = i
			break
		}
	}
	if (index >= 0) != replace {
		return fmt.Errorf("policy existence conflicts with requested operation")
	}
	if index >= 0 {
		next[index] = &p
	} else {
		next = append(next, &p)
	}
	sort.SliceStable(next, func(i, j int) bool { return next[i].Priority < next[j].Priority })
	if err = m.writePolicies(m.configPath, next); err != nil {
		return err
	}
	m.policies = next
	return nil
}
func (m *Manager) DeletePolicy(name string) error {
	m.managementMu.Lock()
	defer m.managementMu.Unlock()
	m.policiesMu.Lock()
	defer m.policiesMu.Unlock()
	next := make([]*PolicyConfig, 0, len(m.policies))
	found := false
	for _, p := range m.policies {
		if p.Name == name {
			found = true
		} else {
			next = append(next, p)
		}
	}
	if !found {
		return fmt.Errorf("policy not found")
	}
	if err := m.writePolicies(m.configPath, next); err != nil {
		return err
	}
	m.policies = next
	return nil
}
func (m *Manager) GetPolicy(name string) (*PolicyConfig, error) {
	for _, p := range m.ListPolicies() {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, fmt.Errorf("policy not found")
}
func (m *Manager) TestPolicy(ctx context.Context, name string, email *EmailContext) (*Action, error) {
	p, err := m.GetPolicy(name)
	if err != nil {
		return nil, err
	}
	engine, err := m.getEngine(p.Type)
	if err != nil {
		return nil, err
	}
	timeout := p.MaxExecutionTime
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return engine.Evaluate(ctx, email, p.Script)
}
