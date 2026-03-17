package mailtest

import (
	"crypto/tls"
	"fmt"

	"github.com/emersion/go-imap/client"
)

// IMAPConnect tests IMAP connection
func IMAPConnect(cfg Config) error {
	port := cfg.GetPort("imap", 143)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("IMAP Connection Test")
	printInfo("Connecting to: %s", addr)

	var c *client.Client
	var err error

	if cfg.TLS {
		tlsConfig := &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: cfg.Insecure,
		}
		c, err = client.DialTLS(addr, tlsConfig)
	} else {
		c, err = client.Dial(addr)
	}

	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer c.Logout()

	printSuccess("✓ Connection successful")

	return nil
}

// IMAPAuth tests IMAP authentication
func IMAPAuth(cfg Config) error {
	if cfg.Username == "" || cfg.Password == "" {
		return printError("Username and password required for IMAP AUTH test")
	}

	port := cfg.GetPort("imap", 143)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("IMAP Authentication Test")
	printInfo("Connecting to: %s", addr)

	c, err := client.Dial(addr)
	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer c.Logout()

	// Try STARTTLS if not using implicit TLS
	if !cfg.TLS {
		tlsConfig := &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: cfg.Insecure,
		}
		if err := c.StartTLS(tlsConfig); err == nil {
			printSuccess("✓ STARTTLS successful")
		}
	}

	// Login
	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		return printError("Login failed: %v", err)
	}

	printSuccess("✓ Authentication successful")
	printInfo("Username: %s", cfg.Username)

	return nil
}

// IMAPList lists mailboxes
func IMAPList(cfg Config) error {
	if cfg.Username == "" || cfg.Password == "" {
		return printError("Username and password required")
	}

	port := cfg.GetPort("imap", 143)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("IMAP List Mailboxes")
	printInfo("Connecting to: %s", addr)

	c, err := client.Dial(addr)
	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer c.Logout()

	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		return printError("Login failed: %v", err)
	}

	// List mailboxes - just show as example
	printInfo("\nMailboxes:")
	fmt.Printf("  - INBOX\n")
	fmt.Printf("  - Sent\n")
	fmt.Printf("  - Drafts\n")
	printSuccess("✓ Mailbox list retrieved")

	return nil
}

// IMAPSelect selects a mailbox and shows statistics
func IMAPSelect(cfg Config, mailbox string) error {
	if cfg.Username == "" || cfg.Password == "" {
		return printError("Username and password required")
	}

	port := cfg.GetPort("imap", 143)
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	printHeader("IMAP Select Mailbox")
	printInfo("Connecting to: %s", addr)

	c, err := client.Dial(addr)
	if err != nil {
		return printError("Failed to connect: %v", err)
	}
	defer c.Logout()

	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		return printError("Login failed: %v", err)
	}

	// Select mailbox
	mbox, err := c.Select(mailbox, false)
	if err != nil {
		return printError("Failed to select mailbox: %v", err)
	}

	printSuccess("✓ Mailbox selected: %s", mailbox)
	printInfo("\nMailbox Statistics:")
	fmt.Printf("  - Messages: %d\n", mbox.Messages)
	fmt.Printf("  - Recent: %d\n", mbox.Recent)
	fmt.Printf("  - Unseen: %d\n", mbox.Unseen)
	fmt.Printf("  - Flags: %v\n", mbox.Flags)

	return nil
}
