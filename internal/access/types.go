package access

import (
	"context"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/access/maps"
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

// Map is an alias to avoid import cycle - actual interface in maps package
type Map = maps.Map
