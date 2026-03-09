package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func queueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Queue management and visibility",
		Long:  "Manage mail queues, view pending messages, retry failed deliveries",
	}

	cmd.AddCommand(queueStatsCmd())
	cmd.AddCommand(queueListCmd())
	cmd.AddCommand(queueRetryCmd())
	cmd.AddCommand(queuePurgeCmd())
	cmd.AddCommand(queueInspectCmd())
	cmd.AddCommand(queueDLQCmd())

	return cmd
}

func queueStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show queue statistics",
		Long:  "Display current queue statistics including pending, processing, and failed messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := apiRequest("GET", "/api/v1/queue/stats", nil)
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

			var stats map[string]interface{}
			if err := json.Unmarshal(body, &stats); err != nil {
				return err
			}

			fmt.Println("Queue Statistics")
			fmt.Println("================")
			fmt.Printf("Pending:     %v\n", getIntOrZero(stats, "pending"))
			fmt.Printf("Processing:  %v\n", getIntOrZero(stats, "processing"))
			fmt.Printf("Completed:   %v\n", getIntOrZero(stats, "completed"))
			fmt.Printf("Failed:      %v\n", getIntOrZero(stats, "failed"))
			fmt.Printf("DLQ:         %v\n", getIntOrZero(stats, "dlq"))
			fmt.Printf("Total:       %v\n", getIntOrZero(stats, "total"))

			return nil
		},
	}
}

func queueListCmd() *cobra.Command {
	var limit int
	var status string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List messages in queue",
		Long:  "List pending, processing, or failed messages in the queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := fmt.Sprintf("/api/v1/queue/pending?limit=%d", limit)
			if status != "" {
				url += "&status=" + status
			}

			resp, err := apiRequest("GET", url, nil)
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

			var result map[string]interface{}
			if err := json.Unmarshal(body, &result); err != nil {
				return err
			}

			messages, ok := result["messages"].([]interface{})
			if !ok || len(messages) == 0 {
				fmt.Println("No messages in queue")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tFROM\tTO\tSTATUS\tRETRIES\tAGE")
			fmt.Fprintln(w, "--\t----\t--\t------\t-------\t---")

			for _, m := range messages {
				msg := m.(map[string]interface{})
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%s\n",
					getString(msg, "id"),
					getString(msg, "from"),
					getString(msg, "to"),
					getString(msg, "status"),
					getIntOrZero(msg, "retries"),
					getString(msg, "age"),
				)
			}

			w.Flush()
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of messages to show")
	cmd.Flags().StringVarP(&status, "status", "s", "", "Filter by status (pending, processing, failed)")

	return cmd
}

func queueRetryCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "retry [message-id]",
		Short: "Retry failed message delivery",
		Long:  "Retry delivery of a specific message or all failed messages",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var url string
			if all {
				url = "/api/v1/dlq/retry/all"
			} else if len(args) > 0 {
				url = "/api/v1/dlq/retry/" + args[0]
			} else {
				return fmt.Errorf("specify message-id or use --all flag")
			}

			resp, err := apiRequest("POST", url, nil)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				if all {
					fmt.Println("✓ Retrying all failed messages")
				} else {
					fmt.Printf("✓ Retrying message %s\n", args[0])
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Retry all failed messages")

	return cmd
}

func queuePurgeCmd() *cobra.Command {
	var force bool
	var older string

	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Purge messages from queue",
		Long:  "Remove messages from the queue (use with caution)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				fmt.Print("WARNING: This will permanently delete messages. Are you sure? (yes/no): ")
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "yes" {
					fmt.Println("Aborted")
					return nil
				}
			}

			url := "/api/v1/queue/purge"
			if older != "" {
				url += "?older=" + older
			}

			resp, err := apiRequest("POST", url, nil)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			fmt.Println("✓ Queue purged")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&older, "older", "", "Purge messages older than duration (e.g., 24h, 7d)")

	return cmd
}

func queueInspectCmd() *cobra.Command {
	var showHeaders bool
	var showBody bool

	cmd := &cobra.Command{
		Use:   "inspect <message-id>",
		Short: "Inspect a specific message",
		Long:  "View detailed information about a message in the queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := fmt.Sprintf("/api/v1/message/%s", args[0])

			resp, err := apiRequest("GET", url, nil)
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

			var msg map[string]interface{}
			if err := json.Unmarshal(body, &msg); err != nil {
				return err
			}

			fmt.Printf("Message ID:   %s\n", getString(msg, "id"))
			fmt.Printf("From:         %s\n", getString(msg, "from"))
			fmt.Printf("To:           %s\n", getString(msg, "to"))
			fmt.Printf("Subject:      %s\n", getString(msg, "subject"))
			fmt.Printf("Status:       %s\n", getString(msg, "status"))
			fmt.Printf("Retries:      %v\n", getIntOrZero(msg, "retries"))
			fmt.Printf("Created:      %s\n", getString(msg, "created_at"))
			fmt.Printf("Last Attempt: %s\n", getString(msg, "last_attempt"))
			fmt.Printf("Size:         %v bytes\n", getIntOrZero(msg, "size"))

			if showHeaders {
				fmt.Println("\nHeaders:")
				if headers, ok := msg["headers"].(map[string]interface{}); ok {
					for k, v := range headers {
						fmt.Printf("  %s: %v\n", k, v)
					}
				}
			}

			if showBody {
				fmt.Println("\nBody:")
				fmt.Println(getString(msg, "body"))
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&showHeaders, "headers", false, "Show message headers")
	cmd.Flags().BoolVar(&showBody, "body", false, "Show message body")

	return cmd
}

func queueDLQCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dlq",
		Short: "Dead letter queue management",
		Long:  "Manage messages in the dead letter queue",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List messages in DLQ",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := apiRequest("GET", "/api/v1/dlq/list", nil)
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

			var result map[string]interface{}
			if err := json.Unmarshal(body, &result); err != nil {
				return err
			}

			messages, ok := result["messages"].([]interface{})
			if !ok || len(messages) == 0 {
				fmt.Println("No messages in DLQ")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tFROM\tTO\tERROR\tRETRIES\tAGE")
			fmt.Fprintln(w, "--\t----\t--\t-----\t-------\t---")

			for _, m := range messages {
				msg := m.(map[string]interface{})
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%s\n",
					getString(msg, "id"),
					getString(msg, "from"),
					getString(msg, "to"),
					truncate(getString(msg, "error"), 40),
					getIntOrZero(msg, "retries"),
					getString(msg, "age"),
				)
			}

			w.Flush()
			return nil
		},
	})

	return cmd
}

// Helper functions

func apiRequest(method, path string, body io.Reader) (*http.Response, error) {
	url := apiEndpoint + path

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(apiUser, apiPassword)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getIntOrZero(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		}
	}
	return 0
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
