package policy

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const maxPolicyDNSLookups = 10
const maxPolicyTraceBytes = 8192

func stringTuple(values []string) starlark.Tuple {
	result := make(starlark.Tuple, len(values))
	for i, value := range values {
		result[i] = starlark.String(value)
	}
	return result
}

// messageView copies metadata and freezes nested values. Scripts cannot mutate
// the caller's message, and attachment payloads are deliberately excluded.
func messageView(m *EmailContext) starlark.Value {
	headers := starlark.NewDict(len(m.Headers))
	names := make([]string, 0, len(m.Headers))
	for name := range m.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		key := starlark.String(strings.ToLower(name))
		values := stringTuple(m.Headers[name])
		if prior, found, _ := headers.Get(key); found {
			values = append(prior.(starlark.Tuple), values...)
		}
		_ = headers.SetKey(key, values)
	}
	attachments := make(starlark.Tuple, len(m.Attachments))
	for i, a := range m.Attachments {
		attachments[i] = starlarkstruct.FromStringDict(starlark.String("attachment"), starlark.StringDict{
			"filename": starlark.String(a.Filename), "content_type": starlark.String(a.ContentType), "size": starlark.MakeInt64(a.Size),
		})
	}
	value := starlarkstruct.FromStringDict(starlark.String("message"), starlark.StringDict{
		"sender": starlark.String(m.From), "recipients": stringTuple(m.To),
		"remote_ip": starlark.String(m.RemoteIP), "subject": starlark.String(m.Subject),
		"headers": headers, "attachments": attachments, "size": starlark.MakeInt64(m.Size),
		"authenticated": starlark.Bool(m.Authenticated), "username": starlark.String(m.Username),
		"internal": starlark.Bool(m.IsInternal), "inbound": starlark.Bool(m.IsInbound), "outbound": starlark.Bool(m.IsOutbound),
		"spf": starlark.String(m.SPFResult), "dkim": starlark.String(m.DKIMResult),
		"dmarc": starlark.String(m.DMARCResult), "arc": starlark.String(m.ARCResult),
		"ip_reputation": starlark.MakeInt(m.IPReputation.Score), "virus_status": starlark.String(m.VirusStatus),
	})
	value.Freeze()
	return value
}

func cidrContains(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var network, address string
	if err := starlark.UnpackArgs("cidr_contains", args, kwargs, "network", &network, "address", &address); err != nil {
		return nil, err
	}
	prefix, err := netip.ParsePrefix(network)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %w", err)
	}
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return nil, fmt.Errorf("invalid IP: %w", err)
	}
	return starlark.Bool(prefix.Contains(ip.Unmap())), nil
}

func policyLookupHost(thread *starlark.Thread, domain string) ([]string, error) {
	if err := spendDNSLookup(thread); err != nil {
		return nil, err
	}
	return net.DefaultResolver.LookupHost(threadContext(thread), domain)
}

func policyLookupMX(thread *starlark.Thread, domain string) ([]*net.MX, error) {
	if err := spendDNSLookup(thread); err != nil {
		return nil, err
	}
	return net.DefaultResolver.LookupMX(threadContext(thread), domain)
}

func spendDNSLookup(thread *starlark.Thread) error {
	s := state(thread)
	s.DNSLookups++
	if s.DNSLookups > maxPolicyDNSLookups {
		return fmt.Errorf("policy DNS lookup budget exceeded")
	}
	return nil
}

func appendTrace(thread *starlark.Thread, message string) {
	s := state(thread)
	remaining := maxPolicyTraceBytes - s.TraceBytes
	if remaining <= 0 {
		return
	}
	// Count a separator even for empty prints, bounding the number of entries.
	if len(message) >= remaining {
		message = message[:remaining-1]
	}
	s.Trace = append(s.Trace, message)
	s.TraceBytes += len(message) + 1
}
