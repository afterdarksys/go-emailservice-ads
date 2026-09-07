package config

import (
	"bytes"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/oauthaccess"
	"io"
	"os"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/ipfilter"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Platform PlatformConfig `yaml:"platform"`
	Server   struct {
		Role              string     `yaml:"role"`
		TrustedNetworks   []string   `yaml:"trusted_networks"`
		Addr              string     `yaml:"addr"`
		Domain            string     `yaml:"domain"`
		Banner            string     `yaml:"banner"` // SMTP 220 banner message (used by legacy SMTP only; main server uses Domain)
		TLS               *TLSConfig `yaml:"tls,omitempty"`
		MaxMessageBytes   int        `yaml:"max_message_bytes"`
		MaxRecipients     int        `yaml:"max_recipients"`
		AllowInsecureAuth bool       `yaml:"allow_insecure_auth"` // Allow AUTH over non-TLS
		RequireAuth       bool       `yaml:"require_auth"`        // Require authentication for all mail
		RequireTLS        bool       `yaml:"require_tls"`         // Require STARTTLS before AUTH
		Mode              string     `yaml:"mode"`                // test or prod

		// Connection and Rate Limiting
		MaxConnections int  `yaml:"max_connections"`   // Total concurrent connections (0 = unlimited)
		MaxPerIP       int  `yaml:"max_per_ip"`        // Connections per IP (0 = unlimited)
		RateLimitPerIP int  `yaml:"rate_limit_per_ip"` // Messages per hour per IP (0 = unlimited)
		EnableGreylist bool `yaml:"enable_greylist"`   // Enable greylisting
		DisableVRFY    bool `yaml:"disable_vrfy"`      // Disable VRFY command
		DisableEXPN    bool `yaml:"disable_expn"`      // Disable EXPN command

		// Command timeout applied by the active SMTP server.
		Timeouts TimeoutConfig `yaml:"timeouts"`

		// Local domains for delivery
		LocalDomains []string `yaml:"local_domains"` // Domains handled locally

		// Relay controls which non-local recipients this server will accept
		// and hand off to internal/delivery. See RelayConfig doc comment.
		Relay RelayConfig `yaml:"relay"`

		// IPFilter applies peer IP lists and DNSBL checks at session creation.
		IPFilter ipfilter.Config `yaml:"ip_filter"`

		// AuthMechanisms lists the SASL mechanisms advertised for SMTP AUTH.
		// Every entry must match one Session.Auth implements (currently just
		// "PLAIN") — LoadConfig rejects anything else at startup rather than
		// silently advertising a mechanism nothing can service.
		AuthMechanisms []string `yaml:"auth_mechanisms"`

		// Proxy Protocol Support
		ProxyProtocol ProxyProtocolConfig `yaml:"proxy_protocol"`

		// DANE Configuration (RFC 6698, RFC 7672)
		DANE DANEConfig `yaml:"dane"`

		// SPF Configuration (RFC 7208)
		SPF SPFPolicyConfig `yaml:"spf"`

		// DMARC Configuration (RFC 7489)
		DMARC DMARCPolicyConfig `yaml:"dmarc"`

		// DKIM signing of outbound mail (RFC 6376) — one entry per sending
		// domain. A shared mailhub serving multiple domains needs one key per
		// domain: DKIM's d= tag must match a domain that actually publishes
		// the corresponding public key, so domain A's key can never validly
		// sign as domain B.
		DKIM []DKIMSignConfig `yaml:"dkim"`

		// Restrictions and Policies
		DelayReject             bool `yaml:"delay_reject"`                // Delay rejection until RCPT TO (default: true)
		DelayOpenUntilValidRcpt bool `yaml:"delay_open_until_valid_rcpt"` // Don't open queue file until valid RCPT (default: true)
	} `yaml:"server"`

	// ContentFilter defines the trust boundary for a perimeter SMTP proxy such
	// as MailScript. The SMTP listener must be private when this is enabled.
	ContentFilter struct {
		Enabled              bool     `yaml:"enabled"`
		TrustedProxyNetworks []string `yaml:"trusted_proxy_networks"`
		QuarantineHeader     string   `yaml:"quarantine_header"`
		QuarantineFolder     string   `yaml:"quarantine_folder"`
	} `yaml:"content_filter"`

	IMAP struct {
		Disabled   bool       `yaml:"disabled"`
		Addr       string     `yaml:"addr"`
		TLS        *TLSConfig `yaml:"tls,omitempty"`
		RequireTLS bool       `yaml:"require_tls"` // Deprecated: require TLS before authentication
		TLSMode    string     `yaml:"tls_mode"`    // "starttls", "implicit", or "disabled"
	} `yaml:"imap"`

	JMAP struct {
		Enabled          bool   `yaml:"enabled"`             // Start the JMAP HTTP service
		Addr             string `yaml:"addr"`                // JMAP HTTP listen address
		JWTPublicKeyPath string `yaml:"jwt_public_key_path"` // PEM RSA/ECDSA public key for Bearer token validation
		JWTAudience      string `yaml:"jwt_audience"`
		JWTIssuer        string `yaml:"jwt_issuer"` // Expected iss claim (empty = skip check)
	} `yaml:"jmap"`

	API struct {
		OAuth         oauthaccess.Config `yaml:"oauth"`
		TLS           *TLSConfig         `yaml:"tls"`
		RESTAddr      string             `yaml:"rest_addr"`
		GRPCAddr      string             `yaml:"grpc_addr"`
		APIKeys       []APIKeyConfig     `yaml:"api_keys"`        // API keys for programmatic access
		AllowedIPs    []string           `yaml:"allowed_ips"`     // IP whitelist for API access
		RequireIPAuth bool               `yaml:"require_ip_auth"` // Require IP whitelist in addition to API key
	} `yaml:"api"`

	Auth struct {
		LDAP         LDAPConfig   `yaml:"ldap"`
		DefaultUsers []UserConfig `yaml:"default_users"`
		// UserDatabaseURL enables the persistent user store. Empty defaults
		// to platform.data_dir/users.db. A postgres:// URL uses PostgreSQL; anything
		// else is treated as a SQLite file path (e.g. ./data/users.db).
		// When set, default_users become bootstrap-only: they are created if
		// missing but never overwrite users managed via the admin API.
		// Startup fails closed if the database cannot be opened.
		UserDatabaseURL string `yaml:"user_database_url"`
		// Master (proxy) IMAP login for trusted control-plane services
		// (msgs.global mail-read gateway). Login form: "<user><sep><master_user>"
		// with master_password. Disabled unless user, password, AND a non-empty
		// IP allowlist are all configured — fail closed.
		MasterUser       string   `yaml:"master_user"`
		MasterPassword   string   `yaml:"master_password"`
		MasterAllowedIPs []string `yaml:"master_allowed_ips"`
		MasterSeparator  string   `yaml:"master_separator"` // default "*"
	} `yaml:"auth"`

	// SSO Configuration for external authentication providers
	SSO struct {
		Enabled      bool     `yaml:"enabled"`       // Enable SSO authentication
		Provider     string   `yaml:"provider"`      // Provider name (e.g., "afterdarksystems", "oidc", "oauth2")
		ClientID     string   `yaml:"client_id"`     // OAuth2 client ID
		ClientSecret string   `yaml:"client_secret"` // OAuth2 client secret
		AuthURL      string   `yaml:"auth_url"`      // Authorization endpoint
		TokenURL     string   `yaml:"token_url"`     // Token endpoint
		UserInfoURL  string   `yaml:"userinfo_url"`  // UserInfo endpoint
		RedirectURL  string   `yaml:"redirect_url"`  // OAuth2 redirect URL
		Scopes       []string `yaml:"scopes"`        // OAuth2 scopes
		// AfterDark Systems specific
		DirectoryURL string `yaml:"directory_url"` // Directory service URL (e.g., https://directory.afterdarksystems.com)
	} `yaml:"sso"`

	// AfterSMTP AMTP Next-Gen Protocol Integration
	AfterSMTP struct {
		Enabled    bool   `yaml:"enabled"`
		LedgerURL  string `yaml:"ledger_url"`
		QUICAddr   string `yaml:"quic_addr"`
		GRPCAddr   string `yaml:"grpc_addr"`
		FallbackDB string `yaml:"fallback_db"`
	} `yaml:"aftersmtp"`

	Logging struct {
		Level  string `yaml:"level"`  // debug, info, warn, error
		Format string `yaml:"format"` // json (default), yaml, syslog, console
	} `yaml:"logging"`

	// Elasticsearch Configuration for mail event logging and search
	Elasticsearch struct {
		Enabled       bool     `yaml:"enabled"`        // Enable Elasticsearch integration
		Endpoints     []string `yaml:"endpoints"`      // ES cluster endpoints
		IndexPrefix   string   `yaml:"index_prefix"`   // Index name prefix (e.g., "mail-events")
		BulkSize      int      `yaml:"bulk_size"`      // Bulk indexer batch size
		FlushInterval string   `yaml:"flush_interval"` // How often to flush bulk indexer (e.g., "5s")

		// Authentication
		APIKey   string `yaml:"api_key"`  // Elasticsearch API key
		Username string `yaml:"username"` // Basic auth username
		Password string `yaml:"password"` // Basic auth password

		// Index Lifecycle Management
		RetentionDays int `yaml:"retention_days"` // How long to keep indices
		Replicas      int `yaml:"replicas"`       // Number of replicas
		Shards        int `yaml:"shards"`         // Number of shards

		// Performance
		Workers      int     `yaml:"workers"`       // Number of bulk indexer workers
		SamplingRate float64 `yaml:"sampling_rate"` // Sample rate (0.0-1.0, 1.0 = all events)

		// Header Logging Configuration
		HeaderLogging HeaderLoggingConfig `yaml:"header_logging"`
	} `yaml:"elasticsearch"`
}

// HeaderLoggingConfig controls which message headers are logged to Elasticsearch
type HeaderLoggingConfig struct {
	Enabled       bool `yaml:"enabled"`         // Global enable/disable for header logging
	LogAllHeaders bool `yaml:"log_all_headers"` // Log all headers or only specific ones

	// Allowlist/Denylist by domain
	AllowDomains []string `yaml:"allow_domains"` // Domains to log headers for (empty = all)
	DenyDomains  []string `yaml:"deny_domains"`  // Domains to never log headers for

	// Allowlist/Denylist by IP
	AllowIPs []string `yaml:"allow_ips"` // IPs to log headers for (supports CIDR)
	DenyIPs  []string `yaml:"deny_ips"`  // IPs to never log headers for (supports CIDR)

	// Allowlist/Denylist by MX record
	AllowMXs []string `yaml:"allow_mxs"` // MX records to log headers for
	DenyMXs  []string `yaml:"deny_mxs"`  // MX records to never log headers for

	// Specific headers to include (if log_all_headers = false)
	IncludeHeaders []string `yaml:"include_headers"` // e.g., ["From", "To", "Subject", "Message-ID"]

	// Headers to always exclude (even if log_all_headers = true)
	ExcludeHeaders []string `yaml:"exclude_headers"` // e.g., ["Authorization", "X-Secret"]

	// Redaction patterns
	RedactPatterns []string `yaml:"redact_patterns"` // Regex patterns to redact from header values
}

// TimeoutConfig defines granular timeout settings for SMTP operations
type TimeoutConfig struct {
	Command string `yaml:"command"`
}

// ProxyProtocolConfig configures PROXY protocol support (HAProxy/nginx)
type ProxyProtocolConfig struct {
	Enabled  bool     `yaml:"enabled"`          // Enable PROXY protocol support
	Timeout  string   `yaml:"timeout"`          // Timeout for PROXY header (default: 5s)
	Networks []string `yaml:"trusted_networks"` // Trusted proxy networks (CIDR format)
}

type TLSConfig struct {
	ClientCAFile      string `yaml:"client_ca_file"`
	RequireClientCert bool   `yaml:"require_client_cert"`
	Cert              string `yaml:"cert"`
	Key               string `yaml:"key"`
}

type UserConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Email    string `yaml:"email"`
}

type APIKeyConfig struct {
	KeyFiles    []string  `yaml:"key_files"`
	ExpiresAt   time.Time `yaml:"expires_at"`
	KeyEnv      string    `yaml:"key_env"`
	Name        string    `yaml:"name"`        // Friendly name for the key (e.g., "Web Platform", "Mobile App")
	Key         string    `yaml:"key"`         // The actual API key
	Permissions []string  `yaml:"permissions"` // Optional permissions (for future RBAC)
	Description string    `yaml:"description"` // Optional description
}

// SPFPolicyConfig configures inbound SPF handling (RFC 7208).
//
// SPF is best consumed as an input to DMARC alignment rather than as a standalone
// reject gate: forwarding breaks envelope SPF, and operators frequently publish
// broken records (>10 DNS lookups, multiple records, stray `+all`). Standalone
// rejection is therefore opt-in.
//   - "monitor" (default): evaluate, log, and stamp the result; feed it to DMARC,
//     but never reject on SPF alone.
//   - "enforce": reject at MAIL FROM on SPF hardfail (-all), and on softfail too
//     if RejectOnSoftfail is set.
type SPFPolicyConfig struct {
	Enabled          bool   `yaml:"enabled"`            // Evaluate SPF for inbound mail
	Mode             string `yaml:"mode"`               // "monitor" (default) or "enforce"
	RejectOnSoftfail bool   `yaml:"reject_on_softfail"` // When enforcing, also reject on ~all softfail
}

// DMARCPolicyConfig configures inbound DMARC handling (RFC 7489).
//
// Two modes are supported so a deployment can observe before it enforces — the
// standard enterprise rollout path:
//   - "monitor": always evaluate, log, and stamp Authentication-Results, but
//     never reject or quarantine regardless of the sender's published p=.
//   - "enforce": honor the sender's published policy (p=reject → reject,
//     p=quarantine → route to QuarantineFolder).
type DMARCPolicyConfig struct {
	Enabled          bool   `yaml:"enabled"`           // Evaluate DMARC for inbound mail
	Mode             string `yaml:"mode"`              // "monitor" (default) or "enforce"
	QuarantineFolder string `yaml:"quarantine_folder"` // Folder for quarantined local mail (default "Junk")
}

// DKIMSignConfig configures DKIM signing of outbound mail for one sending
// domain (RFC 6376).
//
// Signing is opt-in per entry: it requires a private key to exist on disk,
// and it only ever signs messages whose envelope MAIL FROM domain matches
// Domain exactly — this server never signs mail on behalf of a domain it
// doesn't control, e.g. relayed or forwarded mail. On a shared mailhub
// there's one entry per domain the server sends as; each domain's DNS must
// publish the matching public key at Selector._domainkey.Domain.
type DKIMSignConfig struct {
	Enabled        bool   `yaml:"enabled"`          // Sign outbound mail for this domain
	Domain         string `yaml:"domain"`           // SDID to sign as — required, no fallback
	Selector       string `yaml:"selector"`         // DNS selector (default: "mail")
	PrivateKeyPath string `yaml:"private_key_path"` // PEM-encoded RSA or Ed25519 private key
}

// RelayConfig controls which RCPT TO recipients this server accepts.
//
// Threats: an open relay lets anyone on the internet use this server to send
// mail to arbitrary third parties — the server's reputation (and its IPs)
// get blacklisted within hours, and it becomes a spam cannon. Does NOT
// protect against an authenticated user or an allowed network abusing their
// own legitimate relay access; that's rate limiting's job.
//
// Local-domain recipients (server.local_domains) are always accepted — that
// is normal inbound delivery, not relay, and unauthenticated senders must be
// able to reach it (nobody authenticates to send you email). Every other
// recipient is relay, and is fail-closed: denied unless the sender
// authenticated via SMTP AUTH, or the client IP falls in AllowedNetworks
// (e.g. trusted internal application servers submitting outbound mail
// without per-connection SASL). AllowedNetworks is empty by default, so a
// fresh deployment permits zero unauthenticated relay.
type RelayConfig struct {
	AllowedNetworks []string `yaml:"allowed_networks"` // CIDRs permitted to relay to any destination without SMTP AUTH
}

// DANEConfig configures DANE (DNS-Based Authentication of Named Entities)
// RFC 6698, RFC 7672 - SMTP Security via DANE
type DANEConfig struct {
	Enabled    bool     `yaml:"enabled"`     // Enable DANE validation
	StrictMode bool     `yaml:"strict_mode"` // Reject delivery if DANE validation fails
	DNSServers []string `yaml:"dns_servers"` // DNS servers for DNSSEC queries (empty = use system defaults)
	CacheTTL   int      `yaml:"cache_ttl"`   // TLSA cache TTL in seconds (0 = use DNS TTL)
	Timeout    int      `yaml:"timeout"`     // DANE validation timeout in seconds
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	cfg.Platform.ValidateRecipients = true
	// Set defaults
	cfg.Server.Addr = ":2525"
	cfg.Server.Domain = "localhost"
	cfg.Server.Banner = "ESMTP Service Ready"
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
	cfg.Server.AuthMechanisms = []string{"PLAIN"}

	cfg.Server.Timeouts.Command = "300s"

	// Proxy Protocol defaults
	cfg.Server.ProxyProtocol.Enabled = false
	cfg.Server.ProxyProtocol.Timeout = "5s"
	cfg.Server.ProxyProtocol.Networks = []string{}

	// Restrictions
	cfg.Server.DelayReject = true
	cfg.Server.DelayOpenUntilValidRcpt = true

	cfg.Server.DANE.Enabled = true          // SECURITY: Enable DANE by default
	cfg.Server.DANE.StrictMode = false      // Default to opportunistic DANE
	cfg.Server.DANE.DNSServers = []string{} // Use system defaults
	cfg.Server.DANE.CacheTTL = 3600         // 1 hour cache
	cfg.Server.DANE.Timeout = 10            // 10 second timeout

	// SPF: evaluate by default, but in monitor mode — SPF feeds DMARC and is
	// stamped/logged, never a standalone reject. Forwarding and misconfigured
	// records make hard SPF rejection a deliverability hazard; it is opt-in.
	cfg.Server.SPF.Enabled = true
	cfg.Server.SPF.Mode = "monitor"
	cfg.Server.SPF.RejectOnSoftfail = false

	// DMARC: evaluate by default, but in monitor mode — observe and report before
	// enforcing. Operators opt into rejection/quarantine by setting mode: enforce.
	cfg.Server.DMARC.Enabled = true
	cfg.Server.DMARC.Mode = "monitor"
	cfg.Server.DMARC.QuarantineFolder = "Junk"

	// DKIM signing: no domains configured by default (each requires an
	// operator-provisioned private key) — see the post-unmarshal loop below
	// for the per-entry "mail" selector default.

	// Relay: SECURITY — fail closed. No networks are relay-authorized by
	// default; only SMTP-authenticated senders and server.local_domains
	// recipients are accepted until an operator opts a network in.
	cfg.Server.Relay.AllowedNetworks = []string{}

	cfg.IMAP.Addr = ":1143"
	cfg.IMAP.RequireTLS = true // SECURITY: Require TLS before authentication
	cfg.IMAP.TLSMode = "starttls"
	cfg.JMAP.Enabled = false
	cfg.JMAP.Addr = ":8443"

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

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("configuration must contain exactly one YAML document")
	}

	for i := range cfg.API.APIKeys {
		key := &cfg.API.APIKeys[i]
		if key.KeyEnv != "" {
			key.Key = os.Getenv(key.KeyEnv)
			if key.Key == "" {
				return nil, fmt.Errorf("missing API key environment variable %s", key.KeyEnv)
			}
		}
	}
	for i := range cfg.Server.DKIM {
		if cfg.Server.DKIM[i].Selector == "" {
			cfg.Server.DKIM[i].Selector = "mail"
		}
	}

	// Fail fast on a mechanism nothing implements, rather than advertising it
	// in EHLO and only discovering the gap when a client tries to use it.
	for _, mech := range cfg.Server.AuthMechanisms {
		if mech != "PLAIN" {
			return nil, fmt.Errorf("server.auth_mechanisms: unimplemented mechanism %q (supported: PLAIN)", mech)
		}
	}

	if err := cfg.Server.IPFilter.Validate(); err != nil {
		return nil, fmt.Errorf("server.ip_filter: %w", err)
	}
	if err := cfg.validatePlatform(); err != nil {
		return nil, err
	}
	if err := cfg.Auth.LDAP.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}
