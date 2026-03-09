package divert

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"time"

	"go.uber.org/zap"
)

// Composer creates diverted email messages
type Composer struct {
	logger   *zap.Logger
	settings *DivertSettings
}

// NewComposer creates a new message composer
func NewComposer(logger *zap.Logger, settings *DivertSettings) *Composer {
	return &Composer{
		logger:   logger,
		settings: settings,
	}
}

// ComposeDivertMessage creates a new message with diversion notice
func (c *Composer) ComposeDivertMessage(from, originalTo, divertTo, reason string, originalData []byte) ([]byte, error) {
	var buf bytes.Buffer

	// Calculate message hash
	hash := sha256.Sum256(originalData)
	hashStr := fmt.Sprintf("%x", hash)

	// Create multipart message
	boundary := generateBoundary()

	// Write headers
	headers := textproto.MIMEHeader{}
	headers.Set("From", fmt.Sprintf("Mail System <postmaster@divert-system>"))
	headers.Set("To", divertTo)
	headers.Set("Subject", fmt.Sprintf("[DIVERTED] Mail to %s", originalTo))
	headers.Set("Date", time.Now().Format(time.RFC1123Z))
	headers.Set("MIME-Version", "1.0")
	headers.Set("Content-Type", fmt.Sprintf("multipart/mixed; boundary=%s", boundary))
	headers.Set("X-Divert-Original-Recipient", originalTo)
	headers.Set("X-Divert-Original-Sender", from)
	headers.Set("X-Divert-Reason", reason)
	headers.Set("X-Divert-Timestamp", time.Now().UTC().Format(time.RFC3339))
	headers.Set("X-Divert-Message-Hash", hashStr)

	// Write headers to buffer
	for k, vs := range headers {
		for _, v := range vs {
			fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
		}
	}
	buf.WriteString("\r\n")

	// Create multipart writer
	mw := multipart.NewWriter(&buf)
	mw.SetBoundary(boundary)

	// Part 1: Diversion notice (text/plain)
	noticePart, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": []string{"text/plain; charset=utf-8"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create notice part: %w", err)
	}

	notice := fmt.Sprintf(`MAIL DIVERSION NOTICE
====================

This message was diverted from its original recipient per system policy.

Original Recipient: %s
Original Sender: %s
Diverted To: %s
Diversion Reason: %s
Diverted At: %s (UTC)

The original message is attached below as an RFC822 message.

Message Hash (SHA-256): %s

This is an automated system message. Do not reply.
`,
		originalTo,
		from,
		divertTo,
		reason,
		time.Now().UTC().Format(time.RFC3339),
		hashStr,
	)

	if _, err := noticePart.Write([]byte(notice)); err != nil {
		return nil, fmt.Errorf("failed to write notice: %w", err)
	}

	// Part 2: Original message (message/rfc822)
	originalPart, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              []string{c.settings.AttachmentFormat},
		"Content-Disposition":       []string{"attachment; filename=\"original-message.eml\""},
		"Content-Transfer-Encoding": []string{"base64"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create original part: %w", err)
	}

	// Encode original message in base64
	encoded := base64.StdEncoding.EncodeToString(originalData)
	if _, err := originalPart.Write([]byte(encoded)); err != nil {
		return nil, fmt.Errorf("failed to write original message: %w", err)
	}

	// Close multipart
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart: %w", err)
	}

	return buf.Bytes(), nil
}

// generateBoundary creates a MIME boundary string
func generateBoundary() string {
	return fmt.Sprintf("----=_Part_%d_%d",
		time.Now().Unix(),
		time.Now().Nanosecond())
}

// EncodeHeaderValue encodes a header value for safe transmission
func EncodeHeaderValue(value string) string {
	return mime.QEncoding.Encode("utf-8", value)
}
