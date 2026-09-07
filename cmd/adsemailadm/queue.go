package main

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func queueCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "queue", Short: "Inspect pending mail and retry failed deliveries"}
	cmd.AddCommand(endpointCommand("stats", "GET", "/api/v1/queue/stats"))
	var tier string
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, a []string) error {
		return printResponse(cmd, "GET", "/api/v1/queue/pending?tier="+url.QueryEscape(tier), nil)
	}}
	list.Flags().StringVar(&tier, "tier", "", "Queue tier filter")
	cmd.AddCommand(list)
	for _, op := range []string{"inspect", "delete"} {
		op := op
		method := "GET"
		if op == "delete" {
			method = "DELETE"
		}
		cmd.AddCommand(&cobra.Command{Use: op + " ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
			if err := validID(a[0]); err != nil {
				return err
			}
			return printResponse(cmd, method, "/api/v1/message/"+url.PathEscape(a[0]), nil)
		}})
	}
	dlq := &cobra.Command{Use: "dlq"}
	dlq.AddCommand(endpointCommand("list", "GET", "/api/v1/dlq/list"))
	cmd.AddCommand(dlq)
	var all bool
	retry := &cobra.Command{Use: "retry [ID]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		if all && len(a) > 0 || !all && len(a) == 0 {
			return fmt.Errorf("specify ID or --all exclusively")
		}
		ids := a
		if all {
			resp, e := apiRequest("GET", "/api/v1/dlq/list", nil)
			if e != nil {
				return e
			}
			defer resp.Body.Close()
			var entries []struct {
				ID string `json:"id"`
			}
			if e = json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&entries); e != nil {
				return e
			}
			for _, entry := range entries {
				ids = append(ids, entry.ID)
			}
		}
		for _, id := range ids {
			if e := validID(id); e != nil {
				return e
			}
			if e := printResponse(cmd, "POST", "/api/v1/dlq/retry/"+url.PathEscape(id), nil); e != nil {
				return fmt.Errorf("retry %s failed (earlier retries may have succeeded): %w", id, e)
			}
		}
		cmd.Printf("Retry accepted for %d messages\n", len(ids))
		return nil
	}}
	retry.Flags().BoolVar(&all, "all", false, "Enumerate and retry current DLQ entries; stops on first failure")
	cmd.AddCommand(retry)
	return cmd
}
func validID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\?#\r\n") {
		return fmt.Errorf("invalid message ID")
	}
	return nil
}
func endpointCommand(use, method, path string) *cobra.Command {
	return &cobra.Command{Use: use, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, a []string) error { return printResponse(cmd, method, path, nil) }}
}
func printResponse(cmd *cobra.Command, method, path string, body io.Reader) error {
	resp, e := apiRequest(method, path, body)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
	if e != nil {
		return e
	}
	if len(b) > 32<<20 {
		return fmt.Errorf("response exceeds 32 MiB")
	}
	cmd.Println(string(b))
	return nil
}
func genericAPICmd() *cobra.Command {
	var bodyFile string
	cmd := &cobra.Command{Use: "api METHOD /api/v1/PATH", Short: "Call an existing management endpoint with normal server authorization", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, a []string) error {
		if !strings.HasPrefix(a[1], "/api/v1/") {
			return fmt.Errorf("management /api/v1/ path required")
		}
		switch a[0] {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return fmt.Errorf("unsupported method")
		}
		var body io.Reader
		if bodyFile != "" {
			f, e := os.Open(bodyFile)
			if e != nil {
				return e
			}
			defer f.Close()
			body = io.LimitReader(f, 8<<20)
		}
		return printResponse(cmd, a[0], a[1], body)
	}}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "JSON request file")
	return cmd
}
func apiRequest(method, path string, body io.Reader) (*http.Response, error) {
	base, e := url.Parse(apiEndpoint)
	if e != nil {
		return nil, e
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Path != "" && base.Path != "/" {
		return nil, fmt.Errorf("API endpoint must be an origin without credentials, query or path")
	}
	host := base.Hostname()
	ip := net.ParseIP(host)
	loopback := host == "localhost" || ip != nil && ip.IsLoopback()
	if base.Scheme != "https" && !(base.Scheme == "http" && loopback) {
		return nil, fmt.Errorf("remote API access requires HTTPS")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, fmt.Errorf("absolute API path required")
	}
	req, e := http.NewRequest(method, strings.TrimRight(apiEndpoint, "/")+path, body)
	if e != nil {
		return nil, e
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else if apiUser != "" || apiPassword != "" {
		req.SetBasicAuth(apiUser, apiPassword)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("API %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return resp, nil
}
func getString(m map[string]interface{}, key string) string { v, _ := m[key].(string); return v }
func getIntOrZero(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
