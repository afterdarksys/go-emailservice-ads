package mailtest

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
)

// POP3Connect tests POP3 connection
func POP3Connect(cfg Config) error {
	port := cfg.GetPort("pop3", 110)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("POP3 Connection Test")
	printInfo("Connecting to: %s", addr)

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
	
	// Read greeting
	greeting, err := reader.ReadString('\n')
	if err != nil {
		return printError("Failed to read greeting: %v", err)
	}

	if cfg.Verbose {
		printProtocol("<", greeting)
	}

	if !strings.HasPrefix(greeting, "+OK") {
		return printError("Invalid greeting: %s", greeting)
	}

	printSuccess("✓ Connection successful")
	printSuccess("✓ Banner: %s", strings.TrimSpace(greeting[4:]))

	// Send QUIT
	fmt.Fprintf(conn, "QUIT\r\n")
	
	return nil
}

// POP3Auth tests POP3 authentication
func POP3Auth(cfg Config) error {
	if cfg.Username == "" || cfg.Password == "" {
		return printError("Username and password required for POP3 AUTH test")
	}

	port := cfg.GetPort("pop3", 110)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("POP3 Authentication Test")
	printInfo("Connecting to: %s", addr)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	
	// Read greeting
	greeting, _ := reader.ReadString('\n')
	if cfg.Verbose {
		printProtocol("<", greeting)
	}

	// Send USER
	fmt.Fprintf(conn, "USER %s\r\n", cfg.Username)
	if cfg.Verbose {
		printProtocol(">", fmt.Sprintf("USER %s", cfg.Username))
	}
	
	response, _ := reader.ReadString('\n')
	if cfg.Verbose {
		printProtocol("<", response)
	}

	if !strings.HasPrefix(response, "+OK") {
		return printError("USER command failed: %s", response)
	}

	// Send PASS
	fmt.Fprintf(conn, "PASS %s\r\n", cfg.Password)
	if cfg.Verbose {
		printProtocol(">", "PASS ********")
	}
	
	response, _ = reader.ReadString('\n')
	if cfg.Verbose {
		printProtocol("<", response)
	}

	if !strings.HasPrefix(response, "+OK") {
		return printError("Authentication failed: %s", response)
	}

	printSuccess("✓ Authentication successful")

	// Send QUIT
	fmt.Fprintf(conn, "QUIT\r\n")

	return nil
}

// POP3Stat gets mailbox statistics
func POP3Stat(cfg Config) error {
	if cfg.Username == "" || cfg.Password == "" {
		return printError("Username and password required")
	}

	port := cfg.GetPort("pop3", 110)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("POP3 Statistics")
	printInfo("Connecting to: %s", addr)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	
	// Read greeting
	reader.ReadString('\n')

	// Login
	fmt.Fprintf(conn, "USER %s\r\n", cfg.Username)
	reader.ReadString('\n')
	
	fmt.Fprintf(conn, "PASS %s\r\n", cfg.Password)
	response, _ := reader.ReadString('\n')
	
	if !strings.HasPrefix(response, "+OK") {
		return printError("Authentication failed")
	}

	// Send STAT
	fmt.Fprintf(conn, "STAT\r\n")
	if cfg.Verbose {
		printProtocol(">", "STAT")
	}
	
	response, _ = reader.ReadString('\n')
	if cfg.Verbose {
		printProtocol("<", response)
	}

	if !strings.HasPrefix(response, "+OK") {
		return printError("STAT command failed: %s", response)
	}

	printSuccess("✓ Mailbox statistics:")
	printInfo(strings.TrimSpace(response[4:]))

	// Send QUIT
	fmt.Fprintf(conn, "QUIT\r\n")

	return nil
}
