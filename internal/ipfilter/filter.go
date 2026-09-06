// Package ipfilter evaluates SMTP peer addresses without granting relay rights.
package ipfilter

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

type Config struct {
	Allowlist    []string `yaml:"allowlist"`
	Denylist     []string `yaml:"denylist"`
	RBLZones     []string `yaml:"rbl_zones"`
	Timeout      string   `yaml:"timeout"`
	DeferOnError bool     `yaml:"defer_on_error"`
}

func (c Config) Validate() error {
	for _, list := range [][]string{c.Allowlist, c.Denylist} {
		for _, entry := range list {
			if net.ParseIP(entry) == nil {
				if _, _, err := net.ParseCIDR(entry); err != nil {
					return fmt.Errorf("invalid IP or CIDR %q", entry)
				}
			}
		}
	}
	if c.Timeout != "" {
		if d, err := time.ParseDuration(c.Timeout); err != nil || d <= 0 {
			return fmt.Errorf("IP filter timeout must be a positive duration")
		}
	}
	for _, zone := range c.RBLZones {
		name := strings.TrimSuffix(zone, ".")
		if len(name) == 0 || len(name) > 253 {
			return fmt.Errorf("invalid RBL zone %q", zone)
		}
		for _, label := range strings.Split(name, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return fmt.Errorf("invalid RBL zone %q", zone)
			}
			for _, ch := range label {
				if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
					return fmt.Errorf("invalid RBL zone %q", zone)
				}
			}
		}
	}
	return nil
}

type Lookup func(context.Context, string) ([]string, error)

// Check returns an SMTP status (zero means continue). One deadline bounds all
// zones. NXDOMAIN is unlisted; other DNS failures optionally defer delivery.
func Check(ctx context.Context, c Config, peer string, lookup Lookup) (int, string) {
	if len(c.Allowlist)+len(c.Denylist)+len(c.RBLZones) == 0 {
		return 0, ""
	}
	ip := net.ParseIP(peer)
	if ip == nil {
		return 451, "Cannot determine client IP"
	}
	if matches(ip, c.Denylist) {
		return 554, "Client IP denied by local policy"
	}
	if matches(ip, c.Allowlist) {
		return 0, ""
	}
	timeout := 5 * time.Second
	if c.Timeout != "" {
		d, err := time.ParseDuration(c.Timeout)
		if err != nil || d <= 0 {
			return 451, "Invalid IP filter configuration"
		}
		timeout = d
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if lookup == nil {
		lookup = net.DefaultResolver.LookupHost
	}
	failed := false
	for _, zone := range c.RBLZones {
		answers, err := lookup(ctx, reverse(ip)+"."+strings.TrimSuffix(zone, ".")+".")
		if err != nil {
			if dnsErr, ok := err.(*net.DNSError); !ok || !dnsErr.IsNotFound {
				failed = true
			}
			continue
		}
		for _, answer := range answers {
			v4 := net.ParseIP(answer).To4()
			// DNSBL responses must be loopback addresses. 127.255.255.*
			// is reserved by common providers for query/service errors.
			if v4 == nil || v4[0] != 127 || v4[1] == 255 && v4[2] == 255 {
				failed = true
				continue
			}
			return 554, "Client IP listed in " + zone
		}
	}
	if failed && c.DeferOnError {
		return 451, "IP blocklist lookup temporarily unavailable"
	}
	return 0, ""
}

func matches(ip net.IP, entries []string) bool {
	for _, entry := range entries {
		if addr := net.ParseIP(entry); addr != nil && addr.Equal(ip) {
			return true
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func reverse(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d", v4[3], v4[2], v4[1], v4[0])
	}
	hex := fmt.Sprintf("%032x", []byte(ip.To16()))
	parts := make([]string, len(hex))
	for i := range hex {
		parts[i] = string(hex[len(hex)-1-i])
	}
	return strings.Join(parts, ".")
}
