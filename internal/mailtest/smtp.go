package mailtest

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// SMTPConnect tests SMTP connection and greeting
func SMTPConnect(cfg Config) error {
	port := cfg.GetPort("smtp", 25)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("SMTP Connection Test")
	printInfo("Connecting to: %s", addr)

	conn, err := net.DialTimeout("tcp", addr, time.Duration(cfg.Timeout)*time.Second)
	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	
	// Read greeting
	greeting, err := reader.ReadString('\n')
	if err != nil {
		return printError("Failed to read greeting: %v", err)
	}

	if cfg.Verbose {
		printProtocol("<", greeting)
	}

	if !strings.HasPrefix(greeting, "220") {
		return printError("Invalid greeting: %s", greeting)
	}

	printSuccess("✓ Connection successful")
	printSuccess("✓ Banner: %s", strings.TrimSpace(greeting[4:]))

	return nil
}

// SMTPEhlo tests EHLO and displays capabilities
func SMTPEhlo(cfg Config) error {
	port := cfg.GetPort("smtp", 25)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("SMTP EHLO Test")
	printInfo("Connecting to: %s", addr)

	client, err := smtp.Dial(addr)
	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer client.Close()

	// Send EHLO
	hostname, _ := os.Hostname()
	if err := client.Hello(hostname); err != nil {
		return printError("EHLO failed: %v", err)
	}

	printSuccess("✓ EHLO successful")
	printInfo("\nServer Capabilities:")

	// Get capabilities by checking supported features
	caps := []struct {
		name string
		test func() bool
	}{
		{"STARTTLS", func() bool {
			ok, _ := client.Extension("STARTTLS")
			return ok
		}},
		{"AUTH", func() bool {
			ok, _ := client.Extension("AUTH")
			return ok
		}},
		{"PIPELINING", func() bool {
			ok, _ := client.Extension("PIPELINING")
			return ok
		}},
		{"8BITMIME", func() bool {
			ok, _ := client.Extension("8BITMIME")
			return ok
		}},
		{"SMTPUTF8", func() bool {
			ok, _ := client.Extension("SMTPUTF8")
			return ok
		}},
		{"SIZE", func() bool {
			ok, param := client.Extension("SIZE")
			if ok && param != "" {
				fmt.Printf("  - SIZE (max: %s bytes)\n", param)
			}
			return ok
		}},
	}

	for _, cap := range caps {
		if cap.test() {
			if cap.name != "SIZE" { // SIZE prints its own message
				fmt.Printf("  - %s\n", cap.name)
			}
		}
	}

	return nil
}

// SMTPStartTLS tests STARTTLS negotiation
func SMTPStartTLS(cfg Config) error {
	port := cfg.GetPort("smtp", 587)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("SMTP STARTTLS Test")
	printInfo("Connecting to: %s", addr)

	client, err := smtp.Dial(addr)
	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer client.Close()

	// Check if STARTTLS is supported
	ok, _ := client.Extension("STARTTLS")
	if !ok {
		return printError("STARTTLS not supported by server")
	}

	printSuccess("✓ STARTTLS supported")

	// Configure TLS
	tlsConfig := &tls.Config{
		ServerName:         cfg.Host,
		InsecureSkipVerify: cfg.Insecure,
	}

	// Start TLS
	if err := client.StartTLS(tlsConfig); err != nil {
		return printError("STARTTLS failed: %v", err)
	}

	printSuccess("✓ STARTTLS negotiation successful")

	// Get TLS connection state
	state, ok := client.TLSConnectionState()
	if ok {
		printInfo("\nTLS Information:")
		fmt.Printf("  - Version: %s\n", tlsVersionString(state.Version))
		fmt.Printf("  - Cipher Suite: %s\n", tls.CipherSuiteName(state.CipherSuite))
		fmt.Printf("  - Server Name: %s\n", state.ServerName)
		if len(state.PeerCertificates) > 0 {
			cert := state.PeerCertificates[0]
			fmt.Printf("  - Certificate Subject: %s\n", cert.Subject)
			fmt.Printf("  - Certificate Issuer: %s\n", cert.Issuer)
			fmt.Printf("  - Valid Until: %s\n", cert.NotAfter.Format(time.RFC3339))
		}
	}

	return nil
}

// SMTPAuth tests SMTP authentication
func SMTPAuth(cfg Config) error {
	if cfg.Username == "" || cfg.Password == "" {
		return printError("Username and password required for AUTH test")
	}

	port := cfg.GetPort("smtp", 587)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("SMTP AUTH Test")
	printInfo("Connecting to: %s", addr)
	printInfo("Username: %s", cfg.Username)

	client, err := smtp.Dial(addr)
	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer client.Close()

	// STARTTLS if needed
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: cfg.Insecure,
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return printError("STARTTLS failed: %v", err)
		}
		printSuccess("✓ STARTTLS successful")
	}

	// Authenticate
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	if err := client.Auth(auth); err != nil {
		return printError("Authentication failed: %v", err)
	}

	printSuccess("✓ Authentication successful")
	return nil
}

// SMTPSend sends a test message
func SMTPSend(cfg Config) error {
	if cfg.Username == "" || cfg.Password == "" {
		return printError("Username and password required for sending")
	}

	port := cfg.GetPort("smtp", 587)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("SMTP Send Test")
	printInfo("Connecting to: %s", addr)

	from := cfg.Username
	to := []string{cfg.Username} // Send to self
	subject := fmt.Sprintf("Test message from mail-test at %s", time.Now().Format(time.RFC3339))
	body := "This is a test message sent by mail-test tool.\n\nIf you receive this, your SMTP server is working correctly."

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n", from, to[0], subject, body)

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	
	if err := smtp.SendMail(addr, auth, from, to, []byte(msg)); err != nil {
		return printError("Failed to send message: %v", err)
	}

	printSuccess("✓ Message sent successfully")
	printInfo("From: %s", from)
	printInfo("To: %s", to[0])
	printInfo("Subject: %s", subject)

	return nil
}

// SMTPInteractive runs an interactive SMTP session
func SMTPInteractive(cfg Config) error {
	port := cfg.GetPort("smtp", 25)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("SMTP Interactive Session")
	printInfo("Connecting to: %s", addr)
	printInfo("Type SMTP commands (QUIT to exit)")

	var conn net.Conn
	var err error

	if cfg.TLS {
		tlsConfig := &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: cfg.Insecure,
		}
		conn, err = tls.Dial("tcp", addr, tlsConfig)
	} else {
		conn, err = net.Dial("tcp", addr)
	}

	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	stdinReader := bufio.NewReader(os.Stdin)

	// Read greeting
	greeting, _ := reader.ReadString('\n')
	printProtocol("<", greeting)

	for {
		fmt.Print("> ")
		command, _ := stdinReader.ReadString('\n')
		command = strings.TrimSpace(command)

		if command == "" {
			continue
		}

		// Send command
		fmt.Fprintf(conn, "%s\r\n", command)
		printProtocol(">", command)

		// Handle STARTTLS
		if strings.ToUpper(command) == "STARTTLS" {
			response, _ := reader.ReadString('\n')
			printProtocol("<", response)

			if strings.HasPrefix(response, "220") {
				tlsConfig := &tls.Config{
					ServerName:         cfg.Host,
					InsecureSkipVerify: cfg.Insecure,
				}
				tlsConn := tls.Client(conn, tlsConfig)
				if err := tlsConn.Handshake(); err != nil {
					printError("TLS handshake failed: %v", err)
					return err
				}
				conn = tlsConn
				reader = bufio.NewReader(conn)
				printSuccess("TLS connection established")
			}
			continue
		}

		// Handle DATA
		if strings.HasPrefix(strings.ToUpper(command), "DATA") {
			response, _ := reader.ReadString('\n')
			printProtocol("<", response)

			if strings.HasPrefix(response, "354") {
				fmt.Println("Enter message (end with line containing only '.')")
				for {
					line, _ := stdinReader.ReadString('\n')
					line = strings.TrimRight(line, "\n")
					fmt.Fprintf(conn, "%s\r\n", line)
					if line == "." {
						break
					}
				}
			}
		}

		// Read response
		response, _ := reader.ReadString('\n')
		printProtocol("<", response)

		if strings.HasPrefix(strings.ToUpper(command), "QUIT") {
			break
		}
	}

	return nil
}

// SMTPBenchmark benchmarks SMTP performance
func SMTPBenchmark(cfg Config) error {
	printHeader("SMTP Benchmark")
	printInfo("Running benchmark with 10 connections...")

	start := time.Now()
	errors := 0

	for i := 0; i < 10; i++ {
		if err := SMTPConnect(cfg); err != nil {
			errors++
		}
	}

	duration := time.Since(start)

	printInfo("\nBenchmark Results:")
	fmt.Printf("  - Total connections: 10\n")
	fmt.Printf("  - Successful: %d\n", 10-errors)
	fmt.Printf("  - Failed: %d\n", errors)
	fmt.Printf("  - Total time: %v\n", duration)
	fmt.Printf("  - Average time per connection: %v\n", duration/10)

	return nil
}

func tlsVersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("Unknown (0x%04x)", version)
	}
}
