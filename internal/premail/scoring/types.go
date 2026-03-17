package scoring

import (
	"net"
	"time"
)

// ConnectionMetrics holds real-time metrics for a connection
type ConnectionMetrics struct {
	// Identity
	IP            net.IP
	ConnectedAt   time.Time
	DisconnectedAt *time.Time

	// Connection behavior
	PreBannerTalk         bool
	QuickDisconnect       bool  // Disconnected within 2 seconds
	ConnectionDuration    time.Duration
	BytesReceived         int64
	BytesSent             int64

	// SMTP protocol
	SMTPCommandsIssued    int
	InvalidCommands       int
	HeloProvided          bool
	HeloMalformed         bool
	AuthAttempted         bool
	AuthFailed            bool
	AuthFailureCount      int

	// Message metrics
	MessagesAttempted     int
	RecipientsCount       int
	AverageMessageSize    int64

	// Timing patterns
	CommandTimings        []time.Duration
	TimingVariance        float64 // Variance in command timings (bot detection)
}

// IPCharacteristics holds historical data for an IP address
type IPCharacteristics struct {
	IP                     net.IP
	FirstSeen              time.Time
	LastSeen               time.Time

	// Connection metrics
	TotalConnections       int64
	QuickDisconnects       int64
	PreBannerTalks         int64
	AverageConnectionTime  time.Duration

	// Volume metrics
	MessagesSent           int64
	RecipientsCount        int64
	FailedAuthAttempts     int64

	// Scoring
	CurrentScore           int
	MaxScore7d             int // Highest score in last 7 days
	ViolationCount         int64

	// Reputation
	ReputationClass        ReputationClass
	IsBlocklisted          bool
	BlocklistExpires       *time.Time

	// Metadata
	LastViolation          *time.Time
	Notes                  string
}

// ReputationClass categorizes IPs
type ReputationClass string

const (
	ReputationUnknown   ReputationClass = "unknown"
	ReputationClean     ReputationClass = "clean"
	ReputationSuspicious ReputationClass = "suspicious"
	ReputationSpammer   ReputationClass = "spammer"
	ReputationBlocklist ReputationClass = "blocklisted"
)

// ScoreComponents breaks down the composite score
type ScoreComponents struct {
	// Protocol violations (high weight)
	PreBannerTalkScore     int `json:"pre_banner_talk"`
	InvalidCommandScore    int `json:"invalid_commands"`
	MalformedHeloScore     int `json:"malformed_helo"`
	MissingCommandsScore   int `json:"missing_commands"`

	// Connection patterns (medium-high weight)
	QuickDisconnectScore   int `json:"quick_disconnect"`
	FrequencyScore         int `json:"connection_frequency"`
	NoAuthScore            int `json:"no_auth"`
	FailedAuthScore        int `json:"failed_auth"`

	// Volume anomalies (medium weight)
	RecipientCountScore    int `json:"recipient_count"`
	MessageRateScore       int `json:"message_rate"`
	MessageSizeScore       int `json:"message_size"`
	RapidFireScore         int `json:"rapid_fire"`

	// Historical reputation (persistent)
	PreviouslyFlaggedScore int `json:"previously_flagged"`
	TopSpammerScore        int `json:"top_spammer"`
	RecentViolationsScore  int `json:"recent_violations"`
	NewIPScore             int `json:"new_ip"`
	KnownGoodScore         int `json:"known_good"` // Negative score

	// Timing patterns (low weight)
	BotTimingScore         int `json:"bot_timing"`
	OffHoursScore          int `json:"off_hours"`
	IdenticalTimingScore   int `json:"identical_timing"`

	// Total
	TotalScore             int `json:"total_score"`
}

// ScoringDecision represents the action to take
type ScoringDecision struct {
	Score      int
	Action     Action
	Components ScoreComponents
	Reason     string
	Timestamp  time.Time
}

// Action defines what to do with a connection
type Action string

const (
	ActionAllow    Action = "allow"     // 0-30: Clean traffic
	ActionMonitor  Action = "monitor"   // 31-50: Suspicious
	ActionThrottle Action = "throttle"  // 51-70: Likely spam
	ActionTarpit   Action = "tarpit"    // 71-90: High confidence spam
	ActionDrop     Action = "drop"      // 91-100: Definite spam
)

// Thresholds for scoring decisions
type Thresholds struct {
	Allow    int // 0-30
	Monitor  int // 31-50
	Throttle int // 51-70
	Tarpit   int // 71-90
	Drop     int // 91-100
}

// DefaultThresholds returns the default scoring thresholds
func DefaultThresholds() Thresholds {
	return Thresholds{
		Allow:    30,
		Monitor:  50,
		Throttle: 70,
		Tarpit:   90,
		Drop:     91,
	}
}

// ScoreWeights defines the point values for each violation type
type ScoreWeights struct {
	// Protocol violations (instant actions)
	PreBannerTalk         int
	InvalidCommand        int
	MalformedHelo         int
	MissingCommands       int

	// Connection patterns
	QuickDisconnect       int
	FrequencySpike        int
	NoAuthSubmission      int
	FailedAuthPer         int
	FailedAuthMax         int
	MultipleRecipNoAuth   int

	// Volume anomalies
	HighRecipientCount    int
	HighMessagePerConn    int
	UnusualMessageSize    int
	RapidFire             int

	// Historical reputation
	PreviouslyFlagged     int
	TopSpammer            int
	RecentViolations      int
	NewIP                 int
	KnownGood             int // Negative value

	// Timing patterns
	BotLikeTiming         int
	OffHoursSending       int
	IdenticalTiming       int
}

// DefaultScoreWeights returns the default point values
func DefaultScoreWeights() ScoreWeights {
	return ScoreWeights{
		// Protocol violations (instant = 100 or high points)
		PreBannerTalk:         100, // Instant DROP
		InvalidCommand:        40,
		MalformedHelo:         25,
		MissingCommands:       20,

		// Connection patterns
		QuickDisconnect:       30,
		FrequencySpike:        25, // >100 connections/hour
		NoAuthSubmission:      20,
		FailedAuthPer:         15, // Per attempt
		FailedAuthMax:         45, // Maximum from failed auth
		MultipleRecipNoAuth:   20,

		// Volume anomalies
		HighRecipientCount:    20, // >50 recipients
		HighMessagePerConn:    15, // >10 messages
		UnusualMessageSize:    10, // <100b or >10MB
		RapidFire:             15,

		// Historical reputation
		PreviouslyFlagged:     30,
		TopSpammer:            25,
		RecentViolations:      20,
		NewIP:                 10,
		KnownGood:             -50, // Negative score!

		// Timing patterns
		BotLikeTiming:         15,
		OffHoursSending:       10, // 2am-6am UTC
		IdenticalTiming:       10,
	}
}

// HourlyStats aggregates stats for an hour bucket
type HourlyStats struct {
	HourBucket      time.Time
	IP              net.IP
	ConnectionCount int64
	MessageCount    int64
	ViolationCount  int64
	AvgScore        float64
	MaxScore        int
}
