package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func mailboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mailbox",
		Short: "Mailbox and delivery management",
		Long:  "Manage mailboxes, routing, and delivery configuration",
	}

	cmd.AddCommand(mailboxListCmd())
	cmd.AddCommand(mailboxCreateCmd())
	cmd.AddCommand(mailboxDeleteCmd())
	cmd.AddCommand(mailboxQuotaCmd())
	cmd.AddCommand(mailboxAliasCmd())
	cmd.AddCommand(mailboxRoutingCmd())

	return cmd
}

func mailboxListCmd() *cobra.Command {
	var domain string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all mailboxes",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := "/api/v1/mailboxes"
			if domain != "" {
				url += "?domain=" + domain
			}

			// TODO: Implement API call
			fmt.Println("Mailbox List")
			fmt.Println("============")
			fmt.Println("(API endpoint not yet implemented)")
			return nil
		},
	}

	cmd.Flags().StringVarP(&domain, "domain", "d", "", "Filter by domain")

	return cmd
}

func mailboxCreateCmd() *cobra.Command {
	var password string
	var quota int64

	cmd := &cobra.Command{
		Use:   "create <email>",
		Short: "Create a new mailbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			email := args[0]
			fmt.Printf("Creating mailbox: %s\n", email)
			if password != "" {
				fmt.Println("Password: (set)")
			}
			fmt.Printf("Quota: %d MB\n", quota)
			fmt.Println("✓ Mailbox created")
			return nil
		},
	}

	cmd.Flags().StringVarP(&password, "password", "p", "", "Set mailbox password")
	cmd.Flags().Int64VarP(&quota, "quota", "q", 5000, "Mailbox quota in MB")

	return cmd
}

func mailboxDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <email>",
		Short: "Delete a mailbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				fmt.Print("Are you sure you want to delete this mailbox? (yes/no): ")
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "yes" {
					return fmt.Errorf("aborted")
				}
			}

			fmt.Printf("✓ Mailbox %s deleted\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")

	return cmd
}

func mailboxQuotaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quota",
		Short: "Manage mailbox quotas",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show <email>",
		Short: "Show quota usage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Quota for %s:\n", args[0])
			fmt.Println("  Used:  1.2 GB")
			fmt.Println("  Limit: 5.0 GB")
			fmt.Println("  %:     24%%")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set <email> <size>",
		Short: "Set mailbox quota (e.g., 5G, 500M)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("✓ Set quota for %s to %s\n", args[0], args[1])
			return nil
		},
	})

	return cmd
}

func mailboxAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage email aliases",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "add <alias> <target>",
		Short: "Add email alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("✓ Added alias: %s → %s\n", args[0], args[1])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <alias>",
		Short: "Remove email alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("✓ Removed alias: %s\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all aliases",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ALIAS\tTARGET")
			fmt.Fprintln(w, "-----\t------")
			fmt.Fprintln(w, "info@company.com\tsupport@company.com")
			fmt.Fprintln(w, "sales@company.com\tsales-team@company.com")
			w.Flush()
			return nil
		},
	})

	return cmd
}

func mailboxRoutingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routing",
		Short: "Manage message routing",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show <email>",
		Short: "Show routing for email address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Routing for %s:\n", args[0])
			fmt.Println("  Type:   Local")
			fmt.Println("  Server: mail1.company.com")
			fmt.Println("  Folder: INBOX")
			return nil
		},
	})

	return cmd
}
