package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/confadmin"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func apikeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apikeys",
		Short: "API key management",
		Long:  "Create, list, and revoke API keys for programmatic access",
	}

	cmd.AddCommand(apikeysListCmd())
	cmd.AddCommand(apikeysCreateCmd())
	cmd.AddCommand(apikeysRevokeCmd())

	return cmd
}

func apikeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List API keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigRaw()
			if err != nil {
				return err
			}

			apiSection, _ := cfg["api"].(map[string]interface{})
			if apiSection == nil {
				fmt.Println("No API keys configured")
				return nil
			}

			keys, _ := apiSection["api_keys"].([]interface{})
			if len(keys) == 0 {
				fmt.Println("No API keys configured")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tKEY (REDACTED)\tPERMISSIONS\tDESCRIPTION")
			fmt.Fprintln(w, "----\t--------------\tWITH\t-----------")
			for _, k := range keys {
				entry, _ := k.(map[string]interface{})
				if entry == nil {
					continue
				}
				name, _ := entry["name"].(string)
				key, _ := entry["key"].(string)
				desc, _ := entry["description"].(string)

				var perms []string
				if p, ok := entry["permissions"].([]interface{}); ok {
					for _, perm := range p {
						if s, ok := perm.(string); ok {
							perms = append(perms, s)
						}
					}
				}

				redacted := redactKey(key)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, redacted, strings.Join(perms, ","), desc)
			}
			w.Flush()
			return nil
		},
	}
}

func apikeysCreateCmd() *cobra.Command {
	var permissions []string
	var description string
	var expiry string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Generate cryptographically secure key
			keyBytes := make([]byte, 32)
			if _, err := rand.Read(keyBytes); err != nil {
				return fmt.Errorf("failed to generate key: %w", err)
			}
			key := "ads_" + hex.EncodeToString(keyBytes)

			cfg, err := loadConfigRaw()
			if err != nil {
				return err
			}

			// Ensure api section exists
			apiSection, _ := cfg["api"].(map[string]interface{})
			if apiSection == nil {
				apiSection = make(map[string]interface{})
				cfg["api"] = apiSection
			}

			// Check for duplicate name
			keys, _ := apiSection["api_keys"].([]interface{})
			for _, k := range keys {
				if entry, ok := k.(map[string]interface{}); ok {
					if entry["name"] == name {
						return fmt.Errorf("API key with name %q already exists", name)
					}
				}
			}

			entry := map[string]interface{}{
				"name":        name,
				"key":         key,
				"permissions": permissions,
				"description": description,
			}
			if expiry != "" {
				expires, err := time.Parse("2006-01-02", expiry)
				if err != nil {
					return fmt.Errorf("expiry must be YYYY-MM-DD: %w", err)
				}
				entry["expires_at"] = expires.UTC()
			}

			keys = append(keys, entry)
			apiSection["api_keys"] = keys

			if err := saveConfigRaw(cfg); err != nil {
				return err
			}

			fmt.Printf("✓ API key created\n")
			fmt.Printf("  Name:        %s\n", name)
			fmt.Printf("  Key:         %s\n", key)
			fmt.Printf("  Permissions: %s\n", strings.Join(permissions, ", "))
			if description != "" {
				fmt.Printf("  Description: %s\n", description)
			}
			fmt.Println("\nStore this key securely. Reload configuration and verify access before ending this session.")
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&permissions, "permissions", "p", []string{"all"}, "Comma-separated permissions (e.g. queue:read,policy:read or all)")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Human-readable description")
	cmd.Flags().StringVar(&expiry, "expires", "", "Expiry date (e.g. 2026-12-31)")

	return cmd
}

func apikeysRevokeCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "revoke <name>",
		Short: "Revoke an API key by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if !force {
				fmt.Printf("Revoke API key %q? (yes/no): ", name)
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "yes" {
					fmt.Println("Aborted")
					return nil
				}
			}

			cfg, err := loadConfigRaw()
			if err != nil {
				return err
			}

			apiSection, _ := cfg["api"].(map[string]interface{})
			if apiSection == nil {
				return fmt.Errorf("no API keys configured")
			}

			keys, _ := apiSection["api_keys"].([]interface{})
			updated := make([]interface{}, 0, len(keys))
			found := false
			for _, k := range keys {
				entry, _ := k.(map[string]interface{})
				if entry != nil && entry["name"] == name {
					found = true
					continue
				}
				updated = append(updated, k)
			}

			if !found {
				return fmt.Errorf("API key %q not found", name)
			}

			apiSection["api_keys"] = updated
			if err := saveConfigRaw(cfg); err != nil {
				return err
			}

			fmt.Printf("✓ API key %q revoked\n", name)
			fmt.Println("Note: Restart the server for changes to take effect.")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}

// loadConfigRaw reads config.yaml as a raw map to preserve unknown fields.
var configOriginal []byte

func loadConfigRaw() (map[string]interface{}, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config %s: %w", configFile, err)
	}
	configOriginal = append([]byte(nil), data...)
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	if cfg == nil {
		cfg = make(map[string]interface{})
	}
	return cfg, nil
}

// saveConfigRaw writes a raw config map back to config.yaml.
func saveConfigRaw(cfg map[string]interface{}) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	_, err = confadmin.Replace(configFile, configOriginal, data, func(p string) error { return confadmin.ValidateConfig(p, false) })
	return err
}

// redactKey shows only the prefix and last 4 chars.
func redactKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:8] + "..." + key[len(key)-4:]
}
