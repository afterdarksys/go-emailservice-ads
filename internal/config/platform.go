package config

import (
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/extensions"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstorm"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/bounce"
	"github.com/afterdarksys/go-emailservice-ads/internal/compliance"
	"github.com/afterdarksys/go-emailservice-ads/internal/delivery"
)

// PlatformConfig contains active operational controls, shared by listeners.
type PlatformConfig struct {
	AdmissionPlugins           []extensions.Plugin     `yaml:"admission_plugins"`
	Webhooks                   []extensions.Webhook    `yaml:"webhooks"`
	DMARCReporting             bool                    `yaml:"dmarc_reporting"`
	Compliance                 compliance.Config       `yaml:"compliance"`
	Bounce                     bounce.Config           `yaml:"bounce"`
	FencingLeaseFile           string                  `yaml:"fencing_lease_file"`
	DestinationThrottle        delivery.ThrottleConfig `yaml:"destination_throttle"`
	ClamAVAddress              string                  `yaml:"clamav_address"`
	MalwareRequired            bool                    `yaml:"malware_required"`
	ReputationRejectBelow      int                     `yaml:"reputation_reject_below"`
	Mailstorm                  mailstorm.Config        `yaml:"mailstorm"`
	ReputationDatabaseEnv      string                  `yaml:"reputation_database_env"`
	RecipientDirectoryURL      string                  `yaml:"recipient_directory_url"`
	RecipientDirectoryTokenEnv string                  `yaml:"recipient_directory_token_env"`
	SubmissionRetentionDays    int                     `yaml:"submission_retention_days"`
	SieveRetentionDays         int                     `yaml:"sieve_retention_days"`
	QuarantineRetentionDays    int                     `yaml:"quarantine_retention_days"`
	ARC                        bool                    `yaml:"arc"`
	MTASTS                     bool                    `yaml:"mta_sts"`
	TLSReporting               bool                    `yaml:"tls_reporting"`
	DataDir                    string                  `yaml:"data_dir"`
	MaxSpoolBytes              int64                   `yaml:"max_spool_bytes"`
	MaxSpoolMessages           int                     `yaml:"max_spool_messages"`
	MinFreeBytes               uint64                  `yaml:"min_free_bytes"`
	MailboxQuotaBytes          int64                   `yaml:"mailbox_quota_bytes"`
	PolicyRequired             bool                    `yaml:"policy_required"`
	PolicyPath                 string                  `yaml:"policy_path"`
	ValidateRecipients         bool                    `yaml:"validate_recipients"`
	Aliases                    map[string][]string     `yaml:"aliases"`
	Transports                 []delivery.Route        `yaml:"transports"`
	Listeners                  []ListenerConfig        `yaml:"listeners"`
	MaxHops                    int                     `yaml:"max_hops"`
	UserRecipientsPerHour      int                     `yaml:"user_recipients_per_hour"`
	DomainRecipientsPerHour    int                     `yaml:"domain_recipients_per_hour"`
	ScannerURL                 string                  `yaml:"scanner_url"`
	ScannerRequired            bool                    `yaml:"scanner_required"`
	ScannerTimeout             string                  `yaml:"scanner_timeout"`
	ReputationURL              string                  `yaml:"reputation_url"`
	ReputationRequired         bool                    `yaml:"reputation_required"`
}

type ListenerConfig struct {
	Addr            string              `yaml:"addr"`
	Role            string              `yaml:"role"` // perimeter, submission, internal
	TLS             *TLSConfig          `yaml:"tls"`
	TrustedNetworks []string            `yaml:"trusted_networks"`
	ProxyProtocol   ProxyProtocolConfig `yaml:"proxy_protocol"`
}

func (c *Config) validatePlatform() error {
	p := &c.Platform
	if err := extensions.ValidatePlugins(p.AdmissionPlugins); err != nil {
		return err
	}
	if err := extensions.ValidateWebhooks(p.Webhooks); err != nil {
		return err
	}
	if c.API.AdminEnabled && c.API.TLS == nil {
		return fmt.Errorf("administration console requires API TLS")
	}
	if c.API.GRPCEnabled {
		if c.API.TLS == nil {
			return fmt.Errorf("management gRPC requires API TLS")
		}
		if _, _, err := net.SplitHostPort(c.API.GRPCAddr); err != nil {
			return fmt.Errorf("invalid management gRPC address: %w", err)
		}
	}
	if err := c.API.OAuth.Validate(); err != nil {
		return err
	}
	if c.API.OAuth.Enabled && c.API.TLS == nil {
		return fmt.Errorf("OAuth API access requires configured API TLS")
	}
	switch c.Logging.Format {
	case "", "json", "yaml", "syslog", "console":
	default:
		return fmt.Errorf("invalid logging format")
	}
	if err := p.Compliance.Validate(); err != nil {
		return err
	}
	if err := p.Bounce.Validate(); err != nil {
		return err
	}
	if err := p.DestinationThrottle.Validate(); err != nil {
		return err
	}
	if p.MalwareRequired && p.ClamAVAddress == "" {
		return fmt.Errorf("malware_required needs clamav_address")
	}
	if p.ClamAVAddress != "" {
		if _, _, err := net.SplitHostPort(p.ClamAVAddress); err != nil {
			return fmt.Errorf("invalid ClamAV address: %w", err)
		}
	}
	if p.ReputationRejectBelow < 0 || p.ReputationRejectBelow > 100 {
		return fmt.Errorf("reputation_reject_below must be 0..100")
	}
	if err := p.Mailstorm.Validate(); err != nil {
		return err
	}
	if env := os.Getenv("MAILHUB_DATA_DIR"); env != "" {
		p.DataDir = env
	}
	if p.DataDir == "" {
		p.DataDir = "./data"
	}
	p.DataDir = filepath.Clean(p.DataDir)
	if c.Auth.UserDatabaseURL == "" {
		c.Auth.UserDatabaseURL = filepath.Join(p.DataDir, "users.db")
	}
	if p.SubmissionRetentionDays < 0 || p.SubmissionRetentionDays > 36500 || p.SieveRetentionDays < 0 || p.SieveRetentionDays > 36500 {
		return fmt.Errorf("workflow retention days must be between 0 and 36500")
	}
	if p.QuarantineRetentionDays < 0 {
		return fmt.Errorf("quarantine retention must not be negative")
	}
	if p.PolicyPath == "" {
		p.PolicyPath = "policies.yaml"
	}
	if p.MaxHops == 0 {
		p.MaxHops = 30
	}
	if p.MaxSpoolBytes < 0 || p.MaxSpoolMessages < 0 || p.MailboxQuotaBytes < 0 || p.MaxHops < 1 || p.UserRecipientsPerHour < 0 || p.DomainRecipientsPerHour < 0 {
		return fmt.Errorf("platform limits must not be negative")
	}
	if p.ScannerTimeout == "" {
		p.ScannerTimeout = "15s"
	}
	if d, e := time.ParseDuration(p.ScannerTimeout); e != nil || d <= 0 {
		return fmt.Errorf("invalid scanner timeout")
	}
	if p.ARC && (!p.ScannerRequired || p.ScannerURL == "") {
		return fmt.Errorf("ARC requires a required Rspamd scanner")
	}
	if p.ARC {
		for _, key := range c.Server.DKIM {
			if key.Enabled {
				return fmt.Errorf("ARC deployments must configure DKIM signing in Rspamd")
			}
		}
	}
	if p.ScannerRequired && p.ScannerURL == "" {
		return fmt.Errorf("required scanner needs scanner_url")
	}
	if p.ReputationRequired && p.ReputationURL == "" && p.ReputationDatabaseEnv == "" {
		return fmt.Errorf("required reputation needs reputation_url")
	}
	if err := delivery.ValidateRoutes(p.Transports); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, l := range p.Listeners {
		if _, _, err := net.SplitHostPort(l.Addr); err != nil {
			return fmt.Errorf("listener address: %w", err)
		}
		if seen[l.Addr] {
			return fmt.Errorf("duplicate listener %s", l.Addr)
		}
		seen[l.Addr] = true
		switch l.Role {
		case "perimeter":
		case "submission", "internal":
			if l.TLS == nil || l.TLS.Cert == "" || l.TLS.Key == "" {
				return fmt.Errorf("%s listener requires TLS certificates", l.Role)
			}
		default:
			return fmt.Errorf("invalid listener role %q", l.Role)
		}
		if l.Role == "internal" && len(l.TrustedNetworks) == 0 {
			return fmt.Errorf("internal listener requires trusted_networks")
		}
		for _, n := range append(append([]string{}, l.TrustedNetworks...), l.ProxyProtocol.Networks...) {
			if _, _, e := net.ParseCIDR(n); e != nil {
				return fmt.Errorf("invalid trusted network %q", n)
			}
		}
		if l.ProxyProtocol.Enabled && len(l.ProxyProtocol.Networks) == 0 {
			return fmt.Errorf("PROXY protocol requires trusted networks")
		}
	}
	for alias, targets := range p.Aliases {
		if !strings.Contains(alias, "@") || len(targets) == 0 {
			return fmt.Errorf("invalid alias %q", alias)
		}
		for _, target := range targets {
			if !strings.Contains(target, "@") {
				return fmt.Errorf("invalid alias target %q", target)
			}
		}
	}
	return nil
}
