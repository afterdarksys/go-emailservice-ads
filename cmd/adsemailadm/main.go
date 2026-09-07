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
	apiKey      string // Bearer token (overrides Basic Auth when set)
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

		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if apiKey == "" {
				apiKey = os.Getenv("ADS_API_KEY")
			}
			// Setup runs before every command
			if apiEndpoint == "" {
				apiEndpoint = os.Getenv("ADS_API_ENDPOINT")
				if apiEndpoint == "" {
					apiEndpoint = "http://localhost:8080"
				}
			}
			if apiUser == "" {
				apiUser = os.Getenv("ADS_API_USER")
			}
			if apiPassword == "" {
				apiPassword = os.Getenv("ADS_API_PASSWORD")
			}
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&apiEndpoint, "api", "", "API endpoint (default: http://localhost:8080)")
	rootCmd.PersistentFlags().StringVar(&apiUser, "user", "", "API username (or ADS_API_USER)")
	rootCmd.PersistentFlags().StringVar(&apiPassword, "password", "", "API password (prefer ADS_API_PASSWORD)")
	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "API key for Bearer token auth (overrides --user/--password)")
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "config.yaml", "Config file path")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "JSON output format")

	// Add subcommands
	rootCmd.AddCommand(queueCmd(), genericAPICmd())
	rootCmd.AddCommand(mailboxCmd())
	rootCmd.AddCommand(policyCmd())
	rootCmd.AddCommand(tlsCmd())
	rootCmd.AddCommand(directoryCmd())
	rootCmd.AddCommand(monitorCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(clusterCmd())
	rootCmd.AddCommand(securityCmd())
	rootCmd.AddCommand(healthCmd())
	rootCmd.AddCommand(apikeysCmd())
	rootCmd.AddCommand(sieveCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
