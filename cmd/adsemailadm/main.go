package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	// Global flags
	apiEndpoint string
	apiUser     string
	apiPassword string
	configFile  string
	verbose     bool
	jsonOutput  bool

	// Logger
	logger *zap.Logger
)

func main() {
	// Initialize logger
	var err error
	if verbose {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	rootCmd := &cobra.Command{
		Use:   "adsemailadm",
		Short: "Admin utility for go-emailservice-ads",
		Long: `adsemailadm - Administrative utility for go-emailservice-ads

A comprehensive CLI tool for managing your email service:
  • Queue management and visibility
  • Monitoring and statistics
  • Mailbox and delivery management
  • TLS/SSL certificate management
  • Directory service configuration
  • Policy management
  • System configuration

Examples:
  adsemailadm queue stats              # Show queue statistics
  adsemailadm mailbox list              # List all mailboxes
  adsemailadm policy test spam.star     # Test a policy
  adsemailadm tls status                # Check TLS configuration
  adsemailadm monitor realtime          # Real-time monitoring
`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Setup runs before every command
			if apiEndpoint == "" {
				apiEndpoint = os.Getenv("ADS_API_ENDPOINT")
				if apiEndpoint == "" {
					apiEndpoint = "http://localhost:8080"
				}
			}
			if apiUser == "" {
				apiUser = os.Getenv("ADS_API_USER")
				if apiUser == "" {
					apiUser = "admin"
				}
			}
			if apiPassword == "" {
				apiPassword = os.Getenv("ADS_API_PASSWORD")
				if apiPassword == "" {
					apiPassword = "changeme"
				}
			}
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&apiEndpoint, "api", "", "API endpoint (default: http://localhost:8080)")
	rootCmd.PersistentFlags().StringVar(&apiUser, "user", "", "API username (default: admin)")
	rootCmd.PersistentFlags().StringVar(&apiPassword, "password", "", "API password (default: changeme)")
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "config.yaml", "Config file path")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "JSON output format")

	// Add subcommands
	rootCmd.AddCommand(queueCmd())
	rootCmd.AddCommand(mailboxCmd())
	rootCmd.AddCommand(policyCmd())
	rootCmd.AddCommand(tlsCmd())
	rootCmd.AddCommand(directoryCmd())
	rootCmd.AddCommand(monitorCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(clusterCmd())
	rootCmd.AddCommand(securityCmd())
	rootCmd.AddCommand(healthCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
