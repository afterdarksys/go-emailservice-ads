package scoring

import (
	"math"
	"net"
	"time"

	"go.uber.org/zap"
)

// Engine is the composite scoring engine
type Engine struct {
	logger     *zap.Logger
	weights    ScoreWeights
	thresholds Thresholds
	repo       Repository // Database access
}

// Repository defines database operations for scoring
type Repository interface {
	GetIPCharacteristics(ip net.IP) (*IPCharacteristics, error)
	UpdateIPCharacteristics(char *IPCharacteristics) error
	GetHourlyStats(ip net.IP, hour time.Time) (*HourlyStats, error)
	UpdateHourlyStats(stats *HourlyStats) error
	IsInTopSpammers(ip net.IP) (bool, error)
	RecordConnectionEvent(ip net.IP, eventType string, score int, action Action, details map[string]interface{}) error
}

// NewEngine creates a new scoring engine
func NewEngine(logger *zap.Logger, repo Repository) *Engine {
	return &Engine{
		logger:     logger,
		weights:    DefaultScoreWeights(),
		thresholds: DefaultThresholds(),
		repo:       repo,
	}
}

// SetWeights allows customization of score weights
func (e *Engine) SetWeights(weights ScoreWeights) {
	e.weights = weights
}

// SetThresholds allows customization of action thresholds
func (e *Engine) SetThresholds(thresholds Thresholds) {
	e.thresholds = thresholds
}

// CalculateScore computes the composite score for a connection
func (e *Engine) CalculateScore(metrics *ConnectionMetrics) (*ScoringDecision, error) {
	components := ScoreComponents{}

	// Get historical characteristics
	char, err := e.repo.GetIPCharacteristics(metrics.IP)
	if err != nil {
		e.logger.Warn("Failed to get IP characteristics, treating as new IP",
			zap.String("ip", metrics.IP.String()),
			zap.Error(err))
		char = &IPCharacteristics{
			IP:              metrics.IP,
			FirstSeen:       metrics.ConnectedAt,
			LastSeen:        metrics.ConnectedAt,
			ReputationClass: ReputationUnknown,
		}
	}

	// 1. Protocol Violations (High Weight)
	components.PreBannerTalkScore = e.scorePreBannerTalk(metrics)
	components.InvalidCommandScore = e.scoreInvalidCommands(metrics)
	components.MalformedHeloScore = e.scoreMalformedHelo(metrics)
	components.MissingCommandsScore = e.scoreMissingCommands(metrics)

	// 2. Connection Patterns (Medium-High Weight)
	components.QuickDisconnectScore = e.scoreQuickDisconnect(metrics)
	components.FrequencyScore = e.scoreConnectionFrequency(char)
	components.NoAuthScore = e.scoreNoAuth(metrics)
	components.FailedAuthScore = e.scoreFailedAuth(metrics)

	// 3. Volume Anomalies (Medium Weight)
	components.RecipientCountScore = e.scoreRecipientCount(metrics)
	components.MessageRateScore = e.scoreMessageRate(metrics)
	components.MessageSizeScore = e.scoreMessageSize(metrics)
	components.RapidFireScore = e.scoreRapidFire(char)

	// 4. Historical Reputation (Persistent)
	components.PreviouslyFlaggedScore = e.scorePreviouslyFlagged(char)
	components.TopSpammerScore = e.scoreTopSpammer(metrics.IP)
	components.RecentViolationsScore = e.scoreRecentViolations(char)
	components.NewIPScore = e.scoreNewIP(char)
	components.KnownGoodScore = e.scoreKnownGood(char)

	// 5. Timing Patterns (Low Weight)
	components.BotTimingScore = e.scoreBotTiming(metrics)
	components.OffHoursScore = e.scoreOffHours(metrics)
	components.IdenticalTimingScore = e.scoreIdenticalTiming(metrics)

	// Calculate total score
	components.TotalScore = e.sumScoreComponents(&components)

	// Ensure score is within 0-100 range
	if components.TotalScore < 0 {
		components.TotalScore = 0
	}
	if components.TotalScore > 100 {
		components.TotalScore = 100
	}

	// Determine action based on thresholds
	action := e.determineAction(components.TotalScore)

	decision := &ScoringDecision{
		Score:      components.TotalScore,
		Action:     action,
		Components: components,
		Reason:     e.generateReason(&components),
		Timestamp:  time.Now(),
	}

	e.logger.Info("Calculated composite score",
		zap.String("ip", metrics.IP.String()),
		zap.Int("score", decision.Score),
		zap.String("action", string(decision.Action)),
		zap.String("reason", decision.Reason))

	// Record event
	details := map[string]interface{}{
		"score_components": components,
		"connection_duration": metrics.ConnectionDuration.Seconds(),
	}
	if err := e.repo.RecordConnectionEvent(metrics.IP, "score_calculated", decision.Score, action, details); err != nil {
		e.logger.Warn("Failed to record connection event", zap.Error(err))
	}

	return decision, nil
}

// Protocol Violations Scoring

func (e *Engine) scorePreBannerTalk(metrics *ConnectionMetrics) int {
	if metrics.PreBannerTalk {
		return e.weights.PreBannerTalk // Instant 100 = DROP
	}
	return 0
}

func (e *Engine) scoreInvalidCommands(metrics *ConnectionMetrics) int {
	if metrics.InvalidCommands > 0 {
		return e.weights.InvalidCommand
	}
	return 0
}

func (e *Engine) scoreMalformedHelo(metrics *ConnectionMetrics) int {
	if metrics.HeloMalformed {
		return e.weights.MalformedHelo
	}
	return 0
}

func (e *Engine) scoreMissingCommands(metrics *ConnectionMetrics) int {
	// If they sent messages but never said HELO/EHLO
	if metrics.MessagesAttempted > 0 && !metrics.HeloProvided {
		return e.weights.MissingCommands
	}
	return 0
}

// Connection Patterns Scoring

func (e *Engine) scoreQuickDisconnect(metrics *ConnectionMetrics) int {
	if metrics.QuickDisconnect {
		return e.weights.QuickDisconnect
	}
	return 0
}

func (e *Engine) scoreConnectionFrequency(char *IPCharacteristics) int {
	// Calculate connections in the last hour
	hourAgo := time.Now().Add(-1 * time.Hour)
	if char.LastSeen.After(hourAgo) {
		// Simple heuristic: if we've seen many connections recently
		if char.TotalConnections > 100 {
			return e.weights.FrequencySpike
		}
	}
	return 0
}

func (e *Engine) scoreNoAuth(metrics *ConnectionMetrics) int {
	// Sending messages without authentication
	if metrics.MessagesAttempted > 0 && !metrics.AuthAttempted {
		return e.weights.NoAuthSubmission
	}
	return 0
}

func (e *Engine) scoreFailedAuth(metrics *ConnectionMetrics) int {
	if metrics.AuthFailureCount > 0 {
		score := metrics.AuthFailureCount * e.weights.FailedAuthPer
		if score > e.weights.FailedAuthMax {
			score = e.weights.FailedAuthMax
		}
		return score
	}
	return 0
}

// Volume Anomalies Scoring

func (e *Engine) scoreRecipientCount(metrics *ConnectionMetrics) int {
	if metrics.RecipientsCount > 50 {
		return e.weights.HighRecipientCount
	}
	return 0
}

func (e *Engine) scoreMessageRate(metrics *ConnectionMetrics) int {
	if metrics.MessagesAttempted > 10 {
		return e.weights.HighMessagePerConn
	}
	return 0
}

func (e *Engine) scoreMessageSize(metrics *ConnectionMetrics) int {
	if metrics.AverageMessageSize < 100 || metrics.AverageMessageSize > 10*1024*1024 {
		return e.weights.UnusualMessageSize
	}
	return 0
}

func (e *Engine) scoreRapidFire(char *IPCharacteristics) int {
	// If average connection time is very short
	if char.AverageConnectionTime < 1*time.Second && char.TotalConnections > 10 {
		return e.weights.RapidFire
	}
	return 0
}

// Historical Reputation Scoring

func (e *Engine) scorePreviouslyFlagged(char *IPCharacteristics) int {
	if char.ReputationClass == ReputationSpammer || char.IsBlocklisted {
		return e.weights.PreviouslyFlagged
	}
	return 0
}

func (e *Engine) scoreTopSpammer(ip net.IP) int {
	isTop, err := e.repo.IsInTopSpammers(ip)
	if err != nil {
		e.logger.Warn("Failed to check top spammers", zap.Error(err))
		return 0
	}
	if isTop {
		return e.weights.TopSpammer
	}
	return 0
}

func (e *Engine) scoreRecentViolations(char *IPCharacteristics) int {
	// Check if there were recent violations (last 24h)
	if char.LastViolation != nil {
		if time.Since(*char.LastViolation) < 24*time.Hour {
			return e.weights.RecentViolations
		}
	}
	return 0
}

func (e *Engine) scoreNewIP(char *IPCharacteristics) int {
	// Brand new IP (first time seen)
	if char.TotalConnections == 0 {
		return e.weights.NewIP
	}
	return 0
}

func (e *Engine) scoreKnownGood(char *IPCharacteristics) int {
	if char.ReputationClass == ReputationClean {
		return e.weights.KnownGood // Negative score!
	}
	return 0
}

// Timing Patterns Scoring

func (e *Engine) scoreBotTiming(metrics *ConnectionMetrics) int {
	// Calculate variance in command timings
	if metrics.TimingVariance < 0.01 && len(metrics.CommandTimings) > 5 {
		// Very low variance = bot-like behavior
		return e.weights.BotLikeTiming
	}
	return 0
}

func (e *Engine) scoreOffHours(metrics *ConnectionMetrics) int {
	hour := metrics.ConnectedAt.UTC().Hour()
	// Off-hours: 2am-6am UTC
	if hour >= 2 && hour < 6 {
		if metrics.MessagesAttempted > 5 {
			return e.weights.OffHoursSending
		}
	}
	return 0
}

func (e *Engine) scoreIdenticalTiming(metrics *ConnectionMetrics) int {
	// Check if command timings are suspiciously identical
	if len(metrics.CommandTimings) < 3 {
		return 0
	}

	// Calculate if timings are nearly identical
	identical := true
	firstTiming := metrics.CommandTimings[0]
	for _, timing := range metrics.CommandTimings[1:] {
		diff := math.Abs(float64(timing - firstTiming))
		if diff > float64(10*time.Millisecond) {
			identical = false
			break
		}
	}

	if identical {
		return e.weights.IdenticalTiming
	}
	return 0
}

// Helper functions

func (e *Engine) sumScoreComponents(components *ScoreComponents) int {
	return components.PreBannerTalkScore +
		components.InvalidCommandScore +
		components.MalformedHeloScore +
		components.MissingCommandsScore +
		components.QuickDisconnectScore +
		components.FrequencyScore +
		components.NoAuthScore +
		components.FailedAuthScore +
		components.RecipientCountScore +
		components.MessageRateScore +
		components.MessageSizeScore +
		components.RapidFireScore +
		components.PreviouslyFlaggedScore +
		components.TopSpammerScore +
		components.RecentViolationsScore +
		components.NewIPScore +
		components.KnownGoodScore + // Can be negative!
		components.BotTimingScore +
		components.OffHoursScore +
		components.IdenticalTimingScore
}

func (e *Engine) determineAction(score int) Action {
	if score >= e.thresholds.Drop {
		return ActionDrop
	}
	if score >= e.thresholds.Tarpit {
		return ActionTarpit
	}
	if score >= e.thresholds.Throttle {
		return ActionThrottle
	}
	if score >= e.thresholds.Monitor {
		return ActionMonitor
	}
	return ActionAllow
}

func (e *Engine) generateReason(components *ScoreComponents) string {
	reasons := []string{}

	if components.PreBannerTalkScore > 0 {
		reasons = append(reasons, "pre-banner talk")
	}
	if components.InvalidCommandScore > 0 {
		reasons = append(reasons, "invalid SMTP commands")
	}
	if components.QuickDisconnectScore > 0 {
		reasons = append(reasons, "quick disconnect pattern")
	}
	if components.TopSpammerScore > 0 {
		reasons = append(reasons, "in hourly top spammers")
	}
	if components.PreviouslyFlaggedScore > 0 {
		reasons = append(reasons, "previously flagged")
	}
	if components.FailedAuthScore > 0 {
		reasons = append(reasons, "failed authentication")
	}
	if components.RecipientCountScore > 0 {
		reasons = append(reasons, "high recipient count")
	}
	if components.BotTimingScore > 0 {
		reasons = append(reasons, "bot-like timing")
	}

	if len(reasons) == 0 {
		return "composite score evaluation"
	}

	// Join first 3 reasons
	if len(reasons) > 3 {
		reasons = reasons[:3]
	}

	result := ""
	for i, r := range reasons {
		if i > 0 {
			result += ", "
		}
		result += r
	}
	return result
}

// UpdateMetrics updates IP characteristics based on current connection
func (e *Engine) UpdateMetrics(metrics *ConnectionMetrics, decision *ScoringDecision) error {
	char, err := e.repo.GetIPCharacteristics(metrics.IP)
	if err != nil {
		// Create new characteristics
		char = &IPCharacteristics{
			IP:        metrics.IP,
			FirstSeen: metrics.ConnectedAt,
		}
	}

	// Update counts
	char.LastSeen = time.Now()
	char.TotalConnections++

	if metrics.QuickDisconnect {
		char.QuickDisconnects++
	}
	if metrics.PreBannerTalk {
		char.PreBannerTalks++
	}

	char.MessagesSent += int64(metrics.MessagesAttempted)
	char.RecipientsCount += int64(metrics.RecipientsCount)
	char.FailedAuthAttempts += int64(metrics.AuthFailureCount)

	// Update average connection time
	if char.TotalConnections > 0 {
		char.AverageConnectionTime = time.Duration(
			(int64(char.AverageConnectionTime)*char.TotalConnections +
				int64(metrics.ConnectionDuration)) /
				(char.TotalConnections + 1),
		)
	} else {
		char.AverageConnectionTime = metrics.ConnectionDuration
	}

	// Update scoring
	char.CurrentScore = decision.Score
	if decision.Score > char.MaxScore7d {
		char.MaxScore7d = decision.Score
	}

	// Update reputation class
	switch decision.Action {
	case ActionDrop:
		char.ReputationClass = ReputationBlocklist
		char.IsBlocklisted = true
		expires := time.Now().Add(24 * time.Hour)
		char.BlocklistExpires = &expires
		char.ViolationCount++
		now := time.Now()
		char.LastViolation = &now
	case ActionTarpit:
		if char.ReputationClass != ReputationBlocklist {
			char.ReputationClass = ReputationSpammer
		}
	case ActionThrottle:
		if char.ReputationClass == ReputationUnknown || char.ReputationClass == ReputationClean {
			char.ReputationClass = ReputationSuspicious
		}
	case ActionAllow:
		if char.TotalConnections > 10 && char.ViolationCount == 0 {
			char.ReputationClass = ReputationClean
		}
	}

	// Save to database
	if err := e.repo.UpdateIPCharacteristics(char); err != nil {
		return err
	}

	// Update hourly stats
	hourBucket := metrics.ConnectedAt.Truncate(time.Hour)
	stats, err := e.repo.GetHourlyStats(metrics.IP, hourBucket)
	if err != nil {
		stats = &HourlyStats{
			HourBucket: hourBucket,
			IP:         metrics.IP,
		}
	}

	stats.ConnectionCount++
	stats.MessageCount += int64(metrics.MessagesAttempted)
	if decision.Score > 50 {
		stats.ViolationCount++
	}
	stats.MaxScore = max(stats.MaxScore, decision.Score)

	// Recalculate average
	if stats.ConnectionCount > 0 {
		stats.AvgScore = (stats.AvgScore*float64(stats.ConnectionCount-1) +
			float64(decision.Score)) / float64(stats.ConnectionCount)
	} else {
		stats.AvgScore = float64(decision.Score)
	}

	if err := e.repo.UpdateHourlyStats(stats); err != nil {
		return err
	}

	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
