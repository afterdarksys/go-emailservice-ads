package elasticsearch

import "time"

// EventType represents the type of mail event
type EventType string

const (
	EventEnqueued   EventType = "enqueued"
	EventProcessing EventType = "processing"
	EventDelivered  EventType = "delivered"
	EventFailed     EventType = "failed"
	EventBounce     EventType = "bounce"
	EventRetry      EventType = "retry"
	EventDLQ        EventType = "dlq"
)

// MailEvent represents a mail event to be logged to Elasticsearch
type MailEvent struct {
	// Core identifiers
	MessageID         string    `json:"message_id"`
	OriginalMessageID string    `json:"original_message_id,omitempty"` // For bounces/splits
	QueueID           string    `json:"queue_id,omitempty"`
	TraceID           string    `json:"trace_id"`           // Global correlation ID
	ParentTraceID     string    `json:"parent_trace_id,omitempty"` // For related messages
	InstanceID        string    `json:"instance_id"`        // Pod/instance identifier
	Region            string    `json:"region,omitempty"`
	DeploymentMode    string    `json:"deployment_mode,omitempty"` // perimeter, internal, hybrid, standalone

	// Event metadata
	EventType EventType `json:"event_type"`
	Timestamp time.Time `json:"timestamp"`
	Tier      string    `json:"tier"`

	// Envelope
	Envelope EnvelopeInfo `json:"envelope"`

	// Message metadata
	Metadata MessageMetadata `json:"metadata"`

	// Security checks
	Security SecurityInfo `json:"security,omitempty"`

	// Delivery information
	Delivery DeliveryInfo `json:"delivery,omitempty"`

	// Policy information
	Policy PolicyInfo `json:"policy,omitempty"`

	// Error information
	Error ErrorInfo `json:"error,omitempty"`

	// Message headers (optional, controlled by HeaderLogging config)
	Headers map[string][]string `json:"headers,omitempty"`
}

// EnvelopeInfo contains SMTP envelope information
type EnvelopeInfo struct {
	From      string   `json:"from"`
	To        []string `json:"to"`
	SizeBytes int      `json:"size_bytes"`
}

// MessageMetadata contains additional message metadata
type MessageMetadata struct {
	ContentHash       string `json:"content_hash"`
	ClientIP          string `json:"client_ip,omitempty"`
	AuthenticatedUser string `json:"authenticated_user,omitempty"`
	HeloHostname      string `json:"helo_hostname,omitempty"`
	ReceivedAt        time.Time `json:"received_at,omitempty"`
}

// SecurityInfo contains security check results
type SecurityInfo struct {
	SPFResult    string `json:"spf_result,omitempty"`    // pass, fail, softfail, neutral, none, temperror, permerror
	DKIMResult   string `json:"dkim_result,omitempty"`   // pass, fail, neutral, temperror, permerror
	DMARCResult  string `json:"dmarc_result,omitempty"`  // pass, fail, none
	DANEVerified bool   `json:"dane_verified,omitempty"`
	TLSVersion   string `json:"tls_version,omitempty"`   // 1.2, 1.3
	Greylisted   bool   `json:"greylisted,omitempty"`
}

// DeliveryInfo contains delivery attempt information
type DeliveryInfo struct {
	RemoteHost    string `json:"remote_host,omitempty"`
	RemoteIP      string `json:"remote_ip,omitempty"`
	SMTPCode      int    `json:"smtp_code,omitempty"`
	LatencyMs     int64  `json:"latency_ms,omitempty"`
	AttemptNumber int    `json:"attempt_number"`
	NextRetryAt   *time.Time `json:"next_retry_at,omitempty"`
	IsPermanent   bool   `json:"is_permanent,omitempty"`
}

// PolicyInfo contains policy engine information
type PolicyInfo struct {
	PoliciesApplied []string `json:"policies_applied,omitempty"`
	PolicyAction    string   `json:"policy_action,omitempty"` // accept, reject, defer, quarantine
	PolicyScore     float64  `json:"policy_score,omitempty"`
}

// ErrorInfo contains error details
type ErrorInfo struct {
	Message    string `json:"message,omitempty"`
	Code       string `json:"code,omitempty"`
	Category   string `json:"category,omitempty"` // network, timeout, auth, policy, etc.
	Retryable  bool   `json:"retryable,omitempty"`
}

// IndexStats tracks indexing statistics
type IndexStats struct {
	EventsIndexed   int64
	EventsFailed    int64
	EventsDropped   int64
	BytesIndexed    int64
	LastIndexedAt   time.Time
}
