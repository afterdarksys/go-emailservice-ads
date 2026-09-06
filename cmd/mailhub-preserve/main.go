package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/preservation"
	"gopkg.in/yaml.v3"
	"os"
)

func main() {
	path := flag.String("config", "", "preservation YAML configuration")
	flag.Parse()
	if err := run(*path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var cfg preservation.Config
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err = decoder.Decode(&cfg); err != nil {
		return err
	}
	receipt, err := preservation.Archive(context.Background(), cfg, preservation.AWS)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(receipt)
}
