package main

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/confadmin"
	"github.com/spf13/cobra"
)

func tlsCmd() *cobra.Command {
	root := &cobra.Command{Use: "tls", Short: "Verify configured TLS files"}
	root.AddCommand(&cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, a []string) error {
		if e := confadmin.ValidateConfig(configFile, true); e != nil {
			return e
		}
		cmd.Println("Configured key pairs and certificate dates valid")
		return nil
	}})
	return root
}
