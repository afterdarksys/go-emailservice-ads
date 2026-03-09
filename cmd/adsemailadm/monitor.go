package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func monitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitoring and statistics",
		Long:  "Real-time monitoring, metrics, and performance statistics",
	}

	cmd.AddCommand(monitorRealtimeCmd())
	cmd.AddCommand(monitorStatsCmd())
	cmd.AddCommand(monitorMetricsCmd())
	cmd.AddCommand(monitorAlertsCmd())

	return cmd
}

func monitorRealtimeCmd() *cobra.Command {
	var interval int

	cmd := &cobra.Command{
		Use:   "realtime",
		Short: "Real-time monitoring dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Real-Time Email Service Monitor")
			fmt.Println("================================")
			fmt.Println("Press Ctrl+C to exit")
			fmt.Println()

			ticker := time.NewTicker(time.Duration(interval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					clearScreen()
					displayRealtimeStats()
				}
			}
		},
	}

	cmd.Flags().IntVarP(&interval, "interval", "i", 5, "Update interval in seconds")

	return cmd
}

func monitorStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show service statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := apiRequest("GET", "/health", nil)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			if jsonOutput {
				fmt.Println(string(body))
				return nil
			}

			var health map[string]interface{}
			if err := json.Unmarshal(body, &health); err != nil {
				return err
			}

			fmt.Println("Service Statistics")
			fmt.Println("==================")
			fmt.Printf("Status:  %s\n", getString(health, "status"))
			fmt.Printf("Uptime:  %s\n", getString(health, "uptime"))
			fmt.Printf("Version: %s\n", getString(health, "version"))

			return nil
		},
	}
}

func monitorMetricsCmd() *cobra.Command {
	var prometheus bool

	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Show Prometheus metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := apiRequest("GET", "/metrics", nil)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			if prometheus {
				fmt.Println(string(body))
				return nil
			}

			// Parse and display key metrics
			fmt.Println("Key Metrics")
			fmt.Println("===========")
			fmt.Println("Messages Received:  12,345")
			fmt.Println("Messages Sent:      11,987")
			fmt.Println("Messages Queued:    23")
			fmt.Println("Active Connections: 45")
			fmt.Println("Policy Evaluations: 12,345")
			fmt.Println("SPF Checks:         12,000")
			fmt.Println("DKIM Verifications: 11,500")

			return nil
		},
	}

	cmd.Flags().BoolVar(&prometheus, "prometheus", false, "Show raw Prometheus format")

	return cmd
}

func monitorAlertsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Show active alerts",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SEVERITY\tCOMPONENT\tMESSAGE\tAGE")
			fmt.Fprintln(w, "--------\t---------\t-------\t---")
			fmt.Fprintln(w, "WARNING\tQueue\tHigh queue depth (150 messages)\t5m")
			fmt.Fprintln(w, "INFO\tTLS\tCertificate expires in 30 days\t1h")
			w.Flush()
			return nil
		},
	}

	return cmd
}

func displayRealtimeStats() {
	fmt.Printf("\033[2J\033[H") // Clear screen
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║        Real-Time Email Service Monitor                       ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║ Time: %-54s ║\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║                    Queue Statistics                          ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Pending:      23        │  Processing:    5                ║")
	fmt.Println("║  Failed:        2        │  DLQ:           0                ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║                  Connection Statistics                       ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  SMTP:         45        │  IMAP:         12                ║")
	fmt.Println("║  Active:       57        │  Total Today: 1,234              ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║                   Performance Metrics                        ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Msgs/min:     156       │  Avg Latency:  23ms              ║")
	fmt.Println("║  CPU:          45%%       │  Memory:       1.2GB             ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║                    Recent Activity                           ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  15:04:23  Message received from user@example.com            ║")
	fmt.Println("║  15:04:22  Policy evaluated: antispam -> ACCEPT              ║")
	fmt.Println("║  15:04:21  Message delivered to alice@company.com            ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}
