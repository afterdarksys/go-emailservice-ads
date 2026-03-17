package main

import (
	"bytes"
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

	// Domain commands
	domainCmd := &cobra.Command{
		Use:   "domain",
		Short: "Manage domains",
	}

	domainCmd.AddCommand(&cobra.Command{
		Use:   "add <domain>",
		Short: "Add a new domain",
		Args:  cobra.ExactArgs(1),
		RunE:  domainAdd,
	})

	domainCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all domains",
		RunE:  domainList,
	})

	domainCmd.AddCommand(&cobra.Command{
		Use:   "delete <domain>",
		Short: "Delete a domain",
		Args:  cobra.ExactArgs(1),
		RunE:  domainDelete,
	})

	domainCmd.AddCommand(&cobra.Command{
		Use:   "info <domain>",
		Short: "Get domain information",
		Args:  cobra.ExactArgs(1),
		RunE:  domainInfo,
	})

	// User commands
	userCmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users",
	}

	userAddCmd := &cobra.Command{
		Use:   "add <email>",
		Short: "Add a new user",
		Args:  cobra.ExactArgs(1),
		RunE:  userAdd,
	}
	userAddCmd.Flags().String("password", "", "User password (required)")
	userCmd.AddCommand(userAddCmd)

	userCmd.AddCommand(&cobra.Command{
		Use:   "list [domain]",
		Short: "List users (optionally filtered by domain)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  userList,
	})

	userCmd.AddCommand(&cobra.Command{
		Use:   "delete <email>",
		Short: "Delete a user",
		Args:  cobra.ExactArgs(1),
		RunE:  userDelete,
	})

	userCmd.AddCommand(&cobra.Command{
		Use:   "info <email>",
		Short: "Get user information",
		Args:  cobra.ExactArgs(1),
		RunE:  userInfo,
	})

	userPasswdCmd := &cobra.Command{
		Use:   "passwd <email>",
		Short: "Change user password",
		Args:  cobra.ExactArgs(1),
		RunE:  userPasswd,
	}
	userPasswdCmd.Flags().String("password", "", "New password (required)")
	userCmd.AddCommand(userPasswdCmd)

	// Alias commands
	aliasCmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage email aliases",
	}

	aliasAddCmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Add an email alias",
		Args:  cobra.ExactArgs(1),
		RunE:  aliasAdd,
	}
	aliasAddCmd.Flags().String("target", "", "Target email address (required)")
	aliasCmd.AddCommand(aliasAddCmd)

	aliasCmd.AddCommand(&cobra.Command{
		Use:   "list [domain]",
		Short: "List aliases (optionally filtered by domain)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  aliasList,
	})

	aliasCmd.AddCommand(&cobra.Command{
		Use:   "delete <source>",
		Short: "Delete an alias",
		Args:  cobra.ExactArgs(1),
		RunE:  aliasDelete,
	})

	// Tenant commands
	tenantCmd := &cobra.Command{
		Use:   "tenant",
		Short: "Manage tenants (multi-tenant mode)",
	}

	tenantAddCmd := &cobra.Command{
		Use:   "add <tenant-id>",
		Short: "Add a new tenant",
		Args:  cobra.ExactArgs(1),
		RunE:  tenantAdd,
	}
	tenantAddCmd.Flags().String("name", "", "Tenant name (defaults to tenant-id)")
	tenantCmd.AddCommand(tenantAddCmd)

	tenantCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all tenants",
		RunE:  tenantList,
	})

	tenantCmd.AddCommand(&cobra.Command{
		Use:   "delete <tenant-id>",
		Short: "Delete a tenant",
		Args:  cobra.ExactArgs(1),
		RunE:  tenantDelete,
	})

	rootCmd.AddCommand(queueCmd)
	rootCmd.AddCommand(dlqCmd)
	rootCmd.AddCommand(msgCmd)
	rootCmd.AddCommand(replicationCmd)
	rootCmd.AddCommand(domainCmd)
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(aliasCmd)
	rootCmd.AddCommand(tenantCmd)

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

// ===== Domain Management =====

func domainAdd(cmd *cobra.Command, args []string) error {
	domain := args[0]
	client := NewClient(apiURL, username, password, insecure)

	payload := map[string]interface{}{
		"domain": domain,
	}
	jsonData, _ := json.Marshal(payload)

	_, err := client.doRequest("POST", "/api/v1/domains", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}

	fmt.Printf("✓ Domain added: %s\n", domain)
	return nil
}

func domainList(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)
	data, err := client.doRequest("GET", "/api/v1/domains", nil)
	if err != nil {
		return err
	}

	var domains []map[string]interface{}
	if err := json.Unmarshal(data, &domains); err != nil {
		return err
	}

	if len(domains) == 0 {
		fmt.Println("No domains found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DOMAIN\tSTATUS\tUSERS\tCREATED")

	for _, d := range domains {
		fmt.Fprintf(w, "%s\t%s\t%v\t%s\n",
			d["domain"],
			d["status"],
			d["user_count"],
			d["created_at"])
	}
	w.Flush()

	return nil
}

func domainDelete(cmd *cobra.Command, args []string) error {
	domain := args[0]
	client := NewClient(apiURL, username, password, insecure)

	_, err := client.doRequest("DELETE", fmt.Sprintf("/api/v1/domains/%s", domain), nil)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Domain deleted: %s\n", domain)
	return nil
}

func domainInfo(cmd *cobra.Command, args []string) error {
	domain := args[0]
	client := NewClient(apiURL, username, password, insecure)

	data, err := client.doRequest("GET", fmt.Sprintf("/api/v1/domains/%s", domain), nil)
	if err != nil {
		return err
	}

	var info map[string]interface{}
	if err := json.Unmarshal(data, &info); err != nil {
		return err
	}

	yamlData, err := yaml.Marshal(info)
	if err != nil {
		return err
	}

	fmt.Println(string(yamlData))
	return nil
}

// ===== User Management =====

func userAdd(cmd *cobra.Command, args []string) error {
	email := args[0]
	password, _ := cmd.Flags().GetString("password")

	if password == "" {
		return fmt.Errorf("--password flag is required")
	}

	client := NewClient(apiURL, username, password, insecure)

	payload := map[string]interface{}{
		"email":    email,
		"password": password,
	}
	jsonData, _ := json.Marshal(payload)

	_, err := client.doRequest("POST", "/api/v1/users", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}

	fmt.Printf("✓ User added: %s\n", email)
	return nil
}

func userList(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)

	path := "/api/v1/users"
	if len(args) > 0 {
		path = fmt.Sprintf("%s?domain=%s", path, args[0])
	}

	data, err := client.doRequest("GET", path, nil)
	if err != nil {
		return err
	}

	var users []map[string]interface{}
	if err := json.Unmarshal(data, &users); err != nil {
		return err
	}

	if len(users) == 0 {
		fmt.Println("No users found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "EMAIL\tSTATUS\tQUOTA\tCREATED")

	for _, u := range users {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			u["email"],
			u["status"],
			u["quota"],
			u["created_at"])
	}
	w.Flush()

	return nil
}

func userDelete(cmd *cobra.Command, args []string) error {
	email := args[0]
	client := NewClient(apiURL, username, password, insecure)

	_, err := client.doRequest("DELETE", fmt.Sprintf("/api/v1/users/%s", email), nil)
	if err != nil {
		return err
	}

	fmt.Printf("✓ User deleted: %s\n", email)
	return nil
}

func userInfo(cmd *cobra.Command, args []string) error {
	email := args[0]
	client := NewClient(apiURL, username, password, insecure)

	data, err := client.doRequest("GET", fmt.Sprintf("/api/v1/users/%s", email), nil)
	if err != nil {
		return err
	}

	var info map[string]interface{}
	if err := json.Unmarshal(data, &info); err != nil {
		return err
	}

	yamlData, err := yaml.Marshal(info)
	if err != nil {
		return err
	}

	fmt.Println(string(yamlData))
	return nil
}

func userPasswd(cmd *cobra.Command, args []string) error {
	email := args[0]
	newPassword, _ := cmd.Flags().GetString("password")

	if newPassword == "" {
		return fmt.Errorf("--password flag is required")
	}

	client := NewClient(apiURL, username, password, insecure)

	payload := map[string]interface{}{
		"password": newPassword,
	}
	jsonData, _ := json.Marshal(payload)

	_, err := client.doRequest("PUT", fmt.Sprintf("/api/v1/users/%s/password", email), bytes.NewReader(jsonData))
	if err != nil {
		return err
	}

	fmt.Printf("✓ Password updated for: %s\n", email)
	return nil
}

// ===== Alias Management =====

func aliasAdd(cmd *cobra.Command, args []string) error {
	source := args[0]
	target, _ := cmd.Flags().GetString("target")

	if target == "" {
		return fmt.Errorf("--target flag is required")
	}

	client := NewClient(apiURL, username, password, insecure)

	payload := map[string]interface{}{
		"source": source,
		"target": target,
	}
	jsonData, _ := json.Marshal(payload)

	_, err := client.doRequest("POST", "/api/v1/aliases", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}

	fmt.Printf("✓ Alias added: %s -> %s\n", source, target)
	return nil
}

func aliasList(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)

	path := "/api/v1/aliases"
	if len(args) > 0 {
		path = fmt.Sprintf("%s?domain=%s", path, args[0])
	}

	data, err := client.doRequest("GET", path, nil)
	if err != nil {
		return err
	}

	var aliases []map[string]interface{}
	if err := json.Unmarshal(data, &aliases); err != nil {
		return err
	}

	if len(aliases) == 0 {
		fmt.Println("No aliases found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SOURCE\tTARGET\tCREATED")

	for _, a := range aliases {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			a["source"],
			a["target"],
			a["created_at"])
	}
	w.Flush()

	return nil
}

func aliasDelete(cmd *cobra.Command, args []string) error {
	source := args[0]
	client := NewClient(apiURL, username, password, insecure)

	_, err := client.doRequest("DELETE", fmt.Sprintf("/api/v1/aliases/%s", source), nil)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Alias deleted: %s\n", source)
	return nil
}

// ===== Tenant Management =====

func tenantAdd(cmd *cobra.Command, args []string) error {
	tenantID := args[0]
	name, _ := cmd.Flags().GetString("name")

	if name == "" {
		name = tenantID
	}

	client := NewClient(apiURL, username, password, insecure)

	payload := map[string]interface{}{
		"tenant_id": tenantID,
		"name":      name,
	}
	jsonData, _ := json.Marshal(payload)

	_, err := client.doRequest("POST", "/api/v1/tenants", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}

	fmt.Printf("✓ Tenant added: %s (%s)\n", tenantID, name)
	return nil
}

func tenantList(cmd *cobra.Command, args []string) error {
	client := NewClient(apiURL, username, password, insecure)
	data, err := client.doRequest("GET", "/api/v1/tenants", nil)
	if err != nil {
		return err
	}

	var tenants []map[string]interface{}
	if err := json.Unmarshal(data, &tenants); err != nil {
		return err
	}

	if len(tenants) == 0 {
		fmt.Println("No tenants found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TENANT ID\tNAME\tDOMAINS\tUSERS\tCREATED")

	for _, t := range tenants {
		fmt.Fprintf(w, "%s\t%s\t%v\t%v\t%s\n",
			t["tenant_id"],
			t["name"],
			t["domain_count"],
			t["user_count"],
			t["created_at"])
	}
	w.Flush()

	return nil
}

func tenantDelete(cmd *cobra.Command, args []string) error {
	tenantID := args[0]
	client := NewClient(apiURL, username, password, insecure)

	_, err := client.doRequest("DELETE", fmt.Sprintf("/api/v1/tenants/%s", tenantID), nil)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Tenant deleted: %s\n", tenantID)
	return nil
}
