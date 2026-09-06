// Package compliance evaluates explicit envelope-domain preservation rules.
package compliance

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

type Config struct {
	EnforceDomainAccess bool    `yaml:"enforce_domain_access" json:"enforce_domain_access"`
	Access              []Grant `yaml:"access" json:"access"`
	Rules               []Rule  `yaml:"rules" json:"rules"`
}

type Grant struct {
	Principal string   `yaml:"principal" json:"principal"`
	Domains   []string `yaml:"domains" json:"domains"`
	Actions   []string `yaml:"actions" json:"actions"`
}

// Authorized requires permission for every domain in a combined evidence case.
func (c Config) Authorized(principal, domains, action string) bool {
	if !c.EnforceDomainAccess {
		return true
	}
	for _, domain := range strings.Split(domains, ",") {
		allowed := false
		for _, grant := range c.Access {
			if grant.Principal != principal {
				continue
			}
			hasAction := false
			for _, a := range grant.Actions {
				hasAction = hasAction || a == action || a == "*"
			}
			if !hasAction {
				continue
			}
			for _, d := range grant.Domains {
				allowed = allowed || d == "*" || strings.EqualFold(d, domain)
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}

type Rule struct {
	Name      string        `yaml:"name" json:"name"`
	Domain    string        `yaml:"domain" json:"domain"`
	Match     string        `yaml:"match" json:"match"` // sender, recipient, either
	Mode      string        `yaml:"mode" json:"mode"`   // hold, reroute, copy, bcc
	BCC       string        `yaml:"bcc" json:"bcc,omitempty"`
	Retention time.Duration `yaml:"retention" json:"retention"`
	LegalHold bool          `yaml:"legal_hold" json:"legal_hold"`
}

func (c Config) Validate() error {
	for _, grant := range c.Access {
		if grant.Principal == "" || len(grant.Domains) == 0 || len(grant.Actions) == 0 {
			return fmt.Errorf("compliance grants require principal, domains and actions")
		}
		for _, domain := range grant.Domains {
			if domain == "" || strings.ContainsAny(domain, " ,@/\r\n") {
				return fmt.Errorf("invalid compliance access domain")
			}
		}
		for _, action := range grant.Actions {
			switch action {
			case "*", "read", "export", "release", "delete", "legal-hold":
			default:
				return fmt.Errorf("invalid compliance access action")
			}
		}
	}
	names := map[string]bool{}
	for _, r := range c.Rules {
		if r.Name == "" || len(r.Name) > 128 || names[r.Name] || strings.ContainsAny(r.Name, "/\r\n") {
			return fmt.Errorf("invalid or duplicate compliance rule name")
		}
		names[r.Name] = true
		if r.Domain == "" || strings.ContainsAny(r.Domain, " ,@/\r\n") || r.Domain == "*" {
			return fmt.Errorf("compliance requires an explicit domain")
		}
		switch r.Match {
		case "", "either", "sender", "recipient":
		default:
			return fmt.Errorf("invalid compliance match")
		}
		switch r.Mode {
		case "hold", "reroute", "copy", "bcc":
		default:
			return fmt.Errorf("invalid compliance mode")
		}
		if r.Retention < 0 {
			return fmt.Errorf("negative compliance retention")
		}
		if r.Mode == "bcc" {
			a, e := mail.ParseAddress(r.BCC)
			if e != nil || a.Address != r.BCC || strings.ContainsAny(r.BCC, "\r\n") {
				return fmt.Errorf("invalid compliance BCC")
			}
		}
	}
	return nil
}
func domain(address string) string {
	_, d, ok := strings.Cut(address, "@")
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(d, "."))
}
func (c Config) Evaluate(from string, to []string) []Rule {
	var out []Rule
	for _, r := range c.Rules {
		d := strings.ToLower(strings.TrimSuffix(r.Domain, "."))
		matched := r.Match != "recipient" && domain(from) == d
		if r.Match != "sender" {
			for _, a := range to {
				matched = matched || domain(a) == d
			}
		}
		if matched {
			out = append(out, r)
		}
	}
	return out
}
