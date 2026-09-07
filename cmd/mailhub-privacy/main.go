package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/privacy"
	"os"
)

func main() {
	command := flag.String("operation", "inventory", "inventory, export or delete")
	config := flag.String("config", "config.yaml", "deployment configuration")
	account := flag.String("account", "", "account username")
	output := flag.String("output", "", "new export ZIP or deletion case JSON")
	reason := flag.String("reason", "", "approved deletion reason")
	flag.Parse()
	s, err := privacy.Open(*config, *account)
	if err == nil {
		defer s.Close()
		switch *command {
		case "inventory":
			var inventory privacy.Inventory
			inventory, err = s.Inventory(context.Background())
			if err == nil {
				err = json.NewEncoder(os.Stdout).Encode(inventory)
			}
		case "export":
			err = s.Export(context.Background(), *output)
		case "delete":
			err = s.Delete(context.Background(), *output, *reason)
		default:
			err = fmt.Errorf("unknown operation")
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
