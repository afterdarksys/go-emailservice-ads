package directory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

// Client handles lookups against the external msgs.global directory services
type Client struct {
	logger     *zap.Logger
	config     *config.Config
	httpClient *http.Client
	baseURL    string // e.g., "http://gomailservices/directory/"
}

// UserInfo represents the response from the directory service
type UserInfo struct {
	Username   string   `json:"username"`
	Email      string   `json:"email"`
	Roles      []string `json:"roles,omitempty"`
	IsActive   bool     `json:"is_active"`
	QuotaBytes int64    `json:"quota_bytes,omitempty"`
}

// NewClient initializes the directory client
func NewClient(cfg *config.Config, logger *zap.Logger) *Client {
	// Use SSO directory URL if configured, otherwise use placeholder
	baseURL := "http://gomailservices/directory/"
	if cfg.SSO.Enabled && cfg.SSO.DirectoryURL != "" {
		baseURL = cfg.SSO.DirectoryURL + "/v1/"
	}

	return &Client{
		logger: logger,
		config: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL: baseURL,
	}
}

// Lookup queries the directory service for a specific identity
func (c *Client) Lookup(ctx context.Context, query string) (*UserInfo, error) {
	c.logger.Debug("Directory lookup initiated", zap.String("query", query))

	reqURL, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}

	q := reqURL.Query()
	q.Set("query", query)
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("directory lookup failed with status: %d", resp.StatusCode)
	}

	var info UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	return &info, nil
}
