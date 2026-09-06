// mailhub-authcheck qualifies configured production access-token validation.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/oauthaccess"
	"gopkg.in/yaml.v3"
	"os"
)

type Options struct {
	PlatformConfig  string `yaml:"platform_config"`
	TokenEnv        string `yaml:"token_env"`
	RequiredScope   string `yaml:"required_scope"`
	DeniedScope     string `yaml:"denied_scope"`
	RevokedTokenEnv string `yaml:"revoked_token_env"`
}

func main() {
	path := flag.String("config", "", "qualification YAML configuration")
	flag.Parse()
	if err := run(*path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var options Options
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err = decoder.Decode(&options); err != nil {
		return err
	}
	cfg, err := config.LoadConfig(options.PlatformConfig)
	if err != nil {
		return err
	}
	token := os.Getenv(options.TokenEnv)
	revoked := os.Getenv(options.RevokedTokenEnv)
	if !cfg.API.OAuth.Enabled || token == "" || revoked == "" || options.RequiredScope == "" || options.DeniedScope == "" {
		return fmt.Errorf("enabled OAuth and valid/revoked tokens plus required/denied scopes are required")
	}
	if _, err = oauthaccess.ValidateToken(context.Background(), cfg.API.OAuth, token, options.RequiredScope); err != nil {
		return fmt.Errorf("valid-token qualification failed: %w", err)
	}
	if _, err = oauthaccess.ValidateToken(context.Background(), cfg.API.OAuth, token, options.DeniedScope); !errors.Is(err, oauthaccess.ErrInsufficientScope) {
		return fmt.Errorf("scope rejection was not confirmed: %v", err)
	}
	if _, err = oauthaccess.ValidateToken(context.Background(), cfg.API.OAuth, revoked, options.RequiredScope); !errors.Is(err, oauthaccess.ErrInactive) {
		return fmt.Errorf("revocation was not confirmed: %v", err)
	}
	// Recheck a valid token to avoid accepting an outage as proof of revocation.
	if _, err = oauthaccess.ValidateToken(context.Background(), cfg.API.OAuth, token, options.RequiredScope); err != nil {
		return fmt.Errorf("provider health recheck failed: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"issuer": cfg.API.OAuth.Issuer, "valid_token": true, "denied_scope": true, "revoked_token": true})
}
