package main

import "github.com/spf13/cobra"

func directoryCmd() *cobra.Command {
	return &cobra.Command{Use: "directory", Short: "Directory provisioning uses configured LDAP or the SCIM API", RunE: func(*cobra.Command, []string) error {
		return notImplemented("directory sync CLI; use configured LDAP authentication or documented SCIM endpoints")
	}}
}
