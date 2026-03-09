package msgfmt

import (
	"time"
)

// Version is the format version
const Version = "1.0"

// MessageType represents the type of the message
type MessageType string

const (
	TypeMessage          MessageType = "message"
	TypeEncryptedMessage MessageType = "encrypted_message"
	TypeStreamChunk      MessageType = "stream_chunk"
)

// CompressionType represents the compression algorithm
type CompressionType string

const (
	CompressionNone CompressionType = "none"
	CompressionGzip CompressionType = "gzip"
	CompressionZstd CompressionType = "zstd"
	CompressionLZ4  CompressionType = "lz4"
)

// StorageStrategy represents how attachments are stored
type StorageStrategy string

const (
	StorageInline    StorageStrategy = "inline"
	StorageReference StorageStrategy = "reference"
	StorageExternal  StorageStrategy = "external"
)

// Priority represents message priority
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

// Sensitivity represents message sensitivity level
type Sensitivity string

const (
	SensitivityNormal       Sensitivity = "normal"
	SensitivityPersonal     Sensitivity = "personal"
	SensitivityPrivate      Sensitivity = "private"
	SensitivityConfidential Sensitivity = "confidential"
)

// Disposition represents attachment disposition
type Disposition string

const (
	DispositionAttachment Disposition = "attachment"
	DispositionInline     Disposition = "inline"
)

// Message represents the complete AMF message structure
type Message struct {
	Version     string          `json:"version"`
	Type        MessageType     `json:"type"`
	ID          string          `json:"id"`
	Encoding    string          `json:"encoding,omitempty"`
	Compression CompressionType `json:"compression,omitempty"`
	Encrypted   bool            `json:"encrypted,omitempty"`

	Envelope    *Envelope     `json:"envelope"`
	Headers     *Headers      `json:"headers,omitempty"`
	Body        *Body         `json:"body,omitempty"`
	Attachments []*Attachment `json:"attachments,omitempty"`
	Metadata    *Metadata     `json:"metadata,omitempty"`
	Security    *Security     `json:"security,omitempty"`
}

// Address represents an email address with optional display name
type Address struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
	Route   string `json:"route,omitempty"` // Original sender for forwarded messages
}

// Envelope contains core message routing and delivery information
type Envelope struct {
	MessageID    string     `json:"message_id"`
	InReplyTo    string     `json:"in_reply_to,omitempty"`
	References   []string   `json:"references,omitempty"`
	ThreadID     string     `json:"thread_id,omitempty"`
	From         *Address   `json:"from"`
	To           []*Address `json:"to"`
	CC           []*Address `json:"cc,omitempty"`
	BCC          []*Address `json:"bcc,omitempty"`
	ReplyTo      []*Address `json:"reply_to,omitempty"`
	Date         time.Time  `json:"date"`
	ReceivedDate time.Time  `json:"received_date,omitempty"`
	SentDate     time.Time  `json:"sent_date,omitempty"`
	Subject      string     `json:"subject"`
	Priority     Priority   `json:"priority,omitempty"`
	Sensitivity  Sensitivity `json:"sensitivity,omitempty"`
}

// Headers contains email headers
type Headers struct {
	Standard       map[string]string `json:"standard,omitempty"`
	Extended       map[string]string `json:"extended,omitempty"`
	Authentication map[string]string `json:"authentication,omitempty"`
}

// Body contains message content
type Body struct {
	Text     string `json:"text,omitempty"`
	HTML     string `json:"html,omitempty"`
	Markdown string `json:"markdown,omitempty"`

	Encoding string `json:"encoding,omitempty"`
	Charset  string `json:"charset,omitempty"`
	Format   string `json:"format,omitempty"`
	DelSp    string `json:"delsp,omitempty"`

	Size int64  `json:"size,omitempty"`
	Hash string `json:"hash,omitempty"`

	Compressed     bool  `json:"compressed,omitempty"`
	CompressedSize int64 `json:"compressed_size,omitempty"`
}

// Attachment represents a file attachment
type Attachment struct {
	ID          string          `json:"id"`
	Filename    string          `json:"filename"`
	ContentType string          `json:"content_type"`
	Size        int64           `json:"size"`
	Hash        string          `json:"hash,omitempty"`
	Encoding    string          `json:"encoding,omitempty"`
	Disposition Disposition     `json:"disposition,omitempty"`
	ContentID   string          `json:"content_id,omitempty"`
	Storage     StorageStrategy `json:"storage"`

	// Inline storage
	Data string `json:"data,omitempty"`

	// Reference storage
	Reference *AttachmentReference `json:"reference,omitempty"`

	// External storage
	External *AttachmentExternal `json:"external,omitempty"`

	// Compression info
	Compressed            bool            `json:"compressed,omitempty"`
	CompressedSize        int64           `json:"compressed_size,omitempty"`
	CompressionAlgorithm  CompressionType `json:"compression_algorithm,omitempty"`
}

// AttachmentReference represents content-addressable storage reference
type AttachmentReference struct {
	Type  string `json:"type"`  // "content-addressable"
	URI   string `json:"uri"`   // "sha256:abc123..."
	Store string `json:"store"` // "local", "s3", "azure"
}

// AttachmentExternal represents external storage location
type AttachmentExternal struct {
	URL         string    `json:"url"`
	Expires     time.Time `json:"expires,omitempty"`
	Credentials string    `json:"credentials,omitempty"`
}

// Metadata contains rich metadata for organization and search
type Metadata struct {
	Labels     []string          `json:"labels,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Categories []string          `json:"categories,omitempty"`
	Flags      []string          `json:"flags,omitempty"`
	Folder     string            `json:"folder,omitempty"`
	Mailbox    string            `json:"mailbox,omitempty"`
	Thread     *ThreadInfo       `json:"thread,omitempty"`
	Importance float64           `json:"importance,omitempty"`
	SpamScore  float64           `json:"spam_score,omitempty"`
	Classification string        `json:"classification,omitempty"` // "ham", "spam", "suspicious"
	Retention  *RetentionPolicy  `json:"retention,omitempty"`
	Custom     map[string]interface{} `json:"custom,omitempty"`
}

// ThreadInfo contains conversation threading information
type ThreadInfo struct {
	ID       string `json:"id"`
	Position int    `json:"position,omitempty"`
	Depth    int    `json:"depth,omitempty"`
	Root     string `json:"root,omitempty"`
}

// RetentionPolicy contains message retention information
type RetentionPolicy struct {
	Expires   time.Time `json:"expires,omitempty"`
	Policy    string    `json:"policy,omitempty"`
	LegalHold bool      `json:"legal_hold,omitempty"`
}

// Security contains security-related information
type Security struct {
	Encrypted      bool                   `json:"encrypted,omitempty"`
	Encryption     *EncryptionInfo        `json:"encryption,omitempty"`
	Signed         bool                   `json:"signed,omitempty"`
	Signature      *SignatureInfo         `json:"signature,omitempty"`
	Authentication *AuthenticationResults `json:"authentication,omitempty"`
	Quarantine     *QuarantineInfo        `json:"quarantine,omitempty"`
}

// EncryptionInfo contains encryption details
type EncryptionInfo struct {
	Algorithm     string   `json:"algorithm"` // "aes-256-gcm", "pgp", "smime"
	KeyID         string   `json:"key_id,omitempty"`
	RecipientKeys []string `json:"recipient_keys,omitempty"`
	Integrity     string   `json:"integrity,omitempty"`
	Nonce         string   `json:"nonce,omitempty"`
	Tag           string   `json:"tag,omitempty"`
}

// SignatureInfo contains digital signature information
type SignatureInfo struct {
	Algorithm     string    `json:"algorithm"` // "rsa-sha256", "ed25519"
	Signer        string    `json:"signer"`
	KeyID         string    `json:"key_id"`
	Timestamp     time.Time `json:"timestamp"`
	SignatureData string    `json:"signature_data"`
	Valid         bool      `json:"valid"`
}

// AuthenticationResults contains email authentication results
type AuthenticationResults struct {
	SPF   *SPFResult   `json:"spf,omitempty"`
	DKIM  *DKIMResult  `json:"dkim,omitempty"`
	DMARC *DMARCResult `json:"dmarc,omitempty"`
	ARC   *ARCResult   `json:"arc,omitempty"`
}

// SPFResult contains SPF verification results
type SPFResult struct {
	Result string `json:"result"` // "pass", "fail", "softfail", "neutral", "none"
	Domain string `json:"domain"`
	IP     string `json:"ip"`
}

// DKIMResult contains DKIM verification results
type DKIMResult struct {
	Result    string `json:"result"` // "pass", "fail", "neutral", "none"
	Domain    string `json:"domain"`
	Selector  string `json:"selector"`
	Signature string `json:"signature,omitempty"`
}

// DMARCResult contains DMARC verification results
type DMARCResult struct {
	Result    string `json:"result"` // "pass", "fail", "none"
	Policy    string `json:"policy"` // "reject", "quarantine", "none"
	Alignment string `json:"alignment,omitempty"` // "strict", "relaxed"
}

// ARCResult contains ARC verification results
type ARCResult struct {
	Result string                 `json:"result"` // "pass", "fail", "none"
	Chain  []map[string]interface{} `json:"chain,omitempty"`
}

// QuarantineInfo contains quarantine information
type QuarantineInfo struct {
	Quarantined bool    `json:"quarantined"`
	Reason      string  `json:"reason,omitempty"`
	Score       float64 `json:"score,omitempty"`
	Released    bool    `json:"released,omitempty"`
}

// EncryptedMessage represents a message-level encrypted message
type EncryptedMessage struct {
	Version    string           `json:"version"`
	Type       MessageType      `json:"type"`
	ID         string           `json:"id"`
	Encryption *EncryptionInfo  `json:"encryption"`
	EncryptedData string        `json:"encrypted_data"`
}

// StreamChunk represents a chunk in streaming format
type StreamChunk struct {
	Chunk string      `json:"chunk"` // "envelope", "headers", "body", "attachment", "metadata", "security", "end"
	Index int         `json:"index,omitempty"` // For attachments
	Data  interface{} `json:"data,omitempty"`
}

// =========================================
// Future Enhancements (v1.1 - v2.0)
// =========================================

// CalendarEvent represents embedded calendar event data (v1.1)
type CalendarEvent struct {
	Method      string    `json:"method"`       // "REQUEST", "REPLY", "CANCEL"
	UID         string    `json:"uid"`          // Unique event identifier
	Summary     string    `json:"summary"`      // Event title
	Description string    `json:"description,omitempty"`
	Location    string    `json:"location,omitempty"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	AllDay      bool      `json:"all_day,omitempty"`
	Organizer   *Address  `json:"organizer,omitempty"`
	Attendees   []*Attendee `json:"attendees,omitempty"`
	Recurrence  *RecurrenceRule `json:"recurrence,omitempty"`
	Status      string    `json:"status,omitempty"` // "CONFIRMED", "TENTATIVE", "CANCELLED"
	Sequence    int       `json:"sequence,omitempty"`
	ICalData    string    `json:"ical_data,omitempty"` // Raw iCalendar data
}

// Attendee represents a calendar event attendee
type Attendee struct {
	Address       string `json:"address"`
	Name          string `json:"name,omitempty"`
	Role          string `json:"role,omitempty"`          // "REQ-PARTICIPANT", "OPT-PARTICIPANT", "CHAIR"
	Status        string `json:"status,omitempty"`        // "ACCEPTED", "DECLINED", "TENTATIVE", "NEEDS-ACTION"
	RSVP          bool   `json:"rsvp,omitempty"`
	DelegatedFrom string `json:"delegated_from,omitempty"`
	DelegatedTo   string `json:"delegated_to,omitempty"`
}

// RecurrenceRule represents event recurrence pattern
type RecurrenceRule struct {
	Frequency string   `json:"frequency"` // "DAILY", "WEEKLY", "MONTHLY", "YEARLY"
	Interval  int      `json:"interval,omitempty"`
	Count     int      `json:"count,omitempty"`
	Until     time.Time `json:"until,omitempty"`
	ByDay     []string `json:"by_day,omitempty"`     // "MO", "TU", "WE", etc.
	ByMonth   []int    `json:"by_month,omitempty"`
	ByMonthDay []int   `json:"by_month_day,omitempty"`
}

// Collaboration represents real-time collaboration metadata (v1.2)
type Collaboration struct {
	Enabled       bool              `json:"enabled"`
	CollaborationID string          `json:"collaboration_id,omitempty"`
	SharedWith    []*Address        `json:"shared_with,omitempty"`
	Permissions   map[string]string `json:"permissions,omitempty"` // user -> "read", "write", "admin"
	Comments      []*Comment        `json:"comments,omitempty"`
	Versions      []*MessageVersion `json:"versions,omitempty"`
	Locks         []*Lock           `json:"locks,omitempty"`
	Activity      []*Activity       `json:"activity,omitempty"`
}

// Comment represents a comment on the message
type Comment struct {
	ID        string    `json:"id"`
	Author    *Address  `json:"author"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
	ReplyTo   string    `json:"reply_to,omitempty"`
	Resolved  bool      `json:"resolved,omitempty"`
}

// MessageVersion represents a version in the version history
type MessageVersion struct {
	ID        string    `json:"id"`
	Author    *Address  `json:"author"`
	Timestamp time.Time `json:"timestamp"`
	Changes   string    `json:"changes,omitempty"`
	Hash      string    `json:"hash"`
}

// Lock represents an edit lock
type Lock struct {
	User      *Address  `json:"user"`
	Section   string    `json:"section,omitempty"` // "body", "subject", etc.
	Timestamp time.Time `json:"timestamp"`
	Expires   time.Time `json:"expires"`
}

// Activity represents a collaboration activity
type Activity struct {
	ID        string                 `json:"id"`
	User      *Address               `json:"user"`
	Action    string                 `json:"action"` // "viewed", "edited", "commented", etc.
	Timestamp time.Time              `json:"timestamp"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// AIMetadata represents AI/ML analysis results (v1.3)
type AIMetadata struct {
	Analyzed       bool               `json:"analyzed"`
	AnalysisDate   time.Time          `json:"analysis_date,omitempty"`
	ModelVersion   string             `json:"model_version,omitempty"`
	Sentiment      *SentimentAnalysis `json:"sentiment,omitempty"`
	Classification *Classification    `json:"classification,omitempty"`
	Entities       []*Entity          `json:"entities,omitempty"`
	Topics         []string           `json:"topics,omitempty"`
	Summary        string             `json:"summary,omitempty"`
	KeyPhrases     []string           `json:"key_phrases,omitempty"`
	Language       *LanguageDetection `json:"language,omitempty"`
	Intent         *IntentAnalysis    `json:"intent,omitempty"`
	Priority       *PriorityPrediction `json:"priority,omitempty"`
	SuggestedActions []*SuggestedAction `json:"suggested_actions,omitempty"`
}

// SentimentAnalysis contains sentiment analysis results
type SentimentAnalysis struct {
	Overall   string  `json:"overall"`   // "positive", "negative", "neutral", "mixed"
	Score     float64 `json:"score"`     // -1.0 to 1.0
	Positive  float64 `json:"positive"`  // 0.0 to 1.0
	Negative  float64 `json:"negative"`  // 0.0 to 1.0
	Neutral   float64 `json:"neutral"`   // 0.0 to 1.0
	Confidence float64 `json:"confidence"` // 0.0 to 1.0
}

// Classification contains message classification
type Classification struct {
	Category    string  `json:"category"`     // "business", "personal", "finance", "travel", etc.
	Subcategory string  `json:"subcategory,omitempty"`
	Confidence  float64 `json:"confidence"`   // 0.0 to 1.0
	Tags        []string `json:"tags,omitempty"`
}

// Entity represents an extracted entity
type Entity struct {
	Type       string  `json:"type"`       // "person", "organization", "location", "date", "email", "phone", "url", etc.
	Text       string  `json:"text"`       // Original text
	Value      string  `json:"value,omitempty"` // Normalized value
	Confidence float64 `json:"confidence"` // 0.0 to 1.0
	Position   *Position `json:"position,omitempty"`
}

// Position represents text position
type Position struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// LanguageDetection contains language detection results
type LanguageDetection struct {
	Primary    string             `json:"primary"`    // ISO 639-1 code
	Confidence float64            `json:"confidence"` // 0.0 to 1.0
	Alternatives []LanguageAlternative `json:"alternatives,omitempty"`
}

// LanguageAlternative represents an alternative language detection
type LanguageAlternative struct {
	Language   string  `json:"language"`
	Confidence float64 `json:"confidence"`
}

// IntentAnalysis contains intent detection results
type IntentAnalysis struct {
	Primary    string  `json:"primary"`    // "request", "inform", "question", "complaint", etc.
	Confidence float64 `json:"confidence"` // 0.0 to 1.0
	Intents    []IntentScore `json:"intents,omitempty"`
}

// IntentScore represents an intent with score
type IntentScore struct {
	Intent string  `json:"intent"`
	Score  float64 `json:"score"`
}

// PriorityPrediction contains predicted message priority
type PriorityPrediction struct {
	Level      Priority `json:"level"`      // "low", "normal", "high", "urgent"
	Score      float64  `json:"score"`      // 0.0 to 1.0
	Confidence float64  `json:"confidence"` // 0.0 to 1.0
	Factors    []string `json:"factors,omitempty"` // Reasons for prediction
}

// SuggestedAction represents an AI-suggested action
type SuggestedAction struct {
	Type        string                 `json:"type"`        // "reply", "forward", "archive", "label", etc.
	Description string                 `json:"description"`
	Confidence  float64                `json:"confidence"` // 0.0 to 1.0
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// BlockchainVerification represents blockchain-based verification (v1.4)
type BlockchainVerification struct {
	Enabled       bool                `json:"enabled"`
	ChainID       string              `json:"chain_id,omitempty"`       // "ethereum", "polygon", "solana", etc.
	TransactionID string              `json:"transaction_id,omitempty"` // On-chain transaction ID
	BlockNumber   int64               `json:"block_number,omitempty"`
	Timestamp     time.Time           `json:"timestamp,omitempty"`
	Hash          string              `json:"hash"`                     // Message content hash
	ProofType     string              `json:"proof_type,omitempty"`     // "merkle", "signature", "timestamp"
	Proof         string              `json:"proof,omitempty"`          // Cryptographic proof
	Verified      bool                `json:"verified"`
	VerifiedAt    time.Time           `json:"verified_at,omitempty"`
	Certificate   *DigitalCertificate `json:"certificate,omitempty"`
	AuditTrail    []*AuditEntry       `json:"audit_trail,omitempty"`
	Immutable     bool                `json:"immutable"` // Whether message is immutably stored
}

// DigitalCertificate represents a blockchain-based certificate
type DigitalCertificate struct {
	ID           string    `json:"id"`
	Issuer       string    `json:"issuer"`
	Subject      string    `json:"subject"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	PublicKey    string    `json:"public_key"`
	Signature    string    `json:"signature"`
	CertificateData string `json:"certificate_data,omitempty"`
}

// AuditEntry represents an audit trail entry
type AuditEntry struct {
	ID        string                 `json:"id"`
	Action    string                 `json:"action"`    // "created", "modified", "accessed", "deleted"
	Actor     *Address               `json:"actor"`
	Timestamp time.Time              `json:"timestamp"`
	Hash      string                 `json:"hash"`      // State hash after action
	Signature string                 `json:"signature,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	PrevHash  string                 `json:"prev_hash,omitempty"` // Previous audit entry hash
}

// ExtendedMessage includes all enhancement fields
type ExtendedMessage struct {
	*Message

	// v1.1: Calendar support
	CalendarEvent *CalendarEvent `json:"calendar_event,omitempty"`

	// v1.2: Collaboration support
	Collaboration *Collaboration `json:"collaboration,omitempty"`

	// v1.3: AI/ML metadata
	AI *AIMetadata `json:"ai,omitempty"`

	// v1.4: Blockchain verification
	Blockchain *BlockchainVerification `json:"blockchain,omitempty"`
}
