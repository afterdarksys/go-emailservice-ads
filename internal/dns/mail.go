package dns

import (
	"context"
	"fmt"
	mdns "github.com/miekg/dns"
	"net"
	"strings"
	"time"
)

// MailDNSError distinguishes permanent non-deliverability from resolver outages.
type MailDNSError struct {
	Reason    string
	Permanent bool
}

func (e *MailDNSError) Error() string { return e.Reason }

func (r *Resolver) mailQuery(ctx context.Context, name string, qtype uint16) (*mdns.Msg, error) {
	servers := r.mailServers
	if len(servers) == 0 {
		cfg, err := mdns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil {
			return nil, err
		}
		for _, s := range cfg.Servers {
			servers = append(servers, net.JoinHostPort(s, cfg.Port))
		}
	}
	var last error
	for _, server := range servers {
		query := new(mdns.Msg)
		query.SetQuestion(mdns.Fqdn(name), qtype)
		response, _, err := (&mdns.Client{Timeout: 5 * time.Second}).ExchangeContext(ctx, query, server)
		if err == nil && response.Truncated {
			response, _, err = (&mdns.Client{Net: "tcp", Timeout: 5 * time.Second}).ExchangeContext(ctx, query, server)
		}
		if err != nil {
			last = err
			continue
		}
		switch response.Rcode {
		case mdns.RcodeSuccess, mdns.RcodeNameError:
			return response, nil
		default:
			last = fmt.Errorf("DNS response %s", mdns.RcodeToString[response.Rcode])
		}
	}
	if last == nil {
		last = fmt.Errorf("no DNS resolvers configured")
	}
	return nil, last
}

// LookupMailMX implements SMTP address resolution, including Null MX (RFC 7505).
func (r *Resolver) LookupMailMX(ctx context.Context, domain string) ([]*net.MX, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, ok := mdns.IsDomainName(domain); !ok {
		return nil, &MailDNSError{Reason: "invalid destination domain", Permanent: true}
	}
	name := mdns.Fqdn(domain)
	seen := map[string]bool{}
	for depth := 0; depth < 8; depth++ {
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("DNS CNAME loop")
		}
		seen[key] = true
		response, err := r.mailQuery(ctx, name, mdns.TypeMX)
		if err != nil {
			return nil, err
		}
		if response.Rcode == mdns.RcodeNameError {
			return nil, &MailDNSError{Reason: "destination domain does not exist", Permanent: true}
		}
		var records []*net.MX
		var canonical string
		for _, rr := range response.Answer {
			switch v := rr.(type) {
			case *mdns.MX:
				records = append(records, &net.MX{Host: v.Mx, Pref: v.Preference})
			case *mdns.CNAME:
				if strings.EqualFold(v.Hdr.Name, name) {
					canonical = v.Target
				}
			}
		}
		if len(records) > 0 {
			for _, mx := range records {
				if mx.Host == "." {
					if len(records) == 1 && mx.Pref == 0 {
						return nil, &MailDNSError{Reason: "destination publishes Null MX and accepts no mail", Permanent: true}
					}
					return nil, fmt.Errorf("invalid mixed Null MX response")
				}
			}
			return records, nil
		}
		if canonical != "" {
			name = canonical
			continue
		}
		// NODATA MX permits implicit MX only when an address exists.
		var lookupErr error
		for _, kind := range []uint16{mdns.TypeA, mdns.TypeAAAA} {
			addresses, err := r.mailQuery(ctx, name, kind)
			if err != nil {
				lookupErr = err
				continue
			}
			for _, rr := range addresses.Answer {
				if rr.Header().Rrtype == kind {
					return []*net.MX{{Host: name, Pref: 0}}, nil
				}
			}
		}
		if lookupErr != nil {
			return nil, lookupErr
		}
		return nil, &MailDNSError{Reason: "destination has no MX or address records", Permanent: true}
	}
	return nil, fmt.Errorf("DNS CNAME depth exceeded")
}
