package main

import (
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/confadmin"
	"os"
)

func main() {
	if err := confadmin.NewCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
