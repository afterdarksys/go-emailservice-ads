package bounce

import (
	"bytes"
	"fmt"
	"mime/quotedprintable"
	"strings"
	"time"

	"github.com/google/uuid"
)

// BounceGenerator creates RFC 3464 compliant bounce messages (DSN - Delivery Status Notification)
// RFC 3464 - An Extensible Message Format for Delivery Status Notifications
// RFC 5321 Section 3.7 - Relaying and Mail Routing
type BounceGenerator struct {
	hostname string
	postmaster string
}

// NewBounceGenerator creates a new bounce message generator
func NewBounceGenerator(hostname, postmaster string) *BounceGenerator {
	return &BounceGenerator{
		hostname:   hostname,
		postmaster: postmaster,
	}
}

// BounceReason represents the reason for bounce
type BounceReason struct {
	SMTPCode     int
	EnhancedCode string // e.g., "5.1.1"
	Message      string
	IsPermanent  bool
	RemoteHost   string
	Recipient    string
}

// GenerateBounce creates an RFC 3464 compliant bounce message
func (bg *BounceGenerator) GenerateBounce(originalFrom string, reason *BounceReason, originalMessage []byte) ([]byte, error) {
	messageID := fmt.Sprintf("<%s@%s>", uuid.New().String(), bg.hostname)
	timestamp := time.Now().Format(time.RFC1123Z)

	// Determine DSN action and status
	action := "failed"
	if !reason.IsPermanent {
		action = "delayed"
	}

	// Build the multipart/report message
	var buf bytes.Buffer

	// Message headers
	buf.WriteString(fmt.Sprintf("From: Mail Delivery System <%s>\r\n", bg.postmaster))
	buf.WriteString(fmt.Sprintf("To: <%s>\r\n", originalFrom))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", bg.getBounceSubject(reason)))
	buf.WriteString(fmt.Sprintf("Date: %s\r\n", timestamp))
	buf.WriteString(fmt.Sprintf("Message-ID: %s\r\n", messageID))
	buf.WriteString("Auto-Submitted: auto-replied\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")

	// RFC 3464 requires multipart/report with report-type=delivery-status
	boundary := fmt.Sprintf("boundary_%s", uuid.New().String())
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/report; report-type=delivery-status; boundary=\"%s\"\r\n", boundary))
	buf.WriteString("\r\n")

	// Part 1: Human-readable explanation
	buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(bg.getHumanReadableMessage(reason))
	buf.WriteString("\r\n\r\n")

	// Part 2: Machine-readable delivery status (RFC 3464)
	buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	buf.WriteString("Content-Type: message/delivery-status\r\n")
	buf.WriteString("\r\n")

	// Per-Message DSN fields
	buf.WriteString(fmt.Sprintf("Reporting-MTA: dns; %s\r\n", bg.hostname))
	buf.WriteString(fmt.Sprintf("Arrival-Date: %s\r\n", timestamp))
	buf.WriteString("\r\n")

	// Per-Recipient DSN fields
	buf.WriteString(fmt.Sprintf("Final-Recipient: rfc822; %s\r\n", reason.Recipient))
	buf.WriteString(fmt.Sprintf("Action: %s\r\n", action))
	buf.WriteString(fmt.Sprintf("Status: %s\r\n", reason.EnhancedCode))
	if reason.RemoteHost != "" {
		buf.WriteString(fmt.Sprintf("Remote-MTA: dns; %s\r\n", reason.RemoteHost))
	}
	buf.WriteString(fmt.Sprintf("Diagnostic-Code: smtp; %d %s\r\n", reason.SMTPCode, reason.Message))
	buf.WriteString("\r\n")

	// Part 3: Original message headers (RFC 3464 Section 2.4)
	buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	buf.WriteString("Content-Type: text/rfc822-headers\r\n")
	buf.WriteString("\r\n")

	// Extract and include original headers (first 1KB)
	originalHeaders := bg.extractHeaders(originalMessage)
	buf.WriteString(originalHeaders)
	buf.WriteString("\r\n")

	// End of multipart message
	buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	return buf.Bytes(), nil
}

// getBounceSubject generates an appropriate subject line
func (bg *BounceGenerator) getBounceSubject(reason *BounceReason) string {
	if reason.IsPermanent {
		return fmt.Sprintf("Delivery failure: %s", reason.Message)
	}
	return fmt.Sprintf("Delivery delayed: %s", reason.Message)
}

// getHumanReadableMessage creates the human-readable part of the bounce
func (bg *BounceGenerator) getHumanReadableMessage(reason *BounceReason) string {
	var buf bytes.Buffer
	qpWriter := quotedprintable.NewWriter(&buf)

	if reason.IsPermanent {
		qpWriter.Write([]byte("This is a permanent delivery failure notification.\r\n\r\n"))
		qpWriter.Write([]byte("Your message could not be delivered to the following recipient(s):\r\n\r\n"))
	} else {
		qpWriter.Write([]byte("This is a temporary delivery delay notification.\r\n\r\n"))
		qpWriter.Write([]byte("Delivery to the following recipient(s) has been delayed:\r\n\r\n"))
	}

	qpWriter.Write([]byte(fmt.Sprintf("  Recipient: %s\r\n", reason.Recipient)))
	qpWriter.Write([]byte(fmt.Sprintf("  Reason: %s\r\n", reason.Message)))
	if reason.RemoteHost != "" {
		qpWriter.Write([]byte(fmt.Sprintf("  Remote server: %s\r\n", reason.RemoteHost)))
	}
	qpWriter.Write([]byte(fmt.Sprintf("  SMTP code: %d\r\n", reason.SMTPCode)))
	qpWriter.Write([]byte(fmt.Sprintf("  Status code: %s\r\n", reason.EnhancedCode)))

	qpWriter.Write([]byte("\r\n"))

	if reason.IsPermanent {
		qpWriter.Write([]byte("No further delivery attempts will be made.\r\n"))
	} else {
		qpWriter.Write([]byte("Delivery will be retried automatically. No action is required.\r\n"))
	}

	qpWriter.Write([]byte("\r\n"))
	qpWriter.Write([]byte("---\r\n"))
	qpWriter.Write([]byte(fmt.Sprintf("Generated by %s\r\n", bg.hostname)))

	qpWriter.Close()
	return buf.String()
}

// extractHeaders extracts headers from the original message
func (bg *BounceGenerator) extractHeaders(message []byte) string {
	// Find the blank line that separates headers from body
	headerEnd := bytes.Index(message, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		headerEnd = bytes.Index(message, []byte("\n\n"))
	}

	if headerEnd == -1 {
		// No body separator found, use first 1KB
		if len(message) > 1024 {
			return string(message[:1024])
		}
		return string(message)
	}

	headers := string(message[:headerEnd])

	// Limit to important headers only
	importantHeaders := []string{"From:", "To:", "Cc:", "Subject:", "Date:", "Message-ID:"}
	var filteredHeaders strings.Builder

	for _, line := range strings.Split(headers, "\n") {
		line = strings.TrimRight(line, "\r")
		for _, important := range importantHeaders {
			if strings.HasPrefix(line, important) {
				filteredHeaders.WriteString(line)
				filteredHeaders.WriteString("\r\n")
				break
			}
		}
	}

	return filteredHeaders.String()
}

// GenerateDelayWarning creates a delay warning message (not a full DSN)
// RFC 3461 - SMTP Service Extension for Delivery Status Notifications
func (bg *BounceGenerator) GenerateDelayWarning(originalFrom string, recipient string, delayDuration time.Duration, attempts int) ([]byte, error) {
	messageID := fmt.Sprintf("<%s@%s>", uuid.New().String(), bg.hostname)
	timestamp := time.Now().Format(time.RFC1123Z)

	var buf bytes.Buffer

	// Message headers
	buf.WriteString(fmt.Sprintf("From: Mail Delivery System <%s>\r\n", bg.postmaster))
	buf.WriteString(fmt.Sprintf("To: <%s>\r\n", originalFrom))
	buf.WriteString(fmt.Sprintf("Subject: Delivery delayed to %s\r\n", recipient))
	buf.WriteString(fmt.Sprintf("Date: %s\r\n", timestamp))
	buf.WriteString(fmt.Sprintf("Message-ID: %s\r\n", messageID))
	buf.WriteString("Auto-Submitted: auto-replied\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("\r\n")

	// Body
	buf.WriteString(fmt.Sprintf("This is a delay notification for a message you sent to %s.\r\n\r\n", recipient))
	buf.WriteString(fmt.Sprintf("Delivery has been delayed for %s.\r\n", delayDuration.String()))
	buf.WriteString(fmt.Sprintf("Number of delivery attempts: %d\r\n\r\n", attempts))
	buf.WriteString("Delivery will continue to be retried automatically.\r\n")
	buf.WriteString("You do not need to take any action.\r\n\r\n")
	buf.WriteString("If delivery continues to fail, you will receive a final delivery failure notification.\r\n\r\n")
	buf.WriteString("---\r\n")
	buf.WriteString(fmt.Sprintf("Generated by %s\r\n", bg.hostname))

	return buf.Bytes(), nil
}

// GetEnhancedStatusCode converts SMTP code to enhanced status code
// RFC 3463 - Enhanced Mail System Status Codes
func GetEnhancedStatusCode(smtpCode int, reason string) string {
	// Map SMTP codes to enhanced status codes
	switch smtpCode {
	case 550:
		if strings.Contains(strings.ToLower(reason), "mailbox") {
			return "5.1.1" // Mailbox does not exist
		}
		return "5.7.1" // Delivery not authorized
	case 551:
		return "5.1.6" // Mailbox has moved
	case 552:
		return "5.2.2" // Mailbox full
	case 553:
		return "5.1.3" // Bad destination mailbox address
	case 554:
		return "5.5.0" // Transaction failed
	case 450:
		return "4.2.0" // Mailbox temporarily unavailable
	case 451:
		return "4.3.0" // Mail system full
	case 452:
		return "4.2.2" // Mailbox full (temporary)
	case 421:
		return "4.3.2" // Service not available
	default:
		if smtpCode >= 500 {
			return "5.0.0" // Other permanent failure
		}
		return "4.0.0" // Other temporary failure
	}
}
