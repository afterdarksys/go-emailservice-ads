package main

import "github.com/spf13/cobra"

func clusterCmd() *cobra.Command {
	root := &cobra.Command{Use: "cluster", Short: "HA status (failover uses mailhub-failover)"}
	root.AddCommand(endpointCommand("status", "GET", "/api/v1/ha/status"))
	return root
}
