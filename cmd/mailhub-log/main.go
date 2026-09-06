package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"os"

	"github.com/afterdarksys/go-emailservice-ads/internal/auditlog"
	"github.com/afterdarksys/go-emailservice-ads/internal/logformat"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	configPath := flag.String("config", "", "YAML configuration file; explicit flags override file values")
	input := flag.String("input", "auto", "auto, json, yaml, or syslog")
	output := flag.String("output", "json", "json, yaml, or syslog")
	path := flag.String("file", "-", "input file; - reads stdin")
	max := flag.Int64("max-bytes", 64<<20, "maximum input bytes")
	verify := flag.Bool("verify-audit", false, "verify original audit JSONL chain")
	expected := flag.String("expected-head", "", "independently retained expected audit head hash")
	flag.Parse()
	if *configPath != "" {
		options := struct {
			Input        string `yaml:"input"`
			Output       string `yaml:"output"`
			File         string `yaml:"file"`
			MaxBytes     int64  `yaml:"max_bytes"`
			VerifyAudit  bool   `yaml:"verify_audit"`
			ExpectedHead string `yaml:"expected_head"`
		}{"auto", "json", "-", 64 << 20, false, ""}
		f, err := os.Open(*configPath)
		if err != nil {
			return err
		}
		decoder := yaml.NewDecoder(io.LimitReader(f, 1<<20))
		decoder.KnownFields(true)
		err = decoder.Decode(&options)
		f.Close()
		if err != nil {
			return err
		}
		explicit := map[string]bool{}
		flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
		if !explicit["input"] {
			*input = options.Input
		}
		if !explicit["output"] {
			*output = options.Output
		}
		if !explicit["file"] {
			*path = options.File
		}
		if !explicit["max-bytes"] {
			*max = options.MaxBytes
		}
		if !explicit["verify-audit"] {
			*verify = options.VerifyAudit
		}
		if !explicit["expected-head"] {
			*expected = options.ExpectedHead
		}
	}
	if *max < 1 || *max > 1<<30 {
		return fmt.Errorf("max-bytes must be 1..1 GiB")
	}
	var r io.Reader = os.Stdin
	if *path != "-" {
		f, e := os.Open(*path)
		if e != nil {
			return e
		}
		defer f.Close()
		r = f
	}
	raw, e := io.ReadAll(io.LimitReader(r, *max+1))
	if e != nil {
		return e
	}
	if int64(len(raw)) > *max {
		return fmt.Errorf("input exceeds byte limit")
	}
	if *verify {
		seq, head, e := auditlog.Verify(bytes.NewReader(raw))
		if e != nil {
			return e
		}
		if *expected != "" && head != *expected {
			return fmt.Errorf("audit head differs from retained checkpoint")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"sequence": seq, "head": head, "verified": true})
	}
	records, format, e := logformat.Decode(raw, *input)
	if e != nil {
		return e
	}
	fmt.Fprintf(os.Stderr, "detected=%s records=%d\n", format, len(records))
	return logformat.Encode(os.Stdout, *output, records)
}
