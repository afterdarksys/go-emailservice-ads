package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Addr              string     `yaml:"addr"`
		Domain            string     `yaml:"domain"`
		TLS               *TLSConfig `yaml:"tls,omitempty"`
		MaxMessageBytes   int        `yaml:"max_message_bytes"`
		MaxRecipients     int        `yaml:"max_recipients"`
		AllowInsecureAuth bool       `yaml:"allow_insecure_auth"` // Allow AUTH over non-TLS
		RequireAuth       bool       `yaml:"require_auth"`        // Require authentication for all mail
		RequireTLS        bool       `yaml:"require_tls"`         // Require STARTTLS before AUTH
		Mode              string     `yaml:"mode"`                // test or prod

		// Connection and Rate Limiting
		MaxConnections    int  `yaml:"max_connections"`     // Total concurrent connections (0 = unlimited)
		MaxPerIP          int  `yaml:"max_per_ip"`          // Connections per IP (0 = unlimited)
		RateLimitPerIP    int  `yaml:"rate_limit_per_ip"`   // Messages per hour per IP (0 = unlimited)
		EnableGreylist    bool `yaml:"enable_greylist"`     // Enable greylisting
		DisableVRFY       bool `yaml:"disable_vrfy"`        // Disable VRFY command
		DisableEXPN       bool `yaml:"disable_expn"`        // Disable EXPN command

		// Local domains for delivery
		LocalDomains      []string `yaml:"local_domains"`   // Domains handled locally

		// DANE Configuration (RFC 6698, RFC 7672)
		DANE              DANEConfig `yaml:"dane"`
	} `yaml:"server"`

	IMAP struct {
		Addr       string     `yaml:"addr"`
		TLS        *TLSConfig `yaml:"tls,omitempty"`
		RequireTLS bool       `yaml:"require_tls"` // Require TLS for IMAP
	} `yaml:"imap"`

	API struct {
		RESTAddr string `yaml:"rest_addr"`
		GRPCAddr string `yaml:"grpc_addr"`
	} `yaml:"api"`

	Auth struct {
		DefaultUsers []UserConfig `yaml:"default_users"`
	} `yaml:"auth"`

	// SSO Configuration for external authentication providers
	SSO struct {
		Enabled      bool   `yaml:"enabled"`       // Enable SSO authentication
		Provider     string `yaml:"provider"`      // Provider name (e.g., "afterdarksystems", "oidc", "oauth2")
		ClientID     string `yaml:"client_id"`     // OAuth2 client ID
		ClientSecret string `yaml:"client_secret"` // OAuth2 client secret
		AuthURL      string `yaml:"auth_url"`      // Authorization endpoint
		TokenURL     string `yaml:"token_url"`     // Token endpoint
		UserInfoURL  string `yaml:"userinfo_url"`  // UserInfo endpoint
		RedirectURL  string `yaml:"redirect_url"`  // OAuth2 redirect URL
		Scopes       []string `yaml:"scopes"`      // OAuth2 scopes
		// AfterDark Systems specific
		DirectoryURL string `yaml:"directory_url"` // Directory service URL (e.g., https://directory.afterdarksystems.com)
	} `yaml:"sso"`

	// AfterSMTP AMTP Next-Gen Protocol Integration
	AfterSMTP struct {
		Enabled     bool   `yaml:"enabled"`
		LedgerURL   string `yaml:"ledger_url"`
		QUICAddr    string `yaml:"quic_addr"`
		GRPCAddr    string `yaml:"grpc_addr"`
		FallbackDB  string `yaml:"fallback_db"`
	} `yaml:"aftersmtp"`

	Logging struct {
		Level string `yaml:"level"` // debug, info, warn, error
	} `yaml:"logging"`

	// Elasticsearch Configuration for mail event logging and search
	Elasticsearch struct {
		Enabled       bool     `yaml:"enabled"`        // Enable Elasticsearch integration
		Endpoints     []string `yaml:"endpoints"`      // ES cluster endpoints
		IndexPrefix   string   `yaml:"index_prefix"`   // Index name prefix (e.g., "mail-events")
		BulkSize      int      `yaml:"bulk_size"`      // Bulk indexer batch size
		FlushInterval string   `yaml:"flush_interval"` // How often to flush bulk indexer (e.g., "5s")

		// Authentication
		APIKey       string `yaml:"api_key"`       // Elasticsearch API key
		Username     string `yaml:"username"`      // Basic auth username
		Password     string `yaml:"password"`      // Basic auth password

		// Index Lifecycle Management
		RetentionDays int `yaml:"retention_days"` // How long to keep indices
		Replicas      int `yaml:"replicas"`       // Number of replicas
		Shards        int `yaml:"shards"`         // Number of shards

		// Performance
		Workers       int     `yaml:"workers"`        // Number of bulk indexer workers
		SamplingRate  float64 `yaml:"sampling_rate"`  // Sample rate (0.0-1.0, 1.0 = all events)

		// Header Logging Configuration
		HeaderLogging HeaderLoggingConfig `yaml:"header_logging"`
	} `yaml:"elasticsearch"`
}

// HeaderLoggingConfig controls which message headers are logged to Elasticsearch
type HeaderLoggingConfig struct {
	Enabled       bool     `yaml:"enabled"`        // Global enable/disable for header logging
	LogAllHeaders bool     `yaml:"log_all_headers"` // Log all headers or only specific ones

	// Allowlist/Denylist by domain
	AllowDomains  []string `yaml:"allow_domains"`  // Domains to log headers for (empty = all)
	DenyDomains   []string `yaml:"deny_domains"`   // Domains to never log headers for

	// Allowlist/Denylist by IP
	AllowIPs      []string `yaml:"allow_ips"`      // IPs to log headers for (supports CIDR)
	DenyIPs       []string `yaml:"deny_ips"`       // IPs to never log headers for (supports CIDR)

	// Allowlist/Denylist by MX record
	AllowMXs      []string `yaml:"allow_mxs"`      // MX records to log headers for
	DenyMXs       []string `yaml:"deny_mxs"`       // MX records to never log headers for

	// Specific headers to include (if log_all_headers = false)
	IncludeHeaders []string `yaml:"include_headers"` // e.g., ["From", "To", "Subject", "Message-ID"]

	// Headers to always exclude (even if log_all_headers = true)
	ExcludeHeaders []string `yaml:"exclude_headers"` // e.g., ["Authorization", "X-Secret"]

	// Redaction patterns
	RedactPatterns []string `yaml:"redact_patterns"` // Regex patterns to redact from header values
}

type TLSConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type UserConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Email    string `yaml:"email"`
}

// DANEConfig configures DANE (DNS-Based Authentication of Named Entities)
// RFC 6698, RFC 7672 - SMTP Security via DANE
type DANEConfig struct {
	Enabled      bool     `yaml:"enabled"`        // Enable DANE validation
	StrictMode   bool     `yaml:"strict_mode"`    // Reject delivery if DANE validation fails
	DNSServers   []string `yaml:"dns_servers"`    // DNS servers for DNSSEC queries (empty = use system defaults)
	CacheTTL     int      `yaml:"cache_ttl"`      // TLSA cache TTL in seconds (0 = use DNS TTL)
	Timeout      int      `yaml:"timeout"`        // DANE validation timeout in seconds
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	// Set defaults
	cfg.Server.Addr = ":2525"
	cfg.Server.Domain = "localhost"
	cfg.Server.MaxMessageBytes = 10 * 1024 * 1024 // 10MB default
	cfg.Server.MaxRecipients = 50
	cfg.Server.AllowInsecureAuth = false // SECURITY: Default to secure
	cfg.Server.RequireAuth = true        // SECURITY: Require auth by default
	cfg.Server.RequireTLS = true         // SECURITY: Require TLS by default
	cfg.Server.MaxConnections = 1000     // SECURITY: Limit concurrent connections
	cfg.Server.MaxPerIP = 10             // SECURITY: Limit per-IP connections
	cfg.Server.RateLimitPerIP = 100      // SECURITY: 100 messages/hour per IP
	cfg.Server.EnableGreylist = false    // Default off (can cause delays)
	cfg.Server.DisableVRFY = true        // SECURITY: Disable user enumeration
	cfg.Server.DisableEXPN = true        // SECURITY: Disable mailing list expansion
	cfg.Server.LocalDomains = []string{"localhost", "localhost.local"}
	cfg.Server.DANE.Enabled = true       // SECURITY: Enable DANE by default
	cfg.Server.DANE.StrictMode = false   // Default to opportunistic DANE
	cfg.Server.DANE.DNSServers = []string{} // Use system defaults
	cfg.Server.DANE.CacheTTL = 3600      // 1 hour cache
	cfg.Server.DANE.Timeout = 10         // 10 second timeout
	cfg.IMAP.Addr = ":1143"
	cfg.IMAP.RequireTLS = true // SECURITY: Require TLS for IMAP
	
	// AfterSMTP Defaults
	cfg.AfterSMTP.Enabled = false
	cfg.AfterSMTP.LedgerURL = "ws://127.0.0.1:9944"
	cfg.AfterSMTP.QUICAddr = ":4434"
	cfg.AfterSMTP.GRPCAddr = ":4433"
	cfg.AfterSMTP.FallbackDB = "fallback_ledger.db"

	cfg.Logging.Level = "info"

	// Elasticsearch defaults
	cfg.Elasticsearch.Enabled = false // Default off
	cfg.Elasticsearch.Endpoints = []string{"http://localhost:9200"}
	cfg.Elasticsearch.IndexPrefix = "mail-events"
	cfg.Elasticsearch.BulkSize = 1000
	cfg.Elasticsearch.FlushInterval = "5s"
	cfg.Elasticsearch.RetentionDays = 90
	cfg.Elasticsearch.Replicas = 1
	cfg.Elasticsearch.Shards = 3
	cfg.Elasticsearch.Workers = 4
	cfg.Elasticsearch.SamplingRate = 1.0 // Log all events by default

	// Header logging defaults
	cfg.Elasticsearch.HeaderLogging.Enabled = false // Default off for privacy
	cfg.Elasticsearch.HeaderLogging.LogAllHeaders = false
	cfg.Elasticsearch.HeaderLogging.AllowDomains = []string{}
	cfg.Elasticsearch.HeaderLogging.DenyDomains = []string{}
	cfg.Elasticsearch.HeaderLogging.AllowIPs = []string{}
	cfg.Elasticsearch.HeaderLogging.DenyIPs = []string{}
	cfg.Elasticsearch.HeaderLogging.AllowMXs = []string{}
	cfg.Elasticsearch.HeaderLogging.DenyMXs = []string{}
	cfg.Elasticsearch.HeaderLogging.IncludeHeaders = []string{"From", "To", "Subject", "Message-ID", "Date"}
	cfg.Elasticsearch.HeaderLogging.ExcludeHeaders = []string{"Authorization", "X-API-Key", "X-Auth-Token"}
	cfg.Elasticsearch.HeaderLogging.RedactPatterns = []string{}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
