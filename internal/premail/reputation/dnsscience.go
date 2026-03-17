package reputation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/premail/scoring"
)

// DNSScienceFeed handles reputation data feeding to dnsscience.io
type DNSScienceFeed struct {
	logger    *zap.Logger
	config    *Config
	client    *http.Client
	repo      scoring.Repository
	ticker    *time.Ticker
	stopCh    chan struct{}
}

// Config holds configuration for dnsscience.io integration
type Config struct {
	Enabled      bool
	APIURL       string
	APIKey       string
	FeedInterval time.Duration
	BatchSize    int
}

// ReputationReport represents data sent to dnsscience.io
type ReputationReport struct {
	IP              string    `json:"ip"`
	ReputationClass string    `json:"reputation_class"`
	Score           int       `json:"score"`
	ViolationCount  int64     `json:"violation_count"`
	TotalConnections int64    `json:"total_connections"`
	LastSeen        time.Time `json:"last_seen"`
	Violations      []string  `json:"violations,omitempty"`
}

// NewDNSScienceFeed creates a new dnsscience.io feed client
func NewDNSScienceFeed(logger *zap.Logger, config *Config, repo scoring.Repository) *DNSScienceFeed {
	return &DNSScienceFeed{
		logger: logger,
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		repo:   repo,
		stopCh: make(chan struct{}),
	}
}

// Start begins periodic reputation feed
func (f *DNSScienceFeed) Start() {
	if !f.config.Enabled {
		f.logger.Info("dnsscience.io reputation feed disabled")
		return
	}

	f.logger.Info("Starting dnsscience.io reputation feed",
		zap.Duration("interval", f.config.FeedInterval))

	f.ticker = time.NewTicker(f.config.FeedInterval)

	go func() {
		for {
			select {
			case <-f.ticker.C:
				if err := f.sendReputationBatch(); err != nil {
					f.logger.Error("Failed to send reputation batch", zap.Error(err))
				}
			case <-f.stopCh:
				f.ticker.Stop()
				return
			}
		}
	}()
}

// Stop stops the reputation feed
func (f *DNSScienceFeed) Stop() {
	if f.ticker != nil {
		close(f.stopCh)
	}
}

// sendReputationBatch sends a batch of reputation data
func (f *DNSScienceFeed) sendReputationBatch() error {
	f.logger.Info("Collecting reputation data for feed")

	// Get top spammers from last hour
	hourBucket := time.Now().Add(-1 * time.Hour).Truncate(time.Hour)

	topSpammers, err := f.repo.GetTopSpammers(hourBucket, f.config.BatchSize)
	if err != nil {
		return fmt.Errorf("failed to get top spammers: %w", err)
	}

	if len(topSpammers) == 0 {
		f.logger.Debug("No spammers to report")
		return nil
	}

	reports := make([]ReputationReport, 0, len(topSpammers))

	for _, stats := range topSpammers {
		// Get detailed characteristics
		char, err := f.repo.GetIPCharacteristics(stats.IP)
		if err != nil {
			f.logger.Warn("Failed to get IP characteristics",
				zap.String("ip", stats.IP.String()),
				zap.Error(err))
			continue
		}

		report := ReputationReport{
			IP:              stats.IP.String(),
			ReputationClass: string(char.ReputationClass),
			Score:           char.CurrentScore,
			ViolationCount:  char.ViolationCount,
			TotalConnections: char.TotalConnections,
			LastSeen:        char.LastSeen,
			Violations:      f.buildViolationList(char),
		}

		reports = append(reports, report)
	}

	// Send to dnsscience.io
	if err := f.sendReports(reports); err != nil {
		return fmt.Errorf("failed to send reports: %w", err)
	}

	f.logger.Info("Sent reputation data to dnsscience.io",
		zap.Int("count", len(reports)))

	return nil
}

// sendReports sends reputation reports to dnsscience.io API
func (f *DNSScienceFeed) sendReports(reports []ReputationReport) error {
	if len(reports) == 0 {
		return nil
	}

	payload := map[string]interface{}{
		"reports":   reports,
		"source":    "adspremail",
		"timestamp": time.Now().Unix(),
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal reports: %w", err)
	}

	req, err := http.NewRequest("POST", f.config.APIURL+"/api/v1/reputation/bulk", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.config.APIKey)
	req.Header.Set("User-Agent", "ADS-PreMail/1.0")

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	f.logger.Debug("Successfully sent reputation data",
		zap.Int("status_code", resp.StatusCode))

	return nil
}

// buildViolationList builds a list of violation types for an IP
func (f *DNSScienceFeed) buildViolationList(char *scoring.IPCharacteristics) []string {
	violations := []string{}

	if char.PreBannerTalks > 0 {
		violations = append(violations, "pre_banner_talk")
	}

	if char.QuickDisconnects > 0 {
		violations = append(violations, "quick_disconnect")
	}

	if char.FailedAuthAttempts > 5 {
		violations = append(violations, "failed_auth")
	}

	if char.IsBlocklisted {
		violations = append(violations, "blocklisted")
	}

	return violations
}

// ReportIP immediately reports a single IP to dnsscience.io
func (f *DNSScienceFeed) ReportIP(ip net.IP, char *scoring.IPCharacteristics) error {
	if !f.config.Enabled {
		return nil
	}

	report := ReputationReport{
		IP:              ip.String(),
		ReputationClass: string(char.ReputationClass),
		Score:           char.CurrentScore,
		ViolationCount:  char.ViolationCount,
		TotalConnections: char.TotalConnections,
		LastSeen:        char.LastSeen,
		Violations:      f.buildViolationList(char),
	}

	return f.sendReports([]ReputationReport{report})
}

// QueryReputation queries dnsscience.io for IP reputation
func (f *DNSScienceFeed) QueryReputation(ip net.IP) (*ExternalReputation, error) {
	if !f.config.Enabled {
		return nil, nil
	}

	url := fmt.Sprintf("%s/api/v1/reputation/%s", f.config.APIURL, ip.String())

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+f.config.APIKey)
	req.Header.Set("User-Agent", "ADS-PreMail/1.0")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query reputation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // IP not in database
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var reputation ExternalReputation
	if err := json.NewDecoder(resp.Body).Decode(&reputation); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &reputation, nil
}

// ExternalReputation represents reputation data from dnsscience.io
type ExternalReputation struct {
	IP              string    `json:"ip"`
	ReputationScore int       `json:"reputation_score"`
	ThreatLevel     string    `json:"threat_level"`
	Categories      []string  `json:"categories"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	ReportCount     int       `json:"report_count"`
}

// GetReputationScore converts external reputation to our scoring system
func (r *ExternalReputation) GetReputationScore() int {
	// Map external reputation score (0-100) to our system
	// Higher external score = worse reputation

	switch r.ThreatLevel {
	case "critical":
		return 90 // High threat
	case "high":
		return 70
	case "medium":
		return 40
	case "low":
		return 10
	default:
		return 0
	}
}
