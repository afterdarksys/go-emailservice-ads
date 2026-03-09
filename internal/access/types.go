package access

import (
	"context"
	"time"
)

// AccessResult represents the result of an access check
type AccessResult string

const (
	// Actions
	ResultOK       AccessResult = "OK"        // Accept
	ResultReject   AccessResult = "REJECT"    // Reject with default message
	ResultDefer    AccessResult = "DEFER"     // Temporary failure
	ResultDunno    AccessResult = "DUNNO"     // No decision (continue checking)
	ResultDiscard  AccessResult = "DISCARD"   // Accept but discard silently
	ResultHold     AccessResult = "HOLD"      // Place on hold
	ResultWarn     AccessResult = "WARN"      // Log warning and continue
	ResultPrepend  AccessResult = "PREPEND"   // Prepend header
	ResultRedirect AccessResult = "REDIRECT"  // Redirect to different address
	ResultFilter   AccessResult = "FILTER"    // Apply content filter
	ResultBCC      AccessResult = "BCC"       // Send BCC copy
)

// AccessDecision contains the full access control decision
type AccessDecision struct {
	Result  AccessResult
	Message string        // Custom message for REJECT
	Code    int           // SMTP code (4xx/5xx)
	Reason  string        // Internal reason
	Data    interface{}   // Additional data (redirect address, BCC, etc.)
}

// RestrictionStage represents when the restriction is applied
type RestrictionStage string

const (
	StageClient       RestrictionStage = "client"        // After client connection
	StageHelo         RestrictionStage = "helo"          // After HELO/EHLO
	StageSender       RestrictionStage = "sender"        // After MAIL FROM
	StageRecipient    RestrictionStage = "recipient"     // After RCPT TO
	StageData         RestrictionStage = "data"          // After DATA command
	StageEndOfData    RestrictionStage = "end_of_data"   // After message body
	StageEtrn         RestrictionStage = "etrn"          // After ETRN
)

// CheckContext contains context for access checks
type CheckContext struct {
	Stage        RestrictionStage
	ClientAddr   string
	ClientName   string
	HeloName     string
	Sender       string
	Recipient    string
	RecipientNum int
	MessageSize  int64
	Data         []byte
	Timestamp    time.Time
	Metadata     map[string]interface{}
}

// Restriction is an interface for access restrictions
type Restriction interface {
	// Check evaluates the restriction
	Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error)

	// Name returns the restriction name
	Name() string

	// Stage returns when this restriction applies
	Stage() RestrictionStage
}

// RestrictionClass is a named group of restrictions
type RestrictionClass struct {
	Name         string
	Restrictions []Restriction
}

// Map represents a lookup table interface
type Map interface {
	// Lookup performs a lookup and returns the result
	Lookup(ctx context.Context, key string) (string, error)

	// Type returns the map type
	Type() string

	// Close closes the map
	Close() error
}

// MapFactory creates maps from configuration
type MapFactory interface {
	Create(mapType string, params map[string]string) (Map, error)
}
