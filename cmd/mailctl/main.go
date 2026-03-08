package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	apiURL   string
	username string
	password string
	insecure bool
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

func NewClient(baseURL, username, password string, insecure bool) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}

	return &Client{
		baseURL:  baseURL,
		username: username,
		password: password,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}
}

func (c *Client) doRequest(method, path string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	// SASL PLAIN authentication via Basic Auth
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "mailctl",
		Short: "Management CLI for go-emailservice-ads",
		Long:  "Command-line interface for managing the email service, queues, and messages",
	}

	rootCmd.PersistentFlags().StringVarP(&apiURL, "api", "a", "http://localhost:8080", "API server URL")
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "", "SASL username")
	rootCmd.PersistentFlags().StringVarP(&password, "password", "p", "", "SASL password")
	rootCmd.PersistentFlags().BoolVarP(&insecure, "insecure", "k", false, "Skip TLS verification")

	// Queue commands
	queueCmd := &cobra.Command{
		Use:   "queue",
		Short: "Manage message queues",
	}

	queueCmd.AddCommand(&cobra.Command{
		Use:   "stats",
		Short: "Show queue statistics",
		RunE:  queueStats,
	})

	queueCmd.AddCommand(&cobra.Command{
		Use:   "list [tier]",
		Short: "List pending messages",
		Args:  cobra.MaximumNArgs(1),
		RunE:  queueList,
	})

	// DLQ commands
	dlqCmd := &cobra.Command{
		Use:   "dlq",
		Short: "Manage dead letter queue",
	}

	dlqCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List DLQ messages",
		RunE:  dlqList,
	})

	dlqCmd.AddCommand(&cobra.Command{
		Use:   "retry <message-id>",
		Short: "Retry a message from DLQ",
		Args:  cobra.ExactArgs(1),
		RunE:  dlqRetry,
	})

	// Message commands
	msgCmd := &cobra.Command{
		Use:   "message",
		Short: "Manage individual messages",
	}

	msgCmd.AddCommand(&cobra.Command{
		Use:   "get <message-id>",
		Short: "Get message details",
		Args:  cobra.ExactArgs(1),
		RunE:  messageGet,
	})

	msgCmd.AddCommand(&cobra.Command{
		Use:   "delete <message-id>",
		Short: "Delete a message",
		Args:  cobra.ExactArgs(1),
		RunE:  messageDelete,
	})

	// Replication commands
	replicationCmd := &cobra.Command{
		Use:   "replication",
		Short: "Manage replication and failover",
	}

	replicationCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show replication status",
		RunE:  replicationStatus,
	})

	replicationCmd.AddCommand(&cobra.Command{
		Use:   "promote",
		Short: "Promote to primary (failover)",
		RunE:  replicationPromote,
	})

	// Health check
	rootCmd.AddCommand(&cobra.Command{
		Use:   "health",
		Short: "Check service health",
		RunE:  healthCheck,
	})

	rootCmd.AddCommand(queueCmd)
	rootCmd.AddCommand(dlqCmd)
	rootCmd.AddCommand(msgCmd)
	rootCmd.AddCommand(replicationCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func queueStats(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)
	data, err := client.doRequest("GET", "/api/v1/queue/stats", nil)
	if err != nil {
		return err
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(data, &stats); err != nil {
		return err
	}

	fmt.Println("Queue Statistics:")
	fmt.Println("─────────────────────────────────────")

	if metrics, ok := stats["metrics"].(map[string]interface{}); ok {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TIER\tENQUEUED\tPROCESSED\tFAILED")

		tiers := []string{"emergency", "msa", "int", "out", "bulk"}
		for _, tier := range tiers {
			enqueued := getMetricValue(metrics, "enqueued", tier)
			processed := getMetricValue(metrics, "processed", tier)
			failed := getMetricValue(metrics, "failed", tier)
			fmt.Fprintf(w, "%s\t%d\t%d\t%d\n", tier, enqueued, processed, failed)
		}
		w.Flush()
	}

	if storage, ok := stats["storage"].(map[string]interface{}); ok {
		fmt.Println("\nStorage Statistics:")
		fmt.Printf("  Pending:    %v\n", storage["pending"])
		fmt.Printf("  Processing: %v\n", storage["processing"])
		fmt.Printf("  DLQ:        %v\n", storage["dlq"])
		fmt.Printf("  Total:      %v\n", storage["total"])
	}

	return nil
}

func queueList(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)

	tier := ""
	if len(args) > 0 {
		tier = args[0]
	}

	path := "/api/v1/queue/pending"
	if tier != "" {
		path = fmt.Sprintf("%s?tier=%s", path, tier)
	}

	data, err := client.doRequest("GET", path, nil)
	if err != nil {
		return err
	}

	var messages []map[string]interface{}
	if err := json.Unmarshal(data, &messages); err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MESSAGE ID\tFROM\tTO\tTIER\tATTEMPTS\tCREATED")

	for _, msg := range messages {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%s\n",
			msg["message_id"],
			msg["from"],
			msg["to"],
			msg["tier"],
			msg["attempts"],
			msg["created_at"])
	}
	w.Flush()

	return nil
}

func dlqList(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)
	data, err := client.doRequest("GET", "/api/v1/dlq/list", nil)
	if err != nil {
		return err
	}

	var messages []map[string]interface{}
	if err := json.Unmarshal(data, &messages); err != nil {
		return err
	}

	fmt.Printf("Dead Letter Queue: %d messages\n\n", len(messages))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MESSAGE ID\tFROM\tERROR\tATTEMPTS\tLAST ATTEMPT")

	for _, msg := range messages {
		fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%s\n",
			msg["message_id"],
			msg["from"],
			truncate(msg["error_message"].(string), 40),
			msg["attempts"],
			msg["last_attempt"])
	}
	w.Flush()

	return nil
}

func dlqRetry(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)
	messageID := args[0]

	path := fmt.Sprintf("/api/v1/dlq/retry/%s", messageID)
	_, err := client.doRequest("POST", path, nil)
	if err != nil {
		return err
	}

	fmt.Printf("Message %s moved to pending queue for retry\n", messageID)
	return nil
}

func messageGet(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)
	messageID := args[0]

	path := fmt.Sprintf("/api/v1/message/%s", messageID)
	data, err := client.doRequest("GET", path, nil)
	if err != nil {
		return err
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}

	yamlData, err := yaml.Marshal(msg)
	if err != nil {
		return err
	}

	fmt.Println(string(yamlData))
	return nil
}

func messageDelete(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)
	messageID := args[0]

	path := fmt.Sprintf("/api/v1/message/%s", messageID)
	_, err := client.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}

	fmt.Printf("Message %s deleted\n", messageID)
	return nil
}

func replicationStatus(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)
	data, err := client.doRequest("GET", "/api/v1/replication/status", nil)
	if err != nil {
		return err
	}

	var status map[string]interface{}
	if err := json.Unmarshal(data, &status); err != nil {
		return err
	}

	fmt.Println("Replication Status:")
	fmt.Println("─────────────────────────────────────")
	fmt.Printf("Mode: %s\n\n", status["mode"])

	if peers, ok := status["peers"].(map[string]interface{}); ok {
		fmt.Println("Peers:")
		for peer, connected := range peers {
			statusStr := "disconnected"
			if connected.(bool) {
				statusStr = "connected"
			}
			fmt.Printf("  %s: %s\n", peer, statusStr)
		}
	}

	return nil
}

func replicationPromote(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)
	_, err := client.doRequest("POST", "/api/v1/replication/promote", nil)
	if err != nil {
		return err
	}

	fmt.Println("Successfully promoted to primary mode")
	return nil
}

func healthCheck(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)
	data, err := client.doRequest("GET", "/health", nil)
	if err != nil {
		return err
	}

	var health map[string]interface{}
	if err := json.Unmarshal(data, &health); err != nil {
		return err
	}

	fmt.Printf("Health: %s\n", health["status"])
	if uptime, ok := health["uptime"]; ok {
		fmt.Printf("Uptime: %v\n", uptime)
	}

	return nil
}

func getMetricValue(metrics map[string]interface{}, metricType, tier string) int64 {
	if m, ok := metrics[metricType].(map[string]interface{}); ok {
		if val, ok := m[tier].(float64); ok {
			return int64(val)
		}
	}
	return 0
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
