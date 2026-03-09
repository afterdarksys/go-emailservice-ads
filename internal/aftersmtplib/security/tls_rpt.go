package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RFC 8460 - SMTP TLS Reporting
// Provides visibility into TLS connectivity issues for email delivery

// TLSRPTReport represents an aggregate TLS report
// RFC 8460 Section 4 - Report Format
type TLSRPTReport struct {
	OrganizationName string         `json:"organization-name"`
	DateRange        DateRange      `json:"date-range"`
	ContactInfo      string         `json:"contact-info,omitempty"`
	ReportID         string         `json:"report-id"`
	Policies         []PolicyReport `json:"policies"`
}

// DateRange represents the time period for a report
type DateRange struct {
	StartDatetime time.Time `json:"start-datetime"`
	EndDatetime   time.Time `json:"end-datetime"`
}

// PolicyReport represents TLS policy results for a domain
type PolicyReport struct {
	Policy         Policy           `json:"policy"`
	Summary        Summary          `json:"summary"`
	FailureDetails []FailureDetails `json:"failure-details,omitempty"`
}

// Policy describes the TLS policy being reported on
type Policy struct {
	PolicyType   string   `json:"policy-type"` // "sts", "tlsa", "no-policy-found"
	PolicyString []string `json:"policy-string,omitempty"`
	PolicyDomain string   `json:"policy-domain"`
	MXHost       []string `json:"mx-host,omitempty"`
}

// Summary contains aggregate statistics
type Summary struct {
	TotalSuccessfulSessionCount int `json:"total-successful-session-count"`
	TotalFailureSessionCount    int `json:"total-failure-session-count"`
}

// FailureDetails describes TLS connection failures
// RFC 8460 Section 4.2 - Failure Details
type FailureDetails struct {
	ResultType          string  `json:"result-type"` // See RFC 8460 Section 4.2 for result types
	// Standard result types: "starttls-not-supported", "certificate-expired",
	// "certificate-not-trusted", "validation-failure", etc.
	// DANE-specific: "tlsa-invalid", "dnssec-invalid", "dane-required"
	SendingMTAIP        string  `json:"sending-mta-ip,omitempty"`
	ReceivingMXHostname string  `json:"receiving-mx-hostname,omitempty"`
	ReceivingMXHelo     string  `json:"receiving-mx-helo,omitempty"`
	ReceivingIP         string  `json:"receiving-ip,omitempty"`
	FailedSessionCount  int     `json:"failed-session-count"`
	AdditionalInfo      string  `json:"additional-information,omitempty"`
	FailureReasonCode   string  `json:"failure-reason-code,omitempty"`
}

// TLSRPTManager manages TLS reporting
type TLSRPTManager struct {
	logger          *zap.Logger
	organizationName string
	contactInfo     string
	httpClient      *http.Client

	// In-memory tracking of TLS events
	events map[string]*PolicyReport
	mu     sync.RWMutex

	// Reporting configuration
	reportInterval time.Duration
	reportTicker   *time.Ticker
	stopChan       chan struct{}
}

// NewTLSRPTManager creates a new TLS reporting manager
func NewTLSRPTManager(logger *zap.Logger, orgName, contactInfo string) *TLSRPTManager {
	return &TLSRPTManager{
		logger:           logger,
		organizationName: orgName,
		contactInfo:      contactInfo,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		events:         make(map[string]*PolicyReport),
		reportInterval: 24 * time.Hour, // Daily reports
		stopChan:       make(chan struct{}),
	}
}

// RecordSuccess records a successful TLS connection
func (t *TLSRPTManager) RecordSuccess(domain, mxHost, policyType string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := t.getKey(domain, policyType)
	if _, exists := t.events[key]; !exists {
		t.events[key] = &PolicyReport{
			Policy: Policy{
				PolicyType:   policyType,
				PolicyDomain: domain,
				MXHost:       []string{mxHost},
			},
			Summary: Summary{},
		}
	}

	t.events[key].Summary.TotalSuccessfulSessionCount++

	t.logger.Debug("TLS success recorded",
		zap.String("domain", domain),
		zap.String("mx", mxHost),
		zap.String("policy", policyType))
}

// RecordDANESuccess records a successful DANE validation
// RFC 8460 with DANE extensions
func (t *TLSRPTManager) RecordDANESuccess(domain, mxHost string, tlsaUsage uint8) {
	policyType := fmt.Sprintf("tlsa-usage-%d", tlsaUsage)
	t.RecordSuccess(domain, mxHost, policyType)

	t.logger.Info("DANE validation successful",
		zap.String("domain", domain),
		zap.String("mx", mxHost),
		zap.Uint8("tlsa_usage", tlsaUsage))
}

// RecordFailure records a TLS connection failure
func (t *TLSRPTManager) RecordFailure(domain, mxHost, policyType, resultType, additionalInfo string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := t.getKey(domain, policyType)
	if _, exists := t.events[key]; !exists {
		t.events[key] = &PolicyReport{
			Policy: Policy{
				PolicyType:   policyType,
				PolicyDomain: domain,
				MXHost:       []string{mxHost},
			},
			Summary: Summary{},
		}
	}

	t.events[key].Summary.TotalFailureSessionCount++

	// Find or create failure detail
	found := false
	for i := range t.events[key].FailureDetails {
		if t.events[key].FailureDetails[i].ResultType == resultType &&
			t.events[key].FailureDetails[i].ReceivingMXHostname == mxHost {
			t.events[key].FailureDetails[i].FailedSessionCount++
			found = true
			break
		}
	}

	if !found {
		t.events[key].FailureDetails = append(t.events[key].FailureDetails, FailureDetails{
			ResultType:          resultType,
			ReceivingMXHostname: mxHost,
			FailedSessionCount:  1,
			AdditionalInfo:      additionalInfo,
		})
	}

	t.logger.Warn("TLS failure recorded",
		zap.String("domain", domain),
		zap.String("mx", mxHost),
		zap.String("policy", policyType),
		zap.String("result", resultType))
}

// RecordDANEFailure records a DANE validation failure
// RFC 8460 with DANE-specific result types
func (t *TLSRPTManager) RecordDANEFailure(domain, mxHost, resultType, additionalInfo string) {
	// Use "tlsa" as policy type for DANE failures
	t.RecordFailure(domain, mxHost, "tlsa", resultType, additionalInfo)

	t.logger.Error("DANE validation failed",
		zap.String("domain", domain),
		zap.String("mx", mxHost),
		zap.String("result", resultType),
		zap.String("info", additionalInfo))
}

// getKey generates a unique key for event tracking
func (t *TLSRPTManager) getKey(domain, policyType string) string {
	return fmt.Sprintf("%s:%s", domain, policyType)
}

// GenerateReport generates a TLS-RPT report for the accumulated data
func (t *TLSRPTManager) GenerateReport(startTime, endTime time.Time) *TLSRPTReport {
	t.mu.RLock()
	defer t.mu.RUnlock()

	policies := make([]PolicyReport, 0, len(t.events))
	for _, policy := range t.events {
		policies = append(policies, *policy)
	}

	report := &TLSRPTReport{
		OrganizationName: t.organizationName,
		DateRange: DateRange{
			StartDatetime: startTime,
			EndDatetime:   endTime,
		},
		ContactInfo: t.contactInfo,
		ReportID:    fmt.Sprintf("%d", time.Now().Unix()),
		Policies:    policies,
	}

	return report
}

// SendReport sends a TLS-RPT report to the reporting endpoint
// RFC 8460 Section 3 - Reporting Policy Discovery via DNS TXT record
func (t *TLSRPTManager) SendReport(ctx context.Context, domain string, report *TLSRPTReport) error {
	// In production, you would:
	// 1. Query _smtp._tls.example.com TXT record to get reporting URI
	// 2. Send report to that URI (usually HTTPS endpoint or mailto:)
	//
	// For now, we'll demonstrate with a mock endpoint

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	t.logger.Info("Generated TLS-RPT report",
		zap.String("domain", domain),
		zap.String("report_id", report.ReportID),
		zap.Int("policy_count", len(report.Policies)))

	// Log the report for debugging
	t.logger.Debug("TLS-RPT report content", zap.String("json", string(reportJSON)))

	// In production, send to actual reporting endpoint
	// reportingURI := t.discoverReportingURI(domain)
	// return t.sendToEndpoint(ctx, reportingURI, reportJSON)

	return nil
}

// sendToEndpoint sends a report to an HTTPS endpoint
func (t *TLSRPTManager) sendToEndpoint(ctx context.Context, uri string, reportJSON []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", uri, bytes.NewReader(reportJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/tlsrpt+json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	t.logger.Info("TLS-RPT report sent successfully", zap.String("uri", uri))
	return nil
}

// StartReporting starts periodic report generation and sending
func (t *TLSRPTManager) StartReporting() {
	t.reportTicker = time.NewTicker(t.reportInterval)

	go func() {
		for {
			select {
			case <-t.reportTicker.C:
				t.generateAndSendReports()
			case <-t.stopChan:
				return
			}
		}
	}()

	t.logger.Info("TLS-RPT periodic reporting started",
		zap.Duration("interval", t.reportInterval))
}

// StopReporting stops periodic reporting
func (t *TLSRPTManager) StopReporting() {
	if t.reportTicker != nil {
		t.reportTicker.Stop()
	}
	close(t.stopChan)
	t.logger.Info("TLS-RPT reporting stopped")
}

// generateAndSendReports generates and sends reports for all tracked domains
func (t *TLSRPTManager) generateAndSendReports() {
	endTime := time.Now()
	startTime := endTime.Add(-t.reportInterval)

	report := t.GenerateReport(startTime, endTime)

	// Send reports for each unique domain
	domains := make(map[string]bool)
	for _, policy := range report.Policies {
		domains[policy.Policy.PolicyDomain] = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	for domain := range domains {
		if err := t.SendReport(ctx, domain, report); err != nil {
			t.logger.Error("Failed to send TLS-RPT report",
				zap.String("domain", domain),
				zap.Error(err))
		}
	}

	// Clear events after reporting
	t.mu.Lock()
	t.events = make(map[string]*PolicyReport)
	t.mu.Unlock()
}

// GetStats returns statistics about TLS events
func (t *TLSRPTManager) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	totalSuccess := 0
	totalFailure := 0
	domainCount := len(t.events)

	for _, policy := range t.events {
		totalSuccess += policy.Summary.TotalSuccessfulSessionCount
		totalFailure += policy.Summary.TotalFailureSessionCount
	}

	successRate := 0.0
	if totalSuccess+totalFailure > 0 {
		successRate = float64(totalSuccess) / float64(totalSuccess+totalFailure) * 100
	}

	return map[string]interface{}{
		"domains_tracked":  domainCount,
		"total_success":    totalSuccess,
		"total_failure":    totalFailure,
		"success_rate_pct": successRate,
	}
}
