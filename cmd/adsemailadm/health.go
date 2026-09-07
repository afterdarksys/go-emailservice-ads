package main

import "github.com/spf13/cobra"

func healthCmd() *cobra.Command {
	return &cobra.Command{Use: "health", Args: cobra.NoArgs, Short: "Check live health and readiness (both must succeed)", RunE: func(cmd *cobra.Command, a []string) error {
		if e := printResponse(cmd, "GET", "/health", nil); e != nil {
			return e
		}
		return printResponse(cmd, "GET", "/ready", nil)
	}}
}
