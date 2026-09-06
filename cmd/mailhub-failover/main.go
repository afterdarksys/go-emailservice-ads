package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/failover"
	"os"
	"time"
)

func main() {
	planPath := flag.String("plan", "", "JSON activation plan")
	timeout := flag.Duration("timeout", 5*time.Minute, "activation deadline")
	flag.Parse()
	if *planPath == "" {
		fmt.Fprintln(os.Stderr, "--plan is required")
		os.Exit(2)
	}
	raw, err := os.ReadFile(*planPath)
	var plan failover.Plan
	if err == nil {
		err = json.Unmarshal(raw, &plan)
	}
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		err = failover.Activate(ctx, plan)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Fenced standby activation verified")
}
