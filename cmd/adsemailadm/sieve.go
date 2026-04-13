package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

const sieveDir = "data/sieve"

func sieveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sieve",
		Short: "Per-user Sieve script management",
		Long:  "Manage per-user Sieve mail filtering scripts (stored in " + sieveDir + "/)",
	}

	cmd.AddCommand(sieveListCmd())
	cmd.AddCommand(sieveShowCmd())
	cmd.AddCommand(sieveUploadCmd())
	cmd.AddCommand(sieveDeleteCmd())
	cmd.AddCommand(sieveEditCmd())

	return cmd
}

func sieveListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List users with Sieve scripts",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := os.ReadDir(sieveDir)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Printf("No Sieve scripts found (directory %s does not exist)\n", sieveDir)
					return nil
				}
				return fmt.Errorf("failed to read sieve dir: %w", err)
			}

			scripts := make([]os.DirEntry, 0)
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".sieve") {
					scripts = append(scripts, e)
				}
			}

			if len(scripts) == 0 {
				fmt.Println("No Sieve scripts configured")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "USERNAME\tSIZE\tPATH")
			fmt.Fprintln(w, "--------\t----\t----")
			for _, e := range scripts {
				username := strings.TrimSuffix(e.Name(), ".sieve")
				info, _ := e.Info()
				size := int64(0)
				if info != nil {
					size = info.Size()
				}
				fmt.Fprintf(w, "%s\t%d bytes\t%s\n", username, size, filepath.Join(sieveDir, e.Name()))
			}
			w.Flush()
			return nil
		},
	}
}

func sieveShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <username>",
		Short: "Show a user's Sieve script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := sievePath(args[0])
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no Sieve script for user %q", args[0])
				}
				return err
			}
			fmt.Printf("# %s\n", path)
			fmt.Println(string(data))
			return nil
		},
	}
}

func sieveUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <username> <script-file>",
		Short: "Upload a Sieve script for a user",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			username, srcFile := args[0], args[1]

			src, err := os.Open(srcFile)
			if err != nil {
				return fmt.Errorf("failed to open %s: %w", srcFile, err)
			}
			defer src.Close()

			data, err := io.ReadAll(src)
			if err != nil {
				return err
			}

			if err := os.MkdirAll(sieveDir, 0755); err != nil {
				return fmt.Errorf("failed to create sieve dir: %w", err)
			}

			dest := sievePath(username)
			if err := os.WriteFile(dest, data, 0644); err != nil {
				return fmt.Errorf("failed to write script: %w", err)
			}

			fmt.Printf("✓ Sieve script uploaded for user %q → %s (%d bytes)\n", username, dest, len(data))
			return nil
		},
	}
}

func sieveDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <username>",
		Short: "Delete a user's Sieve script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			path := sievePath(username)

			if _, err := os.Stat(path); os.IsNotExist(err) {
				return fmt.Errorf("no Sieve script for user %q", username)
			}

			if !force {
				fmt.Printf("Delete Sieve script for %q? (yes/no): ", username)
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "yes" {
					fmt.Println("Aborted")
					return nil
				}
			}

			if err := os.Remove(path); err != nil {
				return err
			}

			fmt.Printf("✓ Sieve script deleted for user %q\n", username)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}

func sieveEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit <username> <script-content>",
		Short: "Write an inline Sieve script for a user",
		Long: `Write a Sieve script inline. For complex scripts, prefer 'sieve upload'.

Example:
  adsemailadm sieve edit alice 'require ["fileinto"]; if header :contains "Subject" "SPAM" { fileinto "Junk"; }'`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			username, script := args[0], args[1]

			if err := os.MkdirAll(sieveDir, 0755); err != nil {
				return fmt.Errorf("failed to create sieve dir: %w", err)
			}

			dest := sievePath(username)
			if err := os.WriteFile(dest, []byte(script), 0644); err != nil {
				return err
			}

			fmt.Printf("✓ Sieve script saved for user %q → %s\n", username, dest)
			return nil
		},
	}
}

func sievePath(username string) string {
	return filepath.Join(sieveDir, username+".sieve")
}
