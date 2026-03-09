package policy

import (
	"net/mail"
	"strings"
	"time"
)

// EmailContext contains all information about an email message
// that is passed to policy engines for evaluation
type EmailContext struct {
	// === Envelope Information ===
	From     string   // MAIL FROM envelope sender
	To       []string // RCPT TO envelope recipients
	RemoteIP string   // Remote client IP address
	EHLO     string   // EHLO/HELO hostname

	// === Message Headers ===
	Headers mail.Header // Parsed message headers

	// === Message Body ===
	Body        []byte       // Full message body (headers + body)
	BodyText    string       // Text/plain body content
	BodyHTML    string       // Text/html body content
	Attachments []Attachment // Parsed attachments

	// === Security Verification Results ===
	SPFResult   SPFResult
	DKIMResult  DKIMResult
	DMARCResult DMARCResult
	ARCResult   ARCResult

	// === Reputation & Blocklists ===
	RBLResults   []RBLResult
	IPReputation ReputationScore

	// === Message Metadata ===
	MessageID  string    // Message-ID header
	Size       int64     // Total message size in bytes
	ReceivedAt time.Time // When message was received
	Subject    string    // Subject header (convenience)
	Date       time.Time // Date header (convenience)

	// === Authentication Info ===
	Authenticated bool   // Whether sender authenticated
	Username      string // Authenticated username (if any)

	// === Routing Context ===
	IsInbound  bool // True if from external sender
	IsOutbound bool // True if to external recipient
	IsInternal bool // True if both sender and recipient are local
	LocalDomains []string // List of local domains

	// === Group Membership (if available) ===
	SenderGroups    []string // Groups the sender belongs to
	RecipientGroups []string // Groups the recipients belong to
}

// NewEmailContext creates an EmailContext from message components
func NewEmailContext(from string, to []string, remoteIP, ehlo string, body []byte) (*EmailContext, error) {
	// Parse message headers
	msg, err := mail.ReadMessage(bytesReader(body))
	if err != nil {
		return nil, err
	}

	// Extract common headers
	messageID := msg.Header.Get("Message-ID")
	subject := msg.Header.Get("Subject")

	// Parse date
	dateStr := msg.Header.Get("Date")
	date, _ := mail.ParseDate(dateStr)

	ctx := &EmailContext{
		From:       from,
		To:         to,
		RemoteIP:   remoteIP,
		EHLO:       ehlo,
		Headers:    msg.Header,
		Body:       body,
		MessageID:  messageID,
		Size:       int64(len(body)),
		ReceivedAt: time.Now(),
		Subject:    subject,
		Date:       date,
	}

	return ctx, nil
}

// GetHeader returns the first value for a header (case-insensitive)
func (ctx *EmailContext) GetHeader(name string) string {
	return ctx.Headers.Get(name)
}

// GetAllHeaders returns all values for a header (case-insensitive)
func (ctx *EmailContext) GetAllHeaders(name string) []string {
	// Simple implementation - just return the values
	for k, v := range ctx.Headers {
		if canonicalKey(k) == canonicalKey(name) {
			return v
		}
	}
	return nil
}

// HasHeader checks if a header exists
func (ctx *EmailContext) HasHeader(name string) bool {
	for k := range ctx.Headers {
		if canonicalKey(k) == canonicalKey(name) {
			return true
		}
	}
	return false
}

// canonicalKey converts header name to canonical form
func canonicalKey(s string) string {
	return strings.ToLower(s)
}

// GetFromDomain extracts domain from envelope sender
func (ctx *EmailContext) GetFromDomain() string {
	addr, err := mail.ParseAddress(ctx.From)
	if err != nil {
		return ""
	}
	at := -1
	for i, c := range addr.Address {
		if c == '@' {
			at = i
			break
		}
	}
	if at == -1 {
		return ""
	}
	return addr.Address[at+1:]
}

// IsFromDomain checks if sender is from specified domain
func (ctx *EmailContext) IsFromDomain(domain string) bool {
	return ctx.GetFromDomain() == domain
}

// IsToLocal checks if all recipients are local
func (ctx *EmailContext) IsToLocal() bool {
	for _, recipient := range ctx.To {
		if !ctx.isLocalAddress(recipient) {
			return false
		}
	}
	return true
}

// IsFromLocal checks if sender is local
func (ctx *EmailContext) IsFromLocal() bool {
	return ctx.isLocalAddress(ctx.From)
}

// isLocalAddress checks if an email address is local
func (ctx *EmailContext) isLocalAddress(email string) bool {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}

	// Extract domain
	domain := ""
	at := -1
	for i, c := range addr.Address {
		if c == '@' {
			at = i
			break
		}
	}
	if at != -1 {
		domain = addr.Address[at+1:]
	}

	// Check against local domains
	for _, localDomain := range ctx.LocalDomains {
		if domain == localDomain {
			return true
		}
	}
	return false
}

// bytesReader wraps a byte slice to implement io.Reader
type bytesReaderImpl struct {
	data   []byte
	offset int
}

func bytesReader(data []byte) *bytesReaderImpl {
	return &bytesReaderImpl{data: data, offset: 0}
}

func (r *bytesReaderImpl) Read(p []byte) (n int, err error) {
	if r.offset >= len(r.data) {
		return 0, nil
	}
	n = copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}
