package analyzer

import (
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/premail/scoring"
)

// Analyzer performs real-time behavioral analysis on SMTP connections
type Analyzer struct {
	logger               *zap.Logger
	config               *Config

	// Connection pattern detection
	quickDisconnectThreshold time.Duration
	hourlyConnectionLimit    int
}

// Config holds analyzer configuration
type Config struct {
	PreBannerTimeout         time.Duration
	QuickDisconnectThreshold time.Duration
	HourlyConnectionLimit    int
	BotTimingThreshold       float64 // Variance threshold
}

// NewAnalyzer creates a new connection analyzer
func NewAnalyzer(logger *zap.Logger, config *Config) *Analyzer {
	return &Analyzer{
		logger:               logger,
		config:               config,
		quickDisconnectThreshold: config.QuickDisconnectThreshold,
		hourlyConnectionLimit:    config.HourlyConnectionLimit,
	}
}

// AnalyzePreBanner detects pre-banner talking
func (a *Analyzer) AnalyzePreBanner(ip net.IP, data []byte, connTime time.Time) *PreBannerAnalysis {
	analysis := &PreBannerAnalysis{
		IP:              ip,
		AnalyzedAt:      time.Now(),
		PreBannerTalk:   len(data) > 0,
		DataLength:      len(data),
		TimeSinceConnect: time.Since(connTime),
	}

	if analysis.PreBannerTalk {
		a.logger.Warn("Pre-banner talk detected",
			zap.String("ip", ip.String()),
			zap.Int("data_length", analysis.DataLength),
			zap.Duration("time_since_connect", analysis.TimeSinceConnect))

		// Try to parse what they sent
		analysis.ParsedData = string(data)
	}

	return analysis
}

// AnalyzeConnectionPattern detects quick connect/disconnect patterns
func (a *Analyzer) AnalyzeConnectionPattern(metrics *scoring.ConnectionMetrics) *ConnectionPatternAnalysis {
	analysis := &ConnectionPatternAnalysis{
		IP:                metrics.IP,
		AnalyzedAt:        time.Now(),
		ConnectionDuration: metrics.ConnectionDuration,
		QuickDisconnect:   metrics.ConnectionDuration < a.quickDisconnectThreshold,
	}

	// Analyze command frequency
	if len(metrics.CommandTimings) > 0 {
		var totalTime time.Duration
		for _, timing := range metrics.CommandTimings {
			totalTime += timing
		}
		analysis.AvgCommandInterval = totalTime / time.Duration(len(metrics.CommandTimings))
		analysis.CommandCount = len(metrics.CommandTimings)
	}

	// Calculate command rate (commands per second)
	if metrics.ConnectionDuration > 0 {
		analysis.CommandRate = float64(metrics.SMTPCommandsIssued) / metrics.ConnectionDuration.Seconds()
	}

	// Detect rapid-fire connections (>10 commands/sec)
	if analysis.CommandRate > 10.0 {
		analysis.RapidFire = true
		a.logger.Warn("Rapid-fire command pattern detected",
			zap.String("ip", metrics.IP.String()),
			zap.Float64("command_rate", analysis.CommandRate))
	}

	return analysis
}

// AnalyzeTimingPattern detects bot-like timing patterns
func (a *Analyzer) AnalyzeTimingPattern(metrics *scoring.ConnectionMetrics) *TimingPatternAnalysis {
	analysis := &TimingPatternAnalysis{
		IP:         metrics.IP,
		AnalyzedAt: time.Now(),
		SampleSize: len(metrics.CommandTimings),
	}

	if len(metrics.CommandTimings) < 3 {
		return analysis // Not enough data
	}

	// Calculate mean
	var sum time.Duration
	for _, t := range metrics.CommandTimings {
		sum += t
	}
	mean := sum / time.Duration(len(metrics.CommandTimings))
	analysis.MeanInterval = mean

	// Calculate variance
	var variance float64
	for _, t := range metrics.CommandTimings {
		diff := float64(t - mean)
		variance += diff * diff
	}
	variance /= float64(len(metrics.CommandTimings))
	analysis.Variance = variance

	// Calculate standard deviation
	stdDev := time.Duration(int64(variance))
	analysis.StdDeviation = stdDev

	// Detect bot-like behavior (very low variance)
	if variance < a.config.BotTimingThreshold {
		analysis.BotLike = true
		a.logger.Warn("Bot-like timing pattern detected",
			zap.String("ip", metrics.IP.String()),
			zap.Float64("variance", variance),
			zap.Duration("mean", mean))
	}

	// Detect identical timing (all timings within 10ms)
	identical := true
	if len(metrics.CommandTimings) > 0 {
		first := metrics.CommandTimings[0]
		for _, t := range metrics.CommandTimings[1:] {
			if abs(int64(t-first)) > int64(10*time.Millisecond) {
				identical = false
				break
			}
		}
	}
	analysis.IdenticalTiming = identical

	if identical {
		a.logger.Warn("Identical timing pattern detected",
			zap.String("ip", metrics.IP.String()),
			zap.Int("sample_size", len(metrics.CommandTimings)))
	}

	return analysis
}

// AnalyzeProtocolCompliance checks SMTP protocol compliance
func (a *Analyzer) AnalyzeProtocolCompliance(metrics *scoring.ConnectionMetrics) *ProtocolAnalysis {
	analysis := &ProtocolAnalysis{
		IP:         metrics.IP,
		AnalyzedAt: time.Now(),
		Valid:      true,
	}

	// Check for HELO/EHLO
	if metrics.SMTPCommandsIssued > 0 && !metrics.HeloProvided {
		analysis.Valid = false
		analysis.Violations = append(analysis.Violations, "Missing HELO/EHLO")
	}

	if metrics.HeloMalformed {
		analysis.Valid = false
		analysis.Violations = append(analysis.Violations, "Malformed HELO/EHLO")
	}

	// Check for invalid commands
	if metrics.InvalidCommands > 0 {
		analysis.Valid = false
		analysis.Violations = append(analysis.Violations,
			fmt.Sprintf("%d invalid SMTP commands", metrics.InvalidCommands))
	}

	// Check for authentication violations
	if metrics.MessagesAttempted > 0 && !metrics.AuthAttempted {
		analysis.Violations = append(analysis.Violations,
			"Messages sent without authentication attempt")
	}

	if metrics.AuthFailureCount > 0 {
		analysis.Violations = append(analysis.Violations,
			fmt.Sprintf("%d failed authentication attempts", metrics.AuthFailureCount))
	}

	if len(analysis.Violations) > 0 {
		a.logger.Warn("SMTP protocol violations detected",
			zap.String("ip", metrics.IP.String()),
			zap.Strings("violations", analysis.Violations))
	}

	return analysis
}

// AnalyzeVolumeAnomaly detects volume-based anomalies
func (a *Analyzer) AnalyzeVolumeAnomaly(metrics *scoring.ConnectionMetrics) *VolumeAnalysis {
	analysis := &VolumeAnalysis{
		IP:              metrics.IP,
		AnalyzedAt:      time.Now(),
		MessageCount:    metrics.MessagesAttempted,
		RecipientCount:  metrics.RecipientsCount,
		AvgMessageSize:  metrics.AverageMessageSize,
	}

	// High recipient count
	if metrics.RecipientsCount > 50 {
		analysis.Anomalies = append(analysis.Anomalies,
			fmt.Sprintf("High recipient count: %d", metrics.RecipientsCount))
	}

	// High message rate
	if metrics.MessagesAttempted > 10 {
		analysis.Anomalies = append(analysis.Anomalies,
			fmt.Sprintf("High message count: %d", metrics.MessagesAttempted))
	}

	// Unusual message sizes
	if metrics.AverageMessageSize < 100 {
		analysis.Anomalies = append(analysis.Anomalies,
			fmt.Sprintf("Unusually small messages: %d bytes avg", metrics.AverageMessageSize))
	}

	if metrics.AverageMessageSize > 10*1024*1024 {
		analysis.Anomalies = append(analysis.Anomalies,
			fmt.Sprintf("Unusually large messages: %d bytes avg", metrics.AverageMessageSize))
	}

	if len(analysis.Anomalies) > 0 {
		analysis.Suspicious = true
		a.logger.Warn("Volume anomalies detected",
			zap.String("ip", metrics.IP.String()),
			zap.Strings("anomalies", analysis.Anomalies))
	}

	return analysis
}

// AnalyzeTemporalPattern detects time-based patterns
func (a *Analyzer) AnalyzeTemporalPattern(metrics *scoring.ConnectionMetrics) *TemporalAnalysis {
	analysis := &TemporalAnalysis{
		IP:             metrics.IP,
		AnalyzedAt:     time.Now(),
		ConnectionTime: metrics.ConnectedAt,
		Hour:           metrics.ConnectedAt.UTC().Hour(),
	}

	// Off-hours detection (2am-6am UTC)
	if analysis.Hour >= 2 && analysis.Hour < 6 {
		analysis.OffHours = true
		if metrics.MessagesAttempted > 5 {
			analysis.BulkOffHours = true
			a.logger.Warn("Off-hours bulk sending detected",
				zap.String("ip", metrics.IP.String()),
				zap.Int("hour", analysis.Hour),
				zap.Int("messages", metrics.MessagesAttempted))
		}
	}

	return analysis
}

// Analysis result types

type PreBannerAnalysis struct {
	IP                net.IP
	AnalyzedAt        time.Time
	PreBannerTalk     bool
	DataLength        int
	TimeSinceConnect  time.Duration
	ParsedData        string
}

type ConnectionPatternAnalysis struct {
	IP                  net.IP
	AnalyzedAt          time.Time
	ConnectionDuration  time.Duration
	QuickDisconnect     bool
	RapidFire           bool
	CommandCount        int
	CommandRate         float64
	AvgCommandInterval  time.Duration
}

type TimingPatternAnalysis struct {
	IP              net.IP
	AnalyzedAt      time.Time
	SampleSize      int
	MeanInterval    time.Duration
	Variance        float64
	StdDeviation    time.Duration
	BotLike         bool
	IdenticalTiming bool
}

type ProtocolAnalysis struct {
	IP         net.IP
	AnalyzedAt time.Time
	Valid      bool
	Violations []string
}

type VolumeAnalysis struct {
	IP             net.IP
	AnalyzedAt     time.Time
	MessageCount   int
	RecipientCount int
	AvgMessageSize int64
	Suspicious     bool
	Anomalies      []string
}

type TemporalAnalysis struct {
	IP             net.IP
	AnalyzedAt     time.Time
	ConnectionTime time.Time
	Hour           int
	OffHours       bool
	BulkOffHours   bool
}

// Helper functions

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
