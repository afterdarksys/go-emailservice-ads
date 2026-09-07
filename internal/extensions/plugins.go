package extensions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Plugin struct {
	Name        string `yaml:"name"`
	URL         string `yaml:"url"`
	TokenEnv    string `yaml:"token_env"`
	Required    bool   `yaml:"required"`
	IncludeBody bool   `yaml:"include_body"`
}
type Admission struct {
	Version       int      `json:"version"`
	From          string   `json:"from"`
	To            []string `json:"to"`
	ClientIP      string   `json:"client_ip"`
	Username      string   `json:"username"`
	Authenticated bool     `json:"authenticated"`
	Data          []byte   `json:"data,omitempty"`
}
type Decision struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

func ValidatePlugins(plugins []Plugin) error {
	if len(plugins) > 4 {
		return fmt.Errorf("at most four admission plugins")
	}
	seen := map[string]bool{}
	for _, p := range plugins {
		u, e := url.Parse(p.URL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || p.Name == "" || seen[p.Name] || p.TokenEnv == "" || len(os.Getenv(p.TokenEnv)) < 32 {
			return fmt.Errorf("plugin %q requires unique name, HTTPS URL and token_env with at least 32 bytes", p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

// RunPlugins evaluates configured services sequentially. They cannot rewrite
// recipients, bypass authentication, or execute host commands.
func RunPlugins(ctx context.Context, plugins []Plugin, input Admission) (Decision, error) {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return runPlugins(ctx, client, plugins, input)
}
func runPlugins(ctx context.Context, client *http.Client, plugins []Plugin, input Admission) (Decision, error) {
	result := Decision{Action: "allow"}
	for _, p := range plugins {
		data := input
		data.Version = 1
		if !p.IncludeBody {
			data.Data = nil
		}
		decision, e := callPlugin(ctx, client, p, data)
		if e != nil {
			if p.Required {
				return Decision{}, fmt.Errorf("required plugin %s unavailable: %w", p.Name, e)
			}
			continue
		}
		switch decision.Action {
		case "reject", "defer":
			return decision, nil
		case "quarantine":
			result = decision
		}
	}
	return result, nil
}
func callPlugin(ctx context.Context, client *http.Client, p Plugin, data Admission) (Decision, error) {
	token := os.Getenv(p.TokenEnv)
	if len(token) < 32 {
		return Decision{}, fmt.Errorf("missing plugin credential")
	}
	b, e := json.Marshal(data)
	if e != nil {
		return Decision{}, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", p.URL, bytes.NewReader(b))
	if e != nil {
		return Decision{}, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, e := client.Do(req)
	if e != nil {
		return Decision{}, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Decision{}, fmt.Errorf("plugin HTTP %d", res.StatusCode)
	}
	b, e = io.ReadAll(io.LimitReader(res.Body, 4097))
	if e != nil || len(b) > 4096 {
		return Decision{}, fmt.Errorf("invalid plugin response size")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var d Decision
	if e = dec.Decode(&d); e != nil {
		return d, e
	}
	var extra interface{}
	if e = dec.Decode(&extra); e != io.EOF {
		return d, fmt.Errorf("extra plugin response")
	}
	if len(d.Reason) > 256 || strings.ContainsAny(d.Reason, "\r\n\x00") {
		return d, fmt.Errorf("invalid plugin reason")
	}
	switch d.Action {
	case "allow", "reject", "defer", "quarantine":
		return d, nil
	}
	return d, fmt.Errorf("unknown plugin action")
}
