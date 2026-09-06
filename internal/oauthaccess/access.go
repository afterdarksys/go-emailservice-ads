// Package oauthaccess validates opaque or JWT access tokens at the configured
// authorization server. It does not accept ID tokens or trust decoded JWT data.
package oauthaccess

import (
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

type Config struct {
	Enabled              bool          `yaml:"enabled" json:"enabled"`
	ComplianceOnly       bool          `yaml:"compliance_only" json:"compliance_only"`
	RequireForCompliance bool          `yaml:"require_for_compliance" json:"require_for_compliance"`
	IntrospectionURL     string        `yaml:"introspection_url" json:"introspection_url"`
	ClientID             string        `yaml:"client_id" json:"client_id"`
	ClientSecretEnv      string        `yaml:"client_secret_env" json:"client_secret_env"`
	Issuer               string        `yaml:"issuer" json:"issuer"`
	Audience             string        `yaml:"audience" json:"audience"`
	Timeout              time.Duration `yaml:"timeout" json:"timeout"`
}

func (c Config) Validate() error {
	if !c.Enabled {
		if c.RequireForCompliance {
			return fmt.Errorf("OAuth required for compliance but disabled")
		}
		return nil
	}
	u, e := url.Parse(c.IntrospectionURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || c.ClientID == "" || c.ClientSecretEnv == "" || c.Issuer == "" || c.Audience == "" || c.Timeout < 0 || c.Timeout > time.Minute {
		return fmt.Errorf("OAuth requires HTTPS introspection, client credentials, issuer and audience; timeout must be 0..1m")
	}
	return nil
}
func ValidateToken(ctx context.Context, c Config, token, scope string) (string, error) {
	return validateToken(ctx, c, token, scope, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}
func validateToken(ctx context.Context, c Config, token, scope string, client *http.Client) (string, error) {
	if !c.Enabled || len(token) == 0 || len(token) > 8192 {
		return "", fmt.Errorf("invalid token")
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	secret := os.Getenv(c.ClientSecretEnv)
	if secret == "" {
		return "", fmt.Errorf("OAuth secret unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.IntrospectionURL, strings.NewReader(url.Values{"token": {token}, "token_type_hint": {"access_token"}}.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.ClientID, secret)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token validation unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("token validation rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return "", fmt.Errorf("invalid token response")
	}
	var claims struct {
		Active    bool            `json:"active"`
		Subject   string          `json:"sub"`
		Issuer    string          `json:"iss"`
		Audience  json.RawMessage `json:"aud"`
		Expires   int64           `json:"exp"`
		NotBefore int64           `json:"nbf"`
		Scope     string          `json:"scope"`
	}
	if err = json.Unmarshal(raw, &claims); err != nil {
		return "", err
	}
	now := time.Now().Unix()
	if !claims.Active || claims.Subject == "" || claims.Issuer != c.Issuer || claims.Expires <= now || claims.NotBefore > now {
		return "", fmt.Errorf("inactive or invalid access token")
	}
	var audiences []string
	var one string
	if json.Unmarshal(claims.Audience, &one) == nil {
		audiences = []string{one}
	} else {
		_ = json.Unmarshal(claims.Audience, &audiences)
	}
	matched := false
	for _, a := range audiences {
		matched = matched || a == c.Audience
	}
	if !matched {
		return "", fmt.Errorf("wrong token audience")
	}
	for _, s := range strings.Fields(claims.Scope) {
		if s == scope {
			return "oauth:" + claims.Subject, nil
		}
	}
	return "", fmt.Errorf("insufficient token scope")
}
