package security

import (
	"context"
	"fmt"
	"net"
	"strings"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/dns"
)

// SPFResult represents the result of SPF verification
type SPFResult string

const (
	SPFPass      SPFResult = "pass"
	SPFFail      SPFResult = "fail"
	SPFSoftFail  SPFResult = "softfail"
	SPFNeutral   SPFResult = "neutral"
	SPFNone      SPFResult = "none"
	SPFTempError SPFResult = "temperror"
	SPFPermError SPFResult = "permerror"
)

// DMARCResult represents the result of DMARC verification
type DMARCResult string

const (
	DMARCPass DMARCResult = "pass"
	DMARCFail DMARCResult = "fail"
	DMARCNone DMARCResult = "none"
)

// DMARCPolicy represents the DMARC policy action
type DMARCPolicy string

const (
	DMARCPolicyNone       DMARCPolicy = "none"
	DMARCPolicyQuarantine DMARCPolicy = "quarantine"
	DMARCPolicyReject     DMARCPolicy = "reject"
)

// PolicyEngine handles SPF, DKIM, and DMARC validations
// RFC 7208 - Sender Policy Framework (SPF)
// RFC 7489 - Domain-based Message Authentication, Reporting, and Conformance (DMARC)
type PolicyEngine struct {
	logger   *zap.Logger
	resolver *dns.Resolver
}

func NewPolicyEngine(logger *zap.Logger, resolver *dns.Resolver) *PolicyEngine {
	return &PolicyEngine{
		logger:   logger,
		resolver: resolver,
	}
}

// VerifySPF performs SPF verification
// RFC 7208 - Sender Policy Framework (SPF) for Authorizing Use of Domains in Email
func (p *PolicyEngine) VerifySPF(ctx context.Context, ip net.IP, domain, sender string) (SPFResult, error) {
	p.logger.Debug("Verifying SPF",
		zap.String("domain", domain),
		zap.String("ip", ip.String()),
		zap.String("sender", sender))

	// Lookup SPF record (TXT record starting with "v=spf1")
	txtRecords, err := p.resolver.LookupTXT(ctx, domain)
	if err != nil {
		p.logger.Warn("SPF TXT lookup failed", zap.String("domain", domain), zap.Error(err))
		return SPFTempError, err
	}

	// Find SPF record
	var spfRecord string
	for _, record := range txtRecords {
		if strings.HasPrefix(record, "v=spf1") {
			if spfRecord != "" {
				// Multiple SPF records is a PermError (RFC 7208 Section 3.2)
				p.logger.Warn("Multiple SPF records found", zap.String("domain", domain))
				return SPFPermError, fmt.Errorf("multiple SPF records")
			}
			spfRecord = record
		}
	}

	if spfRecord == "" {
		p.logger.Debug("No SPF record found", zap.String("domain", domain))
		return SPFNone, nil
	}

	// Parse and evaluate SPF record
	result := p.evaluateSPF(ctx, spfRecord, ip, domain, sender, 0)

	p.logger.Info("SPF verification complete",
		zap.String("domain", domain),
		zap.String("result", string(result)))

	return result, nil
}

// evaluateSPF recursively evaluates SPF mechanisms
// RFC 7208 Section 5 - Mechanism Evaluation
func (p *PolicyEngine) evaluateSPF(ctx context.Context, spfRecord string, ip net.IP, domain, sender string, depth int) SPFResult {
	// Prevent infinite recursion (max 10 DNS lookups per RFC 7208 Section 4.6.4)
	if depth > 10 {
		return SPFPermError
	}

	// Parse SPF mechanisms
	parts := strings.Fields(spfRecord)
	if len(parts) == 0 || parts[0] != "v=spf1" {
		return SPFPermError
	}

	// Default result if no mechanism matches
	defaultResult := SPFNeutral

	// Evaluate each mechanism
	for _, mechanism := range parts[1:] {
		// Handle qualifiers: + (pass), - (fail), ~ (softfail), ? (neutral)
		qualifier := "+"
		if len(mechanism) > 0 && (mechanism[0] == '+' || mechanism[0] == '-' || mechanism[0] == '~' || mechanism[0] == '?') {
			qualifier = string(mechanism[0])
			mechanism = mechanism[1:]
		}

		// Extract mechanism type and value
		mechanismParts := strings.SplitN(mechanism, ":", 2)
		mechType := mechanismParts[0]
		mechValue := ""
		if len(mechanismParts) > 1 {
			mechValue = mechanismParts[1]
		}

		// Evaluate mechanism
		match := false
		switch mechType {
		case "ip4":
			match = p.matchIP4(ip, mechValue)
		case "ip6":
			match = p.matchIP6(ip, mechValue)
		case "a":
			targetDomain := domain
			if mechValue != "" {
				targetDomain = mechValue
			}
			match = p.matchA(ctx, ip, targetDomain, depth)
		case "mx":
			targetDomain := domain
			if mechValue != "" {
				targetDomain = mechValue
			}
			match = p.matchMX(ctx, ip, targetDomain, depth)
		case "include":
			if mechValue == "" {
				return SPFPermError
			}
			result := p.evaluateSPFDomain(ctx, mechValue, ip, sender, depth+1)
			if result == SPFPass {
				match = true
			}
		case "all":
			match = true
		default:
			// Unknown mechanism, skip
			p.logger.Debug("Unknown SPF mechanism", zap.String("mechanism", mechType))
			continue
		}

		// If mechanism matches, return result based on qualifier
		if match {
			switch qualifier {
			case "+":
				return SPFPass
			case "-":
				return SPFFail
			case "~":
				return SPFSoftFail
			case "?":
				return SPFNeutral
			}
		}
	}

	return defaultResult
}

// evaluateSPFDomain performs SPF evaluation for a specific domain
func (p *PolicyEngine) evaluateSPFDomain(ctx context.Context, domain string, ip net.IP, sender string, depth int) SPFResult {
	txtRecords, err := p.resolver.LookupTXT(ctx, domain)
	if err != nil {
		return SPFTempError
	}

	for _, record := range txtRecords {
		if strings.HasPrefix(record, "v=spf1") {
			return p.evaluateSPF(ctx, record, ip, domain, sender, depth)
		}
	}

	return SPFNone
}

// matchIP4 checks if IP matches an IPv4 CIDR range
func (p *PolicyEngine) matchIP4(ip net.IP, cidr string) bool {
	if ip.To4() == nil {
		return false // Not an IPv4 address
	}

	// Default to /32 if no CIDR specified
	if !strings.Contains(cidr, "/") {
		cidr = cidr + "/32"
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		p.logger.Warn("Invalid IPv4 CIDR", zap.String("cidr", cidr))
		return false
	}

	return ipNet.Contains(ip)
}

// matchIP6 checks if IP matches an IPv6 CIDR range
func (p *PolicyEngine) matchIP6(ip net.IP, cidr string) bool {
	if ip.To4() != nil {
		return false // Not an IPv6 address
	}

	// Default to /128 if no CIDR specified
	if !strings.Contains(cidr, "/") {
		cidr = cidr + "/128"
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		p.logger.Warn("Invalid IPv6 CIDR", zap.String("cidr", cidr))
		return false
	}

	return ipNet.Contains(ip)
}

// matchA checks if IP matches any A record for the domain
func (p *PolicyEngine) matchA(ctx context.Context, ip net.IP, domain string, depth int) bool {
	if depth > 10 {
		return false
	}

	ips, err := p.resolver.LookupIP(ctx, domain)
	if err != nil {
		p.logger.Debug("A record lookup failed", zap.String("domain", domain))
		return false
	}

	for _, recordIP := range ips {
		if recordIP.Equal(ip) {
			return true
		}
	}

	return false
}

// matchMX checks if IP matches any MX host's A record
func (p *PolicyEngine) matchMX(ctx context.Context, ip net.IP, domain string, depth int) bool {
	if depth > 10 {
		return false
	}

	mxRecords, err := p.resolver.LookupMX(ctx, domain)
	if err != nil {
		p.logger.Debug("MX lookup failed", zap.String("domain", domain))
		return false
	}

	for _, mx := range mxRecords {
		mxHost := strings.TrimSuffix(mx.Host, ".")
		if p.matchA(ctx, ip, mxHost, depth+1) {
			return true
		}
	}

	return false
}

// VerifyDMARC performs DMARC verification
// RFC 7489 - Domain-based Message Authentication, Reporting, and Conformance (DMARC)
func (p *PolicyEngine) VerifyDMARC(ctx context.Context, domain string, spfResult SPFResult, dkimResult string) (DMARCResult, DMARCPolicy, error) {
	p.logger.Debug("Verifying DMARC",
		zap.String("domain", domain),
		zap.String("spf_result", string(spfResult)),
		zap.String("dkim_result", dkimResult))

	// Lookup DMARC record (_dmarc.domain.com)
	dmarcDomain := "_dmarc." + domain
	txtRecords, err := p.resolver.LookupTXT(ctx, dmarcDomain)
	if err != nil {
		p.logger.Debug("DMARC TXT lookup failed", zap.String("domain", dmarcDomain))
		return DMARCNone, DMARCPolicyNone, nil
	}

	// Find DMARC record
	var dmarcRecord string
	for _, record := range txtRecords {
		if strings.HasPrefix(record, "v=DMARC1") {
			dmarcRecord = record
			break
		}
	}

	if dmarcRecord == "" {
		p.logger.Debug("No DMARC record found", zap.String("domain", domain))
		return DMARCNone, DMARCPolicyNone, nil
	}

	// Parse DMARC policy
	policy := p.parseDMARCPolicy(dmarcRecord)

	// Check SPF and DKIM alignment (simplified - checks if either passes)
	// RFC 7489 Section 3.1 - DMARC Policy Evaluation
	dmarcPass := false

	if spfResult == SPFPass {
		dmarcPass = true
	}

	if dkimResult == "pass" {
		dmarcPass = true
	}

	result := DMARCFail
	if dmarcPass {
		result = DMARCPass
	}

	p.logger.Info("DMARC verification complete",
		zap.String("domain", domain),
		zap.String("result", string(result)),
		zap.String("policy", string(policy)))

	return result, policy, nil
}

// parseDMARCPolicy extracts the policy from a DMARC record
func (p *PolicyEngine) parseDMARCPolicy(dmarcRecord string) DMARCPolicy {
	// Default policy is "none"
	policy := DMARCPolicyNone

	// Parse DMARC tags
	parts := strings.Split(dmarcRecord, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "p=") {
			policyStr := strings.TrimPrefix(part, "p=")
			switch policyStr {
			case "reject":
				policy = DMARCPolicyReject
			case "quarantine":
				policy = DMARCPolicyQuarantine
			case "none":
				policy = DMARCPolicyNone
			}
		}
	}

	return policy
}
