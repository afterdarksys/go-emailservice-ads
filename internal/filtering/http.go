// Package filtering implements bounded scanner and reputation service clients.
package filtering

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Verdict struct {
	RemoveHeaders map[string]int `json:"-"`
	Headers       []Header       `json:"-"`
	Action        string         `json:"action"`
	Score         float64        `json:"score"`
	Subject       string         `json:"subject"`
}

var client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func Scan(ctx context.Context, endpoint, from, ip string, to []string, data []byte) (Verdict, error) {
	var verdict Verdict
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(endpoint, "/")+"/checkv2", bytes.NewReader(data))
	if err != nil {
		return verdict, err
	}
	req.Header.Set("Content-Type", "message/rfc822")
	req.Header.Set("From", from)
	req.Header.Set("IP", ip)
	for _, r := range to {
		req.Header.Add("Rcpt", r)
	}
	if err = fetch(req, &verdict); err != nil {
		return verdict, err
	}
	switch verdict.Action {
	case "no action", "add header", "rewrite subject", "reject", "soft reject", "greylist", "quarantine", "discard":
	default:
		return verdict, fmt.Errorf("unknown scanner action %q", verdict.Action)
	}
	return verdict, nil
}

type Reputation struct {
	Score     int       `json:"score"`
	Known     bool      `json:"known"`
	Source    string    `json:"source"`
	ExpiresAt time.Time `json:"expires_at"`
}

func LookupReputation(ctx context.Context, endpoint, ip string) (Reputation, error) {
	var r Reputation
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(endpoint, "/")+"/"+url.PathEscape(ip), nil)
	if err != nil {
		return r, err
	}
	if err = fetch(req, &r); err != nil {
		return r, err
	}
	if r.Score < 0 || r.Score > 100 || r.Source == "" || r.ExpiresAt.IsZero() || !r.ExpiresAt.After(time.Now()) {
		return r, fmt.Errorf("invalid or expired reputation response")
	}
	return r, nil
}
func fetch(req *http.Request, dst any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("filter service HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("filter response too large")
	}
	return json.Unmarshal(raw, dst)
}
