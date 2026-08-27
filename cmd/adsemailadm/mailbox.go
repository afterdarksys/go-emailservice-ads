package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

type mailboxEntry struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Enabled  bool   `json:"enabled"`
}

// mailboxAPIError turns a non-2xx API response into a readable error.
func mailboxAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := string(bytes.TrimSpace(body))
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("API error (%d): %s", resp.StatusCode, msg)
}

// generatePassword returns a 24-character password from the OS CSPRNG. The
// alphabet is exactly 64 symbols so the modulo mapping is bias-free.
func generatePassword() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
	const length = 24
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate password: %w", err)
	}
	out := make([]byte, length)
	for i, b := range raw {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}

func mailboxListCmd() *cobra.Command {
	var domain string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all mailboxes",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := apiRequest("GET", "/api/v1/mailboxes", nil)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return mailboxAPIError(resp)
			}

			var mailboxes []mailboxEntry
			if err := json.NewDecoder(resp.Body).Decode(&mailboxes); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if domain != "" {
				filtered := mailboxes[:0]
				for _, m := range mailboxes {
					if hasDomainSuffix(m.Username, domain) || hasDomainSuffix(m.Email, domain) {
						filtered = append(filtered, m)
					}
				}
				mailboxes = filtered
			}

			if jsonOutput {
				out, err := json.MarshalIndent(mailboxes, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(out))
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "USERNAME\tEMAIL\tENABLED")
			fmt.Fprintln(w, "--------\t-----\t-------")
			for _, m := range mailboxes {
				fmt.Fprintf(w, "%s\t%s\t%v\n", m.Username, m.Email, m.Enabled)
			}
			w.Flush()
			fmt.Printf("\n%d mailbox(es)\n", len(mailboxes))
			return nil
		},
	}

	cmd.Flags().StringVarP(&domain, "domain", "d", "", "Filter by domain")

	return cmd
}

func hasDomainSuffix(addr, domain string) bool {
	suffix := "@" + domain
	return len(addr) > len(suffix) && addr[len(addr)-len(suffix):] == suffix
}

func mailboxCreateCmd() *cobra.Command {
	var password string
	var email string

	cmd := &cobra.Command{
		Use:   "create <username>",
		Short: "Create a new mailbox",
		Long: `Create a new mailbox on the running server (no restart needed).

The username is normally the full address (e.g. hello@purrr.email). When
--password is omitted, a random 24-character password is generated and
printed ONCE — record it immediately.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]

			generated := false
			if password == "" {
				var err error
				password, err = generatePassword()
				if err != nil {
					return err
				}
				generated = true
			}

			payload, err := json.Marshal(map[string]string{
				"username": username,
				"password": password,
				"email":    email,
			})
			if err != nil {
				return err
			}

			resp, err := apiRequest("POST", "/api/v1/mailboxes", bytes.NewReader(payload))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusCreated {
				return mailboxAPIError(resp)
			}

			var created mailboxEntry
			if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			fmt.Printf("✓ Mailbox created: %s (email: %s)\n", created.Username, created.Email)
			if generated {
				fmt.Printf("Generated password (shown once): %s\n", password)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&password, "password", "p", "", "Mailbox password (generated when omitted)")
	cmd.Flags().StringVarP(&email, "email", "e", "", "Email address (defaults to the username)")

	return cmd
}

func mailboxDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <username>",
		Short: "Delete a mailbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]

			if !force {
				fmt.Printf("Are you sure you want to delete mailbox %s? (yes/no): ", username)
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "yes" {
					return fmt.Errorf("aborted")
				}
			}

			resp, err := apiRequest("DELETE", "/api/v1/mailboxes/"+url.PathEscape(username), nil)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return mailboxAPIError(resp)
			}

			fmt.Printf("✓ Mailbox %s deleted\n", username)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")

	return cmd
}

// The commands below are not backed by server endpoints yet. They fail
// honestly instead of printing fake success output.

func notImplemented(feature string) error {
	return fmt.Errorf("%s is not implemented by the server yet", feature)
}

func mailboxQuotaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quota",
		Short: "Manage mailbox quotas (not yet implemented)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show <email>",
		Short: "Show quota usage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("quota reporting")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set <email> <size>",
		Short: "Set mailbox quota (e.g., 5G, 500M)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("quota management")
		},
	})

	return cmd
}

func mailboxAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage email aliases (not yet implemented)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "add <alias> <target>",
		Short: "Add email alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("alias management")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <alias>",
		Short: "Remove email alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("alias management")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all aliases",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("alias management")
		},
	})

	return cmd
}

func mailboxRoutingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routing",
		Short: "Manage message routing (not yet implemented)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show <email>",
		Short: "Show routing for email address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("routing inspection")
		},
	})

	return cmd
}
