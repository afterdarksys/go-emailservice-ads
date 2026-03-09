package screen

import (
	"bytes"
	"fmt"
	"net/textproto"
	"time"

	"go.uber.org/zap"
)

// Copier creates screened copies of messages
type Copier struct {
	logger   *zap.Logger
	settings *ScreenSettings
}

// NewCopier creates a new message copier
func NewCopier(logger *zap.Logger, settings *ScreenSettings) *Copier {
	return &Copier{
		logger:   logger,
		settings: settings,
	}
}

// CreateScreenedCopy creates a copy of the message for a watcher
func (c *Copier) CreateScreenedCopy(from, to, watcher string, originalData []byte) ([]byte, error) {
	var buf bytes.Buffer

	// Parse original headers (simple approach)
	headers, body := c.splitMessage(originalData)

	// Copy original headers
	for k, vs := range headers {
		for _, v := range vs {
			fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
		}
	}

	// Add screening headers if enabled (in settings, not per-rule for simplicity)
	// For transparent screening, we can make this configurable
	if c.settings.NotifyWatchers {
		buf.WriteString(fmt.Sprintf("X-Screened-For: %s\r\n", watcher))
		buf.WriteString(fmt.Sprintf("X-Screened-From: %s\r\n", from))
		buf.WriteString(fmt.Sprintf("X-Screened-To: %s\r\n", to))
		buf.WriteString(fmt.Sprintf("X-Screened-Timestamp: %s\r\n", time.Now().UTC().Format(time.RFC3339)))
	}

	// Separator between headers and body
	buf.WriteString("\r\n")

	// Copy body
	buf.Write(body)

	return buf.Bytes(), nil
}

// splitMessage splits a message into headers and body
func (c *Copier) splitMessage(data []byte) (textproto.MIMEHeader, []byte) {
	// Find the end of headers (double CRLF)
	headerEnd := bytes.Index(data, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		// Try just LF
		headerEnd = bytes.Index(data, []byte("\n\n"))
		if headerEnd == -1 {
			// No headers found, treat entire message as body
			return make(textproto.MIMEHeader), data
		}
		headerEnd += 2
	} else {
		headerEnd += 4
	}

	headerData := data[:headerEnd]
	bodyData := data[headerEnd:]

	// Parse headers
	headers := make(textproto.MIMEHeader)
	lines := bytes.Split(headerData, []byte("\r\n"))
	if len(lines) == 1 {
		lines = bytes.Split(headerData, []byte("\n"))
	}

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		// Simple header parsing (doesn't handle multiline headers properly)
		idx := bytes.IndexByte(line, ':')
		if idx > 0 {
			key := string(line[:idx])
			value := string(bytes.TrimSpace(line[idx+1:]))
			headers.Add(key, value)
		}
	}

	return headers, bodyData
}

// EncryptCopy encrypts a screened copy
func (c *Copier) EncryptCopy(data []byte, recipient string) ([]byte, error) {
	// TODO: Implement encryption
	// Should use recipient's public key for PGP/S/MIME encryption
	c.logger.Warn("Message encryption not yet implemented")
	return data, nil
}

// AddRetentionMetadata adds retention information to screened copy
func (c *Copier) AddRetentionMetadata(data []byte, retentionDays int) ([]byte, error) {
	if retentionDays == 0 {
		retentionDays = c.settings.DefaultRetentionDays
	}

	expiryDate := time.Now().AddDate(0, 0, retentionDays)

	var buf bytes.Buffer

	// Parse headers and body
	headers, body := c.splitMessage(data)

	// Copy existing headers
	for k, vs := range headers {
		for _, v := range vs {
			fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
		}
	}

	// Add retention headers
	buf.WriteString(fmt.Sprintf("X-Screen-Retention-Days: %d\r\n", retentionDays))
	buf.WriteString(fmt.Sprintf("X-Screen-Expiry-Date: %s\r\n", expiryDate.Format(time.RFC3339)))

	buf.WriteString("\r\n")
	buf.Write(body)

	return buf.Bytes(), nil
}
