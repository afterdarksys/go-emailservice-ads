package main

import (
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/confadmin"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"github.com/spf13/cobra"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func sieveCmd() *cobra.Command {
	var dir string
	root := &cobra.Command{Use: "sieve", Short: "Validated local Sieve scripts in the configured data directory", PersistentPreRunE: func(cmd *cobra.Command, a []string) error {
		c, e := config.LoadConfig(configFile)
		if e != nil {
			return e
		}
		dir = filepath.Join(c.Platform.DataDir, "sieve")
		return nil
	}}
	root.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, a []string) error {
		entries, e := os.ReadDir(dir)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".sieve") {
				name, e := url.PathUnescape(strings.TrimSuffix(entry.Name(), ".sieve"))
				if e != nil {
					return e
				}
				cmd.Println(name)
			}
		}
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "show USER", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		b, e := confadmin.Read(filepath.Join(dir, url.PathEscape(a[0])+".sieve"))
		if e != nil {
			return e
		}
		cmd.Print(string(b))
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "upload USER FILE", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, a []string) error {
		if a[0] == "" {
			return fmt.Errorf("user required")
		}
		b, e := confadmin.Read(a[1])
		if e != nil {
			return e
		}
		if e = policy.ValidateSieve(string(b)); e != nil {
			return e
		}
		if e = os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		dest := filepath.Join(dir, url.PathEscape(a[0])+".sieve")
		old, e := confadmin.Read(dest)
		if os.IsNotExist(e) {
			return confadmin.Create(dest, b)
		}
		if e != nil {
			return e
		}
		backup, e := confadmin.Replace(dest, old, b, func(p string) error {
			b, e := confadmin.Read(p)
			if e != nil {
				return e
			}
			return policy.ValidateSieve(string(b))
		})
		if e != nil {
			return e
		}
		cmd.Printf("Saved; backup: %s\n", backup)
		return nil
	}})
	// Uploading a validated `keep;` program is an explicit, reversible disable.
	return root
}
