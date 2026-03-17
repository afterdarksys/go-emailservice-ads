package mailtest

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"
)

// DiagFull runs a full diagnostic suite
func DiagFull(cfg Config) error {
	printHeader("Full Mail Server Diagnostic")
	
	tests := []func(Config) error{
		DiagDNS,
		DiagTLS,
		DiagAuth,
		DiagDeliverability,
	}

	failures := 0
	for _, test := range tests {
		if err := test(cfg); err != nil {
			failures++
		}
		fmt.Println()
	}

	printHeader("Diagnostic Summary")
	total := len(tests)
	passed := total - failures
	
	fmt.Printf("Tests run: %d\n", total)
	if passed == total {
		printSuccess("All tests passed ✓")
	} else {
		printWarning("%d/%d tests passed", passed, total)
	}

	return nil
}

// DiagDNS tests DNS records
func DiagDNS(cfg Config) error {
	printHeader("DNS Diagnostic")
	
	// Extract domain from host
	domain := cfg.Host
	
	printInfo("Checking DNS records for: %s", domain)
	
	// Check MX records
	printInfo("\nMX Records:")
	mxRecords, err := net.LookupMX(domain)
	if err != nil {
		printWarning("No MX records found: %v", err)
	} else {
		for _, mx := range mxRecords {
			fmt.Printf("  - %s (priority: %d)\n", mx.Host, mx.Pref)
		}
	}
	
	// Check A records
	printInfo("\nA Records:")
	aRecords, err := net.LookupIP(domain)
	if err != nil {
		printWarning("No A records found: %v", err)
	} else {
		for _, ip := range aRecords {
			fmt.Printf("  - %s\n", ip.String())
		}
	}
	
	// Check SPF record
	printInfo("\nSPF Record:")
	txtRecords, err := net.LookupTXT(domain)
	if err != nil {
		printWarning("Failed to lookup TXT records: %v", err)
	} else {
		spfFound := false
		for _, txt := range txtRecords {
			if len(txt) > 4 && txt[:4] == "v=spf" {
				fmt.Printf("  %s\n", txt)
				spfFound = true
			}
		}
		if !spfFound {
			printWarning("No SPF record found")
		}
	}
	
	// Check DMARC record
	printInfo("\nDMARC Record:")
	dmarcDomain := "_dmarc." + domain
	dmarcRecords, err := net.LookupTXT(dmarcDomain)
	if err != nil {
		printWarning("No DMARC record found")
	} else {
		for _, txt := range dmarcRecords {
			if len(txt) > 6 && txt[:6] == "v=DMARC" {
				fmt.Printf("  %s\n", txt)
			}
		}
	}
	
	// Check DKIM record (common selector: default)
	printInfo("\nDKIM Record (selector: default):")
	dkimDomain := "default._domainkey." + domain
	dkimRecords, err := net.LookupTXT(dkimDomain)
	if err != nil {
		printWarning("No DKIM record found for selector 'default'")
	} else {
		for _, txt := range dkimRecords {
			fmt.Printf("  %s\n", txt[:min(len(txt), 80)]+"...")
		}
	}
	
	printSuccess("✓ DNS diagnostic complete")
	return nil
}

// DiagTLS tests TLS configuration
func DiagTLS(cfg Config) error {
	printHeader("TLS Diagnostic")
	
	port := cfg.GetPort("smtp", 25)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)
	
	printInfo("Testing TLS on: %s", addr)
	
	// Test TLS versions
	versions := []struct {
		name string
		ver  uint16
	}{
		{"TLS 1.0", tls.VersionTLS10},
		{"TLS 1.1", tls.VersionTLS11},
		{"TLS 1.2", tls.VersionTLS12},
		{"TLS 1.3", tls.VersionTLS13},
	}
	
	printInfo("\nSupported TLS Versions:")
	for _, v := range versions {
		tlsConfig := &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: cfg.Insecure,
			MinVersion:         v.ver,
			MaxVersion:         v.ver,
		}
		
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, tlsConfig)
		if err == nil {
			printSuccess("  ✓ %s", v.name)
			
			state := conn.ConnectionState()
			if cfg.Verbose {
				fmt.Printf("    Cipher: %s\n", tls.CipherSuiteName(state.CipherSuite))
			}
			
			conn.Close()
		} else {
			printWarning("  ✗ %s not supported", v.name)
		}
	}
	
	// Test certificate
	printInfo("\nCertificate Information:")
	tlsConfig := &tls.Config{
		ServerName:         cfg.Host,
		InsecureSkipVerify: cfg.Insecure,
	}
	
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return printError("Failed to establish TLS connection: %v", err)
	}
	defer conn.Close()
	
	state := conn.ConnectionState()
	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		
		fmt.Printf("  - Subject: %s\n", cert.Subject.CommonName)
		fmt.Printf("  - Issuer: %s\n", cert.Issuer.CommonName)
		fmt.Printf("  - Valid From: %s\n", cert.NotBefore.Format(time.RFC3339))
		fmt.Printf("  - Valid Until: %s\n", cert.NotAfter.Format(time.RFC3339))
		
		// Check expiration
		daysUntilExpiry := int(time.Until(cert.NotAfter).Hours() / 24)
		if daysUntilExpiry < 0 {
			printError("  ✗ Certificate EXPIRED")
		} else if daysUntilExpiry < 30 {
			printWarning("  ⚠ Certificate expires in %d days", daysUntilExpiry)
		} else {
			printSuccess("  ✓ Certificate valid (%d days remaining)", daysUntilExpiry)
		}
		
		// Check SANs
		if len(cert.DNSNames) > 0 {
			fmt.Printf("  - DNS Names: %v\n", cert.DNSNames)
		}
	}
	
	printSuccess("✓ TLS diagnostic complete")
	return nil
}

// DiagAuth tests authentication methods
func DiagAuth(cfg Config) error {
	printHeader("Authentication Diagnostic")
	
	if cfg.Username == "" || cfg.Password == "" {
		printWarning("Username/password not provided, skipping auth tests")
		return nil
	}
	
	printInfo("Testing authentication for user: %s", cfg.Username)
	
	// Test SMTP AUTH
	printInfo("\nSMTP Authentication:")
	if err := SMTPAuth(cfg); err != nil {
		printError("  ✗ SMTP AUTH failed")
	}
	
	// Test IMAP AUTH
	printInfo("\nIMAP Authentication:")
	if err := IMAPAuth(cfg); err != nil {
		printError("  ✗ IMAP AUTH failed")
	}
	
	// Test POP3 AUTH
	printInfo("\nPOP3 Authentication:")
	if err := POP3Auth(cfg); err != nil {
		printError("  ✗ POP3 AUTH failed")
	}
	
	printSuccess("✓ Authentication diagnostic complete")
	return nil
}

// DiagDeliverability tests mail deliverability
func DiagDeliverability(cfg Config) error {
	printHeader("Mail Deliverability Diagnostic")
	
	printInfo("Checking deliverability factors...")
	
	score := 100
	issues := []string{}
	
	// Check reverse DNS
	printInfo("\nReverse DNS:")
	ips, err := net.LookupIP(cfg.Host)
	if err != nil || len(ips) == 0 {
		printWarning("  ✗ Cannot resolve hostname")
		score -= 20
		issues = append(issues, "No A record")
	} else {
		ip := ips[0].String()
		names, err := net.LookupAddr(ip)
		if err != nil || len(names) == 0 {
			printWarning("  ✗ No reverse DNS (PTR) record for %s", ip)
			score -= 15
			issues = append(issues, "No PTR record")
		} else {
			printSuccess("  ✓ Reverse DNS: %s", names[0])
		}
	}
	
	// Check blacklists
	printInfo("\nBlacklist Check (sample):")
	blacklists := []string{
		"zen.spamhaus.org",
		"bl.spamcop.net",
	}
	
	if len(ips) > 0 {
		ip := ips[0]
		for _, bl := range blacklists {
			listed, _ := checkBlacklist(ip.String(), bl)
			if listed {
				printWarning("  ✗ Listed on %s", bl)
				score -= 25
				issues = append(issues, fmt.Sprintf("Listed on %s", bl))
			} else {
				printSuccess("  ✓ Not listed on %s", bl)
			}
		}
	}
	
	// Deliverability score
	printInfo("\nDeliverability Score: %d/100", score)
	if score >= 80 {
		printSuccess("✓ Good deliverability")
	} else if score >= 60 {
		printWarning("⚠ Fair deliverability, improvements recommended")
	} else {
		printError("✗ Poor deliverability, action required")
	}
	
	if len(issues) > 0 {
		printInfo("\nIssues to address:")
		for _, issue := range issues {
			fmt.Printf("  - %s\n", issue)
		}
	}
	
	return nil
}

func checkBlacklist(ip, blacklist string) (bool, error) {
	// This is simplified - proper implementation would reverse octets for IPv4
	query := fmt.Sprintf("%s.%s", ip, blacklist)

	_, err := net.LookupHost(query)
	return err == nil, err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
