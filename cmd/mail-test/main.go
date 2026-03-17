package main

import (
	"fmt"
	"os"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailtest"
	"github.com/spf13/cobra"
)

var (
	version = "2.3.0"

	// Global flags
	host     string
	port     int
	tls      bool
	insecure bool
	timeout  int
	verbose  bool
	output   string

	// Auth flags
	username string
	password string

	// Remote testing flags
	remote   bool
	apiURL   string
	grpcAddr string
	apiKey   string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "mail-test",
		Short: "Mail Protocol Testing and Debugging Tool",
		Long: `mail-test is a comprehensive testing and debugging tool for mail protocols.
It supports SMTP, ESMTP, IMAP, IMAPS, POP3, STARTTLS, and AfterSMTP/MailBlocks.

Tests can be run locally (direct connection) or remotely via API/gRPC.`,
		Version: version,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&host, "host", "h", "localhost", "Mail server hostname")
	rootCmd.PersistentFlags().IntVarP(&port, "port", "p", 0, "Server port (auto-detected if not specified)")
	rootCmd.PersistentFlags().BoolVar(&tls, "tls", false, "Use implicit TLS (not STARTTLS)")
	rootCmd.PersistentFlags().BoolVarP(&insecure, "insecure", "k", false, "Skip TLS certificate verification")
	rootCmd.PersistentFlags().IntVarP(&timeout, "timeout", "t", 30, "Connection timeout in seconds")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output (show protocol conversation)")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "text", "Output format: text, json, yaml")

	// Auth flags
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "", "Username for authentication")
	rootCmd.PersistentFlags().StringVar(&password, "password", "", "Password for authentication")

	// Remote testing flags
	rootCmd.PersistentFlags().BoolVar(&remote, "remote", false, "Use remote testing via API/gRPC")
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "http://localhost:8080", "API URL for remote testing")
	rootCmd.PersistentFlags().StringVar(&grpcAddr, "grpc-addr", "localhost:50051", "gRPC address for remote testing")
	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "API key for remote testing")

	// SMTP commands
	smtpCmd := &cobra.Command{
		Use:   "smtp",
		Short: "Test SMTP/ESMTP protocol",
		Long:  "Test SMTP/ESMTP server functionality including AUTH, STARTTLS, and message sending",
	}

	smtpCmd.AddCommand(
		&cobra.Command{
			Use:   "connect",
			Short: "Test SMTP connection and greeting",
			RunE:  runSMTPConnect,
		},
		&cobra.Command{
			Use:   "ehlo",
			Short: "Test EHLO and get server capabilities",
			RunE:  runSMTPEhlo,
		},
		&cobra.Command{
			Use:   "starttls",
			Short: "Test STARTTLS negotiation",
			RunE:  runSMTPStartTLS,
		},
		&cobra.Command{
			Use:   "auth",
			Short: "Test SMTP authentication",
			RunE:  runSMTPAuth,
		},
		&cobra.Command{
			Use:   "send",
			Short: "Send a test message",
			RunE:  runSMTPSend,
		},
		&cobra.Command{
			Use:   "interactive",
			Short: "Interactive SMTP session",
			RunE:  runSMTPInteractive,
		},
		&cobra.Command{
			Use:   "benchmark",
			Short: "Benchmark SMTP performance",
			RunE:  runSMTPBenchmark,
		},
	)

	// IMAP commands
	imapCmd := &cobra.Command{
		Use:   "imap",
		Short: "Test IMAP/IMAPS protocol",
		Long:  "Test IMAP/IMAPS server functionality",
	}

	imapCmd.AddCommand(
		&cobra.Command{
			Use:   "connect",
			Short: "Test IMAP connection",
			RunE:  runIMAPConnect,
		},
		&cobra.Command{
			Use:   "auth",
			Short: "Test IMAP authentication",
			RunE:  runIMAPAuth,
		},
		&cobra.Command{
			Use:   "list",
			Short: "List mailboxes",
			RunE:  runIMAPList,
		},
		&cobra.Command{
			Use:   "select <mailbox>",
			Short: "Select a mailbox",
			Args:  cobra.ExactArgs(1),
			RunE:  runIMAPSelect,
		},
	)

	// POP3 commands
	pop3Cmd := &cobra.Command{
		Use:   "pop3",
		Short: "Test POP3 protocol",
		Long:  "Test POP3 server functionality",
	}

	pop3Cmd.AddCommand(
		&cobra.Command{
			Use:   "connect",
			Short: "Test POP3 connection",
			RunE:  runPOP3Connect,
		},
		&cobra.Command{
			Use:   "auth",
			Short: "Test POP3 authentication",
			RunE:  runPOP3Auth,
		},
		&cobra.Command{
			Use:   "stat",
			Short: "Get mailbox statistics",
			RunE:  runPOP3Stat,
		},
	)

	// Diagnostic commands
	diagCmd := &cobra.Command{
		Use:   "diag",
		Short: "Run diagnostic tests",
		Long:  "Run comprehensive diagnostic tests on mail server",
	}

	diagCmd.AddCommand(
		&cobra.Command{
			Use:   "full",
			Short: "Run full diagnostic suite",
			RunE:  runDiagFull,
		},
		&cobra.Command{
			Use:   "dns",
			Short: "Test DNS records (MX, SPF, DKIM, DMARC)",
			RunE:  runDiagDNS,
		},
		&cobra.Command{
			Use:   "tls",
			Short: "Test TLS configuration",
			RunE:  runDiagTLS,
		},
		&cobra.Command{
			Use:   "auth",
			Short: "Test authentication methods",
			RunE:  runDiagAuth,
		},
		&cobra.Command{
			Use:   "deliverability",
			Short: "Test mail deliverability",
			RunE:  runDiagDeliverability,
		},
	)

	rootCmd.AddCommand(smtpCmd, imapCmd, pop3Cmd, diagCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func getConfig() mailtest.Config {
	return mailtest.Config{
		Host:     host,
		Port:     port,
		TLS:      tls,
		Insecure: insecure,
		Timeout:  timeout,
		Verbose:  verbose,
		Username: username,
		Password: password,
		Remote:   remote,
		APIURL:   apiURL,
		GRPCAddr: grpcAddr,
		APIKey:   apiKey,
	}
}

// SMTP command implementations
func runSMTPConnect(cmd *cobra.Command, args []string) error {
	return mailtest.SMTPConnect(getConfig())
}

func runSMTPEhlo(cmd *cobra.Command, args []string) error {
	return mailtest.SMTPEhlo(getConfig())
}

func runSMTPStartTLS(cmd *cobra.Command, args []string) error {
	return mailtest.SMTPStartTLS(getConfig())
}

func runSMTPAuth(cmd *cobra.Command, args []string) error {
	return mailtest.SMTPAuth(getConfig())
}

func runSMTPSend(cmd *cobra.Command, args []string) error {
	return mailtest.SMTPSend(getConfig())
}

func runSMTPInteractive(cmd *cobra.Command, args []string) error {
	return mailtest.SMTPInteractive(getConfig())
}

func runSMTPBenchmark(cmd *cobra.Command, args []string) error {
	return mailtest.SMTPBenchmark(getConfig())
}

// IMAP command implementations
func runIMAPConnect(cmd *cobra.Command, args []string) error {
	return mailtest.IMAPConnect(getConfig())
}

func runIMAPAuth(cmd *cobra.Command, args []string) error {
	return mailtest.IMAPAuth(getConfig())
}

func runIMAPList(cmd *cobra.Command, args []string) error {
	return mailtest.IMAPList(getConfig())
}

func runIMAPSelect(cmd *cobra.Command, args []string) error {
	cfg := getConfig()
	return mailtest.IMAPSelect(cfg, args[0])
}

// POP3 command implementations
func runPOP3Connect(cmd *cobra.Command, args []string) error {
	return mailtest.POP3Connect(getConfig())
}

func runPOP3Auth(cmd *cobra.Command, args []string) error {
	return mailtest.POP3Auth(getConfig())
}

func runPOP3Stat(cmd *cobra.Command, args []string) error {
	return mailtest.POP3Stat(getConfig())
}

// Diagnostic command implementations
func runDiagFull(cmd *cobra.Command, args []string) error {
	return mailtest.DiagFull(getConfig())
}

func runDiagDNS(cmd *cobra.Command, args []string) error {
	return mailtest.DiagDNS(getConfig())
}

func runDiagTLS(cmd *cobra.Command, args []string) error {
	return mailtest.DiagTLS(getConfig())
}

func runDiagAuth(cmd *cobra.Command, args []string) error {
	return mailtest.DiagAuth(getConfig())
}

func runDiagDeliverability(cmd *cobra.Command, args []string) error {
	return mailtest.DiagDeliverability(getConfig())
}
