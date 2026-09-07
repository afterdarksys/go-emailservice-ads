package main

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/confadmin"
	"github.com/spf13/cobra"
)

func configCmd() *cobra.Command {
	cmd := confadmin.ConfigCommand(&configFile)
	cmd.AddCommand(endpointCommand("reload", "POST", "/api/v1/config/reload"))
	return cmd
}
