package policy

import (
	"time"
)

// ActionType defines the type of action a policy can return
type ActionType string

const (
	// RFC 5228 Sieve actions
	ActionKeep     ActionType = "keep"      // Keep message in inbox
	ActionDiscard  ActionType = "discard"   // Silently discard
	ActionReject   ActionType = "reject"    // Reject with message
	ActionFileinto ActionType = "fileinto"  // File into folder
	ActionRedirect ActionType = "redirect"  // Forward to address

	// Extended actions
	ActionAccept      ActionType = "accept"       // Explicitly accept
	ActionDefer       ActionType = "defer"        // Temporary failure
	ActionQuarantine  ActionType = "quarantine"   // Quarantine message
	ActionTag         ActionType = "tag"          // Add tags/flags
	ActionModify      ActionType = "modify"       // Modify message
	ActionNotify      ActionType = "notify"       // Send notification
	ActionVacation    ActionType = "vacation"     // Vacation responder

	// MailScript actions
	ActionDrop         ActionType = "drop"          // Forcefully drop message
	ActionBounce       ActionType = "bounce"        // Bounce back to sender
	ActionAutoReply    ActionType = "auto_reply"    // Send automated reply
	ActionAddToDigest  ActionType = "add_to_digest" // Add to digest queue
	ActionDivertTo     ActionType = "divert_to"     // Divert to different address
	ActionScreenTo     ActionType = "screen_to"     // Screen/copy to address
	ActionSMTPError    ActionType = "smtp_error"    // Reply with SMTP error code
	ActionSMTPDSN      ActionType = "smtp_dsn"      // Reply with SMTP DSN
	ActionForceSecondPass ActionType = "force_second_pass" // Route to another server
	ActionSkipCheck    ActionType = "skip_check"    // Skip security checks
	ActionSetDLP       ActionType = "set_dlp"       // Set DLP policy
)

// Action represents the result of policy evaluation
type Action struct {
	Type     ActionType // Action to take
	Reason   string     // Human-readable reason (for reject/defer)
	Target   string     // Target for redirect/fileinto/divert/screen
	Headers  []Header   // Headers to add/modify
	Tags     []string   // Tags/flags to add
	Priority int        // Action priority (higher = more important)

	// Extended fields
	RetryAfter int        // Seconds to wait before retry (for defer)
	Vacation   *Vacation  // Vacation responder details
	Notify     *Notify    // Notification details

	// MailScript-specific fields
	SMTPCode       int    // SMTP error code (for smtp_error)
	SMTPDSN        string // SMTP DSN string (for smtp_dsn)
	AutoReplyText  string // Auto-reply message text
	CheckToSkip    string // Type of check to skip (malware/spam/whitelist)
	DLPMode        string // DLP policy mode
	DLPTarget      string // DLP target (user/domain)
	ForceSecondServer string // Server for second pass
}

// Header represents an email header modification
type Header struct {
	Name   string
	Value  string
	Action string // "add", "remove", "replace"
}

// Vacation represents vacation responder settings
type Vacation struct {
	Subject  string
	Message  string
	Days     int
	FromDate time.Time
	ToDate   time.Time
}

// Notify represents notification settings
type Notify struct {
	Method  string // "mailto", "sms", "webhook"
	Target  string // Email address, phone number, or URL
	Message string
}

// Attachment represents an email attachment
type Attachment struct {
	Filename    string
	ContentType string
	Size        int64
	Extension   string
	Data        []byte // Optional: full data (memory intensive)
}

// SPFResult represents SPF verification result
type SPFResult string

const (
	SPFNone      SPFResult = "none"
	SPFNeutral   SPFResult = "neutral"
	SPFPass      SPFResult = "pass"
	SPFFail      SPFResult = "fail"
	SPFSoftfail  SPFResult = "softfail"
	SPFTempError SPFResult = "temperror"
	SPFPermError SPFResult = "permerror"
)

// DKIMResult represents DKIM verification result
type DKIMResult string

const (
	DKIMNone    DKIMResult = "none"
	DKIMPass    DKIMResult = "pass"
	DKIMFail    DKIMResult = "fail"
	DKIMPolicy  DKIMResult = "policy"
	DKIMNeutral DKIMResult = "neutral"
	DKIMTempError DKIMResult = "temperror"
	DKIMPermError DKIMResult = "permerror"
)

// DMARCResult represents DMARC policy result
type DMARCResult string

const (
	DMARCNone       DMARCResult = "none"
	DMARCPass       DMARCResult = "pass"
	DMARCFail       DMARCResult = "fail"
	DMARCQuarantine DMARCResult = "quarantine"
	DMARCReject     DMARCResult = "reject"
)

// ARCResult represents ARC verification result
type ARCResult string

const (
	ARCNone    ARCResult = "none"
	ARCPass    ARCResult = "pass"
	ARCFail    ARCResult = "fail"
)

// RBLResult represents a RBL lookup result
type RBLResult struct {
	Server string
	Listed bool
	Reason string
}

// ReputationScore represents IP/domain reputation (0-100)
type ReputationScore struct {
	Score  int
	Source string // "internal", "spamhaus", "barracuda", etc.
}

// PolicyScope defines where a policy applies
type PolicyScope struct {
	Type string   // "global", "user", "group", "domain", "direction"

	// Type-specific fields
	Users     []string // For type="user"
	Groups    []string // For type="group"
	Domains   []string // For type="domain"
	Direction string   // "inbound", "outbound", "internal" for type="direction"
}

// PolicyType defines the scripting engine to use
type PolicyType string

const (
	PolicyTypeSieve    PolicyType = "sieve"
	PolicyTypeStarlark PolicyType = "starlark"
)

// PolicyConfig represents a single policy configuration
type PolicyConfig struct {
	Name       string
	Type       PolicyType
	Enabled    bool
	Priority   int // Lower number = higher priority
	Scope      PolicyScope
	ScriptPath string
	Script     string // Inline script (alternative to ScriptPath)

	// Execution limits
	MaxExecutionTime time.Duration // Default: 10s
	MaxMemory        int64         // Default: 128MB
}

// PolicyResult represents the evaluation result from a policy
type PolicyResult struct {
	PolicyName string
	Matched    bool
	Action     *Action
	Error      error
	Duration   time.Duration
}
