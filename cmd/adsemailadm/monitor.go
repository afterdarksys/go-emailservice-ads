package main

import "github.com/spf13/cobra"

func monitorCmd() *cobra.Command {
	root := &cobra.Command{Use: "monitor", Short: "Live service responses"}
	root.AddCommand(endpointCommand("stats", "GET", "/health"), endpointCommand("metrics", "GET", "/metrics"))
	return root
}
