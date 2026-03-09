package elasticsearch

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

// Client wraps the Elasticsearch client with mail-specific functionality
type Client struct {
	es            *elasticsearch.Client
	logger        *zap.Logger
	config        *config.Config
	indexPrefix   string
	headerLogger  *HeaderLogger
}

// NewClient creates a new Elasticsearch client
func NewClient(cfg *config.Config, logger *zap.Logger) (*Client, error) {
	if !cfg.Elasticsearch.Enabled {
		return nil, fmt.Errorf("elasticsearch is not enabled in configuration")
	}

	// Expand environment variables in credentials
	apiKey := os.ExpandEnv(cfg.Elasticsearch.APIKey)
	username := os.ExpandEnv(cfg.Elasticsearch.Username)
	password := os.ExpandEnv(cfg.Elasticsearch.Password)

	// Build Elasticsearch config
	esConfig := elasticsearch.Config{
		Addresses: cfg.Elasticsearch.Endpoints,
		Transport: &http.Transport{
			MaxIdleConnsPerHost:   10,
			ResponseHeaderTimeout: 30 * time.Second,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	// Set authentication (prefer API key)
	if apiKey != "" && apiKey != "${ES_API_KEY}" {
		esConfig.APIKey = apiKey
	} else if username != "" && password != "" && password != "${ES_PASSWORD}" {
		esConfig.Username = username
		esConfig.Password = password
	}

	// Create Elasticsearch client
	es, err := elasticsearch.NewClient(esConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := es.Info(es.Info.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to elasticsearch: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	logger.Info("Connected to Elasticsearch",
		zap.Strings("endpoints", cfg.Elasticsearch.Endpoints))

	// Create header logger
	headerLogger := NewHeaderLogger(cfg, logger)

	return &Client{
		es:           es,
		logger:       logger,
		config:       cfg,
		indexPrefix:  cfg.Elasticsearch.IndexPrefix,
		headerLogger: headerLogger,
	}, nil
}

// GetIndexName returns the time-based index name for the current date
func (c *Client) GetIndexName() string {
	// Create daily indices: mail-events-2026.03.09
	return fmt.Sprintf("%s-%s", c.indexPrefix, time.Now().Format("2006.01.02"))
}

// GetIndexNameForTime returns the index name for a specific time
func (c *Client) GetIndexNameForTime(t time.Time) string {
	return fmt.Sprintf("%s-%s", c.indexPrefix, t.Format("2006.01.02"))
}

// GetIndexPattern returns the index pattern for searching across all mail events
func (c *Client) GetIndexPattern() string {
	return fmt.Sprintf("%s-*", c.indexPrefix)
}

// Close closes the Elasticsearch client
func (c *Client) Close() error {
	c.logger.Info("Closing Elasticsearch client")
	return nil
}

// Ping tests the connection to Elasticsearch
func (c *Client) Ping(ctx context.Context) error {
	res, err := c.es.Ping(c.es.Ping.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch ping failed: %s", res.String())
	}

	return nil
}

// shouldLogHeaders determines if headers should be logged for this event
// based on the header logging configuration
func (c *Client) shouldLogHeaders(event *MailEvent) bool {
	return c.headerLogger.ShouldLogHeaders(event)
}

// extractHeaders extracts and filters headers from the raw message data
func (c *Client) extractHeaders(data []byte) (map[string][]string, error) {
	return c.headerLogger.ExtractHeaders(data)
}

// getESEndpointInfo returns formatted info about the ES cluster
func (c *Client) getESEndpointInfo() string {
	return strings.Join(c.config.Elasticsearch.Endpoints, ", ")
}
