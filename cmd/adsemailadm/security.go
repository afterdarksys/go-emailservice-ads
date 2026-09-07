package main

import "github.com/spf13/cobra"

func securityCmd() *cobra.Command {
	root := &cobra.Command{Use: "security", Short: "Operational security statistics"}
	root.AddCommand(endpointCommand("stats", "GET", "/api/v1/security/stats"), endpointCommand("dns", "GET", "/api/v1/dns/stats"), endpointCommand("greylisting", "GET", "/api/v1/greylisting/stats"))
	return root
}
