package confadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/spf13/cobra"
)

func printJSON(cmd *cobra.Command, v interface{}) error {
	e := json.NewEncoder(cmd.OutOrStdout())
	e.SetIndent("", "  ")
	return e.Encode(v)
}

// ConfigCommand is shared by both administration entry points.
func ConfigCommand(configPath *string) *cobra.Command {
	root := &cobra.Command{Use: "config", Short: "Validate and edit platform configuration"}
	var tlsFiles bool
	check := &cobra.Command{Use: "check [file]", Aliases: []string{"validate"}, Args: cobra.MaximumNArgs(1), Short: "Check schema and settings; optionally verify certificate files", RunE: func(cmd *cobra.Command, a []string) error {
		p := *configPath
		if len(a) > 0 {
			p = a[0]
		}
		if e := ValidateConfig(p, tlsFiles); e != nil {
			return e
		}
		cmd.Println("Configuration settings valid")
		return nil
	}}
	check.Flags().BoolVar(&tlsFiles, "tls", false, "Also load TLS keys and check certificate validity dates")
	root.AddCommand(check, &cobra.Command{Use: "create FILE", Args: cobra.ExactArgs(1), Short: "Create secure defaults; provision certificates and accounts before starting", RunE: func(cmd *cobra.Command, a []string) error { return Create(a[0], []byte(config.DefaultDocument)) }})
	for _, op := range []string{"show", "format", "set", "edit"} {
		root.AddCommand(documentCommand(op, true, configPath))
	}
	return root
}
func documentCommand(op string, platform bool, configPath *string) *cobra.Command {
	var apply bool
	var valueFile, editor string
	use := op + " FILE"
	count := 1
	if platform {
		use = op
		count = 0
	}
	if op == "set" {
		use += " POINTER"
		count++
	}
	cmd := &cobra.Command{Use: use, Args: cobra.ExactArgs(count), Short: op + " a document (writes require --apply)", RunE: func(cmd *cobra.Command, a []string) error {
		path := *configPath
		offset := 0
		if !platform {
			path = a[0]
			offset = 1
		}
		old, e := Read(path)
		if e != nil {
			return e
		}
		// An editor must be able to repair malformed input; validation still
		// applies to the completed candidate before any replacement.
		if op != "edit" {
			n, err := Parse(path, old)
			if err != nil {
				return err
			}
			if op == "show" {
				cmd.Print(string(Redacted(n)))
				return nil
			}
		}
		var next []byte
		switch op {
		case "format":
			next, e = Format(path, old)
		case "set":
			var value []byte
			value, e = Read(valueFile)
			if e == nil {
				next, e = Set(path, old, a[offset], value)
			}
		case "edit":
			if editor == "" {
				return fmt.Errorf("--editor must name an executable (no shell arguments)")
			}
			f, err := os.CreateTemp(filepath.Dir(path), ".gemsads-edit-*")
			if err != nil {
				return err
			}
			defer os.Remove(f.Name())
			_, e = f.Write(old)
			ce := f.Close()
			if e == nil {
				e = ce
			}
			if e != nil {
				return e
			}
			process := exec.CommandContext(cmd.Context(), editor, f.Name())
			process.Stdin = os.Stdin
			process.Stdout = cmd.OutOrStdout()
			process.Stderr = cmd.ErrOrStderr()
			if e = process.Run(); e != nil {
				return e
			}
			next, e = Read(f.Name())
		}
		if e != nil {
			return e
		}
		validate := func(candidate string) error {
			b, e := Read(candidate)
			if e != nil {
				return e
			}
			if _, e = Parse(path, b); e != nil {
				return e
			}
			if platform {
				return ValidateConfig(candidate, false)
			}
			return nil
		}
		// Validate dry runs using the same candidate validation as applied changes.
		f, e := os.CreateTemp(filepath.Dir(path), ".gemsads-check-*")
		if e != nil {
			return e
		}
		defer os.Remove(f.Name())
		_, e = f.Write(next)
		ce := f.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return e
		}
		if e = validate(f.Name()); e != nil {
			return e
		}
		if !apply {
			cmd.Println("Candidate valid; no change written. Use --apply to save with a backup.")
			return nil
		}
		if !platform {
			lock, e := Offline(*configPath)
			if e != nil {
				return e
			}
			defer lock.Close()
		}
		backup, e := Replace(path, old, next, validate)
		if e != nil {
			return e
		}
		cmd.Printf("Saved; backup: %s\n", backup)
		return nil
	}}
	if op != "show" {
		cmd.Flags().BoolVar(&apply, "apply", false, "Save validated change with a private backup")
	}
	if op == "set" {
		cmd.Flags().StringVar(&valueFile, "value-file", "", "File containing one JSON/YAML value")
		cmd.MarkFlagRequired("value-file")
	}
	if op == "edit" {
		cmd.Flags().StringVar(&editor, "editor", os.Getenv("EDITOR"), "Editor executable; no shell expansion")
	}
	return cmd
}
func NewCommand() *cobra.Command {
	var path string
	root := &cobra.Command{Use: "gemsads-conf", Short: "Configuration and SQLite administration", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&path, "config", "config.yaml", "Deployment configuration (run from the service working directory)")
	root.AddCommand(ConfigCommand(&path))
	file := &cobra.Command{Use: "file", Short: "JSON/YAML documents; applied changes require an offline deployment"}
	for _, op := range []string{"show", "format", "set", "edit"} {
		file.AddCommand(documentCommand(op, false, &path))
	}
	file.AddCommand(&cobra.Command{Use: "check FILE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		b, e := Read(a[0])
		if e != nil {
			return e
		}
		_, e = Parse(a[0], b)
		return e
	}})
	var contentFile string
	createFile := &cobra.Command{Use: "create FILE", Args: cobra.ExactArgs(1), Short: "Create a new JSON/YAML document from validated content", RunE: func(cmd *cobra.Command, a []string) error {
		b, e := Read(contentFile)
		if e != nil {
			return e
		}
		if _, e = Parse(a[0], b); e != nil {
			return e
		}
		return Create(a[0], b)
	}}
	createFile.Flags().StringVar(&contentFile, "content-file", "", "Source content file")
	createFile.MarkFlagRequired("content-file")
	file.AddCommand(createFile)
	root.AddCommand(file, databaseCommand(&path))
	root.AddCommand(&cobra.Command{Use: "doctor", Short: "Check configuration, TLS files and SQLite databases under data_dir", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, a []string) error {
		if e := ValidateConfig(path, true); e != nil {
			return e
		}
		c, e := config.LoadConfig(path)
		if e != nil {
			return e
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		count := 0
		e = filepath.WalkDir(c.Platform.DataDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() || !strings.EqualFold(filepath.Ext(p), ".db") {
				return nil
			}
			count++
			db, e := OpenDB(p, false)
			if e != nil {
				return e
			}
			e = CheckDB(ctx, db)
			db.Close()
			if e == nil {
				cmd.Printf("SQLite checks passed: %s\n", p)
			}
			return e
		})
		if e != nil {
			return e
		}
		cmd.Printf("Configuration/TLS checks passed; %d SQLite files checked. External databases and live service readiness were not checked.\n", count)
		return nil
	}})
	var address, from, to, ca string
	var startTLS bool
	relay := &cobra.Command{Use: "relay-check", Short: "Unauthenticated envelope probe; never sends message DATA", RunE: func(cmd *cobra.Command, a []string) error {
		r, e := RelayCheck(cmd.Context(), address, from, to, ca, startTLS)
		if pe := printJSON(cmd, r); pe != nil {
			return pe
		}
		if e != nil {
			return e
		}
		if r.Outcome != "rejected" {
			return fmt.Errorf("relay probe requires investigation")
		}
		return nil
	}}
	relay.Flags().StringVar(&address, "address", "127.0.0.1:587", "SMTP host:port")
	relay.Flags().StringVar(&from, "from", "", "External sender address")
	relay.Flags().StringVar(&to, "to", "", "External recipient address")
	relay.Flags().StringVar(&ca, "ca-file", "", "Trusted PEM CA for STARTTLS")
	relay.Flags().BoolVar(&startTLS, "starttls", false, "Use verified STARTTLS before probing")
	relay.MarkFlagRequired("from")
	relay.MarkFlagRequired("to")
	root.AddCommand(relay)
	return root
}
func databaseCommand(configPath *string) *cobra.Command {
	root := &cobra.Command{Use: "db", Short: "SQLite inspection and offline maintenance; no arbitrary corruption repair"}
	for _, op := range []string{"inspect", "rows", "check"} {
		op := op
		use := op + " FILE"
		n := 1
		if op == "rows" {
			use += " TABLE"
			n = 2
		}
		root.AddCommand(&cobra.Command{Use: use, Args: cobra.ExactArgs(n), RunE: func(cmd *cobra.Command, a []string) error {
			db, e := OpenDB(a[0], false)
			if e != nil {
				return e
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			if op == "check" {
				if e = CheckDB(ctx, db); e != nil {
					return e
				}
				cmd.Println("SQLite integrity and foreign keys valid")
				return nil
			}
			query := "SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY type,name"
			if op == "rows" {
				query = `SELECT * FROM "` + strings.ReplaceAll(a[1], `"`, `""`) + `" LIMIT 100`
			}
			rows, e := DBRows(ctx, db, query)
			if e != nil {
				return e
			}
			return printJSON(cmd, rows)
		}})
	}
	var output string
	backup := &cobra.Command{Use: "backup FILE", Args: cobra.ExactArgs(1), Short: "Create a consistent private SQLite snapshot", RunE: func(cmd *cobra.Command, a []string) error { return BackupDB(cmd.Context(), a[0], output) }}
	backup.Flags().StringVar(&output, "output", "", "New backup path")
	backup.MarkFlagRequired("output")
	root.AddCommand(backup)
	var schema string
	create := &cobra.Command{Use: "create FILE", Args: cobra.ExactArgs(1), Short: "Create an empty SQLite database from explicit table/index DDL", RunE: func(cmd *cobra.Command, a []string) error {
		b, e := Read(schema)
		if e != nil {
			return e
		}
		return CreateDB(cmd.Context(), a[0], string(b))
	}}
	create.Flags().StringVar(&schema, "schema-file", "", "SQL CREATE TABLE/INDEX statements (no triggers)")
	create.MarkFlagRequired("schema-file")
	root.AddCommand(create)
	for _, op := range []string{"edit", "maintain"} {
		op := op
		var apply bool
		var backup, sqlFile string
		cmd := &cobra.Command{Use: op + " FILE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
			if !apply {
				return fmt.Errorf("offline mutation requires --apply and --backup")
			}
			if backup == "" {
				return fmt.Errorf("--backup is required")
			}
			lock, e := Offline(*configPath)
			if e != nil {
				return e
			}
			defer lock.Close()
			if e = CheckManagedDB(*configPath, a[0]); e != nil {
				return e
			}
			if e = BackupDB(cmd.Context(), a[0], backup); e != nil {
				return e
			}
			cmd.Printf("Verified backup: %s\n", backup)
			if op == "maintain" {
				return MaintainDB(cmd.Context(), a[0])
			}
			b, e := Read(sqlFile)
			if e != nil {
				return e
			}
			return EditDB(cmd.Context(), a[0], string(b))
		}}
		if op == "maintain" {
			cmd.Aliases = []string{"fix", "reformat"}
			cmd.Short = "Rebuild indexes and compact a healthy database; refuses corruption"
		}
		cmd.Flags().BoolVar(&apply, "apply", false, "Apply offline mutation")
		cmd.Flags().StringVar(&backup, "backup", "", "New path for mandatory verified backup")
		if op == "edit" {
			cmd.Flags().StringVar(&sqlFile, "sql-file", "", "One INSERT, UPDATE or DELETE statement")
			cmd.MarkFlagRequired("sql-file")
		}
		root.AddCommand(cmd)
	}
	return root
}
