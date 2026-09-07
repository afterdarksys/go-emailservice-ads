package config

import (
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

func (c *Config) validateSafety() error {
	for _, raw := range c.Server.Relay.AllowedNetworks {
		ip, n, e := net.ParseCIDR(raw)
		if e != nil {
			return fmt.Errorf("invalid relay network %q", raw)
		}
		last := append(net.IP(nil), n.IP...)
		for i := range last {
			last[i] |= ^n.Mask[i]
		}
		private := func(ip net.IP) bool { return ip.IsPrivate() || ip.IsLoopback() }
		if !private(ip) || !private(n.IP) || !private(last) {
			return fmt.Errorf("relay networks must be private or loopback; use SMTP AUTH for public clients")
		}
	}
	if c.Server.AllowInsecureAuth {
		host, _, e := net.SplitHostPort(c.Server.Addr)
		ip := net.ParseIP(host)
		if e != nil || ip == nil || !ip.IsLoopback() || len(c.Platform.Listeners) > 0 {
			return fmt.Errorf("insecure SMTP AUTH is limited to an explicit loopback test listener")
		}
	}
	if (c.Server.RequireAuth || c.Server.Role == "submission") && len(c.Server.AuthMechanisms) == 0 {
		return fmt.Errorf("authenticated submission requires a SASL mechanism")
	}
	if c.Server.Role == "submission" && (!c.Server.RequireAuth || !c.Server.RequireTLS || c.Server.AllowInsecureAuth) {
		return fmt.Errorf("submission requires authentication and TLS")
	}
	for _, l := range c.Platform.Listeners {
		if l.Role == "submission" && len(c.Server.AuthMechanisms) == 0 {
			return fmt.Errorf("submission listener requires a SASL mechanism")
		}
	}
	addresses := []string{c.API.RESTAddr}
	if c.API.GRPCEnabled {
		addresses = append(addresses, c.API.GRPCAddr)
	}
	if !c.IMAP.Disabled {
		addresses = append(addresses, c.IMAP.Addr)
	}
	if c.JMAP.Enabled {
		addresses = append(addresses, c.JMAP.Addr)
	}
	if len(c.Platform.Listeners) == 0 {
		addresses = append(addresses, c.Server.Addr)
	} else {
		for _, l := range c.Platform.Listeners {
			addresses = append(addresses, l.Addr)
		}
	}
	type binding struct{ host, port string }
	var seen []binding
	for _, addr := range addresses {
		h, p, e := net.SplitHostPort(addr)
		n, pe := strconv.Atoi(p)
		if e != nil || pe != nil || n < 1 || n > 65535 {
			return fmt.Errorf("invalid listener address %q", addr)
		}
		wildcard := func(v string) bool { return v == "" || v == "0.0.0.0" || v == "::" }
		for _, old := range seen {
			if p == old.port && (h == old.host || wildcard(h) || wildcard(old.host)) {
				return fmt.Errorf("overlapping listeners on port %s", p)
			}
		}
		seen = append(seen, binding{h, p})
	}
	for _, d := range c.Server.LocalDomains {
		if d == "" || strings.ContainsAny(d, "*/ @\r\n") {
			return fmt.Errorf("invalid local domain %q", d)
		}
	}
	return nil
}

// ValidateReloadFrom rejects changes needing an explicit data migration or
// ownership handoff. A process reload must not accidentally open an empty store.
func (c *Config) ValidateReloadFrom(old *Config) error {
	if filepath.Clean(c.Platform.DataDir) != filepath.Clean(old.Platform.DataDir) || c.Auth.UserDatabaseURL != old.Auth.UserDatabaseURL || c.Platform.FencingLeaseFile != old.Platform.FencingLeaseFile || !reflect.DeepEqual(c.Platform.HA, old.Platform.HA) {
		return fmt.Errorf("storage or HA ownership changes require a stopped-service migration")
	}
	return nil
}

// ValidateRuntimeSettings catches known-unusable listener combinations without
// contacting providers or reading certificate contents.
func (c *Config) ValidateRuntimeSettings() error {
	configured := func(t *TLSConfig) bool { return t != nil && t.Cert != "" && t.Key != "" }
	if len(c.Platform.Listeners) == 0 && (c.Server.RequireTLS || c.Server.RequireAuth && !c.Server.AllowInsecureAuth) && !configured(c.Server.TLS) {
		return fmt.Errorf("SMTP requires TLS but certificate/key settings are missing")
	}
	if !c.IMAP.Disabled && c.IMAP.TLSMode != "disabled" && !configured(c.IMAP.TLS) {
		return fmt.Errorf("IMAP TLS mode requires certificate/key settings")
	}
	return nil
}
