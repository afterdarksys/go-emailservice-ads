package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

// SSOProvider handles external SSO authentication
type SSOProvider struct {
	logger       *zap.Logger
	config       *config.Config
	oauth2Config *oauth2.Config
	httpClient   *http.Client
}

// SSOUser represents a user authenticated via SSO
type SSOUser struct {
	Sub           string   `json:"sub"`            // Subject (unique user ID)
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Name          string   `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Roles         []string `json:"roles,omitempty"`
	Groups        []string `json:"groups,omitempty"`
}

// NewSSOProvider creates a new SSO authentication provider
func NewSSOProvider(cfg *config.Config, logger *zap.Logger) *SSOProvider {
	if !cfg.SSO.Enabled {
		logger.Info("SSO authentication is disabled")
		return nil
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     expandEnvVars(cfg.SSO.ClientID),
		ClientSecret: expandEnvVars(cfg.SSO.ClientSecret),
		RedirectURL:  cfg.SSO.RedirectURL,
		Scopes:       cfg.SSO.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg.SSO.AuthURL,
			TokenURL: cfg.SSO.TokenURL,
		},
	}

	return &SSOProvider{
		logger:       logger,
		config:       cfg,
		oauth2Config: oauth2Cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// AuthenticateWithToken validates an OAuth2 access token
func (s *SSOProvider) AuthenticateWithToken(ctx context.Context, accessToken string) (*SSOUser, error) {
	if !s.config.SSO.Enabled {
		return nil, fmt.Errorf("SSO is not enabled")
	}

	// Call the UserInfo endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", s.config.SSO.UserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create userinfo request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo request failed with status: %d", resp.StatusCode)
	}

	var user SSOUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode userinfo response: %w", err)
	}

	s.logger.Info("SSO authentication successful",
		zap.String("email", user.Email),
		zap.String("name", user.Name),
		zap.Bool("email_verified", user.EmailVerified))

	return &user, nil
}

// AuthenticateWithPassword validates credentials via After Dark Systems directory
// This is used for SMTP AUTH PLAIN/LOGIN
func (s *SSOProvider) AuthenticateWithPassword(ctx context.Context, email, password string) (*SSOUser, error) {
	if !s.config.SSO.Enabled {
		return nil, fmt.Errorf("SSO is not enabled")
	}

	// For msgs.global users, authenticate via directory service
	if !strings.HasSuffix(strings.ToLower(email), "@msgs.global") {
		return nil, fmt.Errorf("email domain not supported for SSO")
	}

	directoryURL := s.config.SSO.DirectoryURL
	if directoryURL == "" {
		directoryURL = "https://directory.msgs.global"
	}

	// Call directory authentication endpoint
	authURL := fmt.Sprintf("%s/v1/auth/verify", directoryURL)

	data := url.Values{}
	data.Set("email", email)
	data.Set("password", password)

	req, err := http.NewRequestWithContext(ctx, "POST", authURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create auth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Error("Directory authentication failed",
			zap.String("url", authURL),
			zap.Error(err))
		return nil, fmt.Errorf("directory service unavailable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		s.logger.Warn("SSO authentication failed",
			zap.String("email", email),
			zap.Int("status", resp.StatusCode))
		return nil, ErrInvalidCredentials
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("Directory authentication error",
			zap.String("email", email),
			zap.Int("status", resp.StatusCode))
		return nil, fmt.Errorf("directory service error: status %d", resp.StatusCode)
	}

	var user SSOUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode directory response: %w", err)
	}

	// Ensure email is set if not in response
	if user.Email == "" {
		user.Email = email
	}

	s.logger.Info("SSO directory authentication successful",
		zap.String("email", user.Email),
		zap.String("name", user.Name))

	return &user, nil
}

// GetAuthorizationURL returns the OAuth2 authorization URL for web-based SSO
func (s *SSOProvider) GetAuthorizationURL(state string) string {
	return s.oauth2Config.AuthCodeURL(state)
}

// ExchangeCode exchanges an authorization code for tokens
func (s *SSOProvider) ExchangeCode(ctx context.Context, code string) (*oauth2.Token, error) {
	return s.oauth2Config.Exchange(ctx, code)
}

// expandEnvVars expands ${VAR} syntax in configuration values
func expandEnvVars(s string) string {
	// Simple implementation - replace ${VAR} with environment variable value
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		envVar := s[2 : len(s)-1]
		if val := getEnv(envVar); val != "" {
			return val
		}
	}
	return s
}

// getEnv gets environment variable with empty string as default
func getEnv(key string) string {
	return os.Getenv(key)
}
