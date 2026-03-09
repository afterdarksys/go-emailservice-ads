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

	Logging struct {
		Level string `yaml:"level"` // debug, info, warn, error
	} `yaml:"logging"`
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
	cfg.Logging.Level = "info"

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
