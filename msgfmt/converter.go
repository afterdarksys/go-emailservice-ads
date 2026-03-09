package msgfmt

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Converter provides functionality to convert between AMF and other formats
type Converter struct {
	options ConverterOptions
}

// ConverterOptions configures converter behavior
type ConverterOptions struct {
	// PreserveOriginalHeaders includes all original headers
	PreserveOriginalHeaders bool
	// InlineAttachmentThreshold determines when to inline vs reference (bytes)
	InlineAttachmentThreshold int64
	// GenerateThreadID automatically generates thread IDs from references
	GenerateThreadID bool
	// ParseSecurity extracts SPF/DKIM/DMARC results from headers
	ParseSecurity bool
}

// NewConverter creates a new format converter
func NewConverter(opts *ConverterOptions) *Converter {
	if opts == nil {
		opts = &ConverterOptions{
			PreserveOriginalHeaders:   true,
			InlineAttachmentThreshold: 1024 * 1024, // 1MB
			GenerateThreadID:          true,
			ParseSecurity:             true,
		}
	}
	return &Converter{options: *opts}
}

// FromEML converts an RFC 5322 (.eml) message to AMF
func (c *Converter) FromEML(reader io.Reader) (*Message, error) {
	emlMsg, err := mail.ReadMessage(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse EML: %w", err)
	}

	msg := &Message{
		Version: Version,
		Type:    TypeMessage,
		ID:      uuid.New().String(),
	}

	// Parse envelope
	msg.Envelope = &Envelope{
		MessageID: emlMsg.Header.Get("Message-ID"),
		InReplyTo: emlMsg.Header.Get("In-Reply-To"),
		Subject:   emlMsg.Header.Get("Subject"),
	}

	// Parse date
	if dateStr := emlMsg.Header.Get("Date"); dateStr != "" {
		if date, err := mail.ParseDate(dateStr); err == nil {
			msg.Envelope.Date = date
		}
	}

	// Parse addresses
	if from, err := mail.ParseAddress(emlMsg.Header.Get("From")); err == nil {
		msg.Envelope.From = &Address{
			Address: from.Address,
			Name:    from.Name,
		}
	}

	if toAddrs, err := mail.ParseAddressList(emlMsg.Header.Get("To")); err == nil {
		for _, addr := range toAddrs {
			msg.Envelope.To = append(msg.Envelope.To, &Address{
				Address: addr.Address,
				Name:    addr.Name,
			})
		}
	}

	if ccAddrs, err := mail.ParseAddressList(emlMsg.Header.Get("Cc")); err == nil {
		for _, addr := range ccAddrs {
			msg.Envelope.CC = append(msg.Envelope.CC, &Address{
				Address: addr.Address,
				Name:    addr.Name,
			})
		}
	}

	// Parse references
	if refs := emlMsg.Header.Get("References"); refs != "" {
		msg.Envelope.References = strings.Fields(refs)
	}

	// Generate thread ID if requested
	if c.options.GenerateThreadID {
		msg.Envelope.ThreadID = c.generateThreadID(msg.Envelope)
	}

	// Parse headers
	msg.Headers = &Headers{
		Standard: make(map[string]string),
		Extended: make(map[string]string),
	}

	for key, values := range emlMsg.Header {
		if len(values) > 0 {
			value := values[0]
			if c.isStandardHeader(key) {
				msg.Headers.Standard[key] = value
			} else {
				msg.Headers.Extended[key] = value
			}
		}
	}

	// Parse security headers
	if c.options.ParseSecurity {
		msg.Security = c.parseSecurityHeaders(emlMsg.Header)
	}

	// Parse body and attachments
	contentType := emlMsg.Header.Get("Content-Type")
	if contentType == "" {
		// Plain text body
		bodyBytes, _ := io.ReadAll(emlMsg.Body)
		msg.Body = &Body{
			Text: string(bodyBytes),
			Size: int64(len(bodyBytes)),
		}
	} else {
		mediaType, params, err := mime.ParseMediaType(contentType)
		if err == nil {
			if strings.HasPrefix(mediaType, "multipart/") {
				boundary := params["boundary"]
				if err := c.parseMultipart(emlMsg.Body, boundary, msg); err != nil {
					return nil, fmt.Errorf("failed to parse multipart: %w", err)
				}
			} else if strings.HasPrefix(mediaType, "text/") {
				bodyBytes, _ := io.ReadAll(emlMsg.Body)
				msg.Body = &Body{
					Text:     string(bodyBytes),
					Encoding: params["charset"],
					Size:     int64(len(bodyBytes)),
				}
			}
		}
	}

	return msg, nil
}

// ToEML converts an AMF message to RFC 5322 (.eml) format
func (c *Converter) ToEML(msg *Message, writer io.Writer) error {
	buf := &bytes.Buffer{}

	// Write headers
	if msg.Headers != nil && msg.Headers.Standard != nil && len(msg.Headers.Standard) > 0 {
		for key, value := range msg.Headers.Standard {
			fmt.Fprintf(buf, "%s: %s\r\n", key, value)
		}
	}

	// Always generate headers from envelope if not in standard headers
	if msg.Envelope != nil {
		// Check if From header already written
		hasFrom := false
		if msg.Headers != nil && msg.Headers.Standard != nil {
			_, hasFrom = msg.Headers.Standard["From"]
		}

		if !hasFrom && msg.Envelope.From != nil {
			from := mail.Address{
				Name:    msg.Envelope.From.Name,
				Address: msg.Envelope.From.Address,
			}
			fmt.Fprintf(buf, "From: %s\r\n", from.String())
		}

		if len(msg.Envelope.To) > 0 {
			var toAddrs []string
			for _, addr := range msg.Envelope.To {
				to := mail.Address{
					Name:    addr.Name,
					Address: addr.Address,
				}
				toAddrs = append(toAddrs, to.String())
			}
			fmt.Fprintf(buf, "To: %s\r\n", strings.Join(toAddrs, ", "))
		}

		if msg.Envelope.Subject != "" {
			fmt.Fprintf(buf, "Subject: %s\r\n", msg.Envelope.Subject)
		}

		if !msg.Envelope.Date.IsZero() {
			fmt.Fprintf(buf, "Date: %s\r\n", msg.Envelope.Date.Format(time.RFC1123Z))
		}

		if msg.Envelope.MessageID != "" {
			fmt.Fprintf(buf, "Message-ID: %s\r\n", msg.Envelope.MessageID)
		}

		if msg.Envelope.InReplyTo != "" {
			fmt.Fprintf(buf, "In-Reply-To: %s\r\n", msg.Envelope.InReplyTo)
		}

		if len(msg.Envelope.References) > 0 {
			fmt.Fprintf(buf, "References: %s\r\n", strings.Join(msg.Envelope.References, " "))
		}
	}

	// Add MIME version if there are attachments
	if len(msg.Attachments) > 0 {
		fmt.Fprintf(buf, "MIME-Version: 1.0\r\n")

		// Create multipart message
		boundary := fmt.Sprintf("----=_Part_%s", uuid.New().String())
		fmt.Fprintf(buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary)
		fmt.Fprintf(buf, "\r\n")

		// Write body part
		if msg.Body != nil {
			fmt.Fprintf(buf, "--%s\r\n", boundary)
			if msg.Body.HTML != "" {
				fmt.Fprintf(buf, "Content-Type: text/html; charset=utf-8\r\n\r\n")
				fmt.Fprintf(buf, "%s\r\n", msg.Body.HTML)
			} else if msg.Body.Text != "" {
				fmt.Fprintf(buf, "Content-Type: text/plain; charset=utf-8\r\n\r\n")
				fmt.Fprintf(buf, "%s\r\n", msg.Body.Text)
			}
		}

		// Write attachments
		for _, att := range msg.Attachments {
			fmt.Fprintf(buf, "--%s\r\n", boundary)
			fmt.Fprintf(buf, "Content-Type: %s\r\n", att.ContentType)
			fmt.Fprintf(buf, "Content-Disposition: %s; filename=\"%s\"\r\n", att.Disposition, att.Filename)
			fmt.Fprintf(buf, "Content-Transfer-Encoding: base64\r\n")
			if att.ContentID != "" {
				fmt.Fprintf(buf, "Content-ID: <%s>\r\n", att.ContentID)
			}
			fmt.Fprintf(buf, "\r\n")
			fmt.Fprintf(buf, "%s\r\n", att.Data)
		}

		fmt.Fprintf(buf, "--%s--\r\n", boundary)
	} else {
		// Simple message
		fmt.Fprintf(buf, "Content-Type: text/plain; charset=utf-8\r\n")
		fmt.Fprintf(buf, "\r\n")
		if msg.Body != nil && msg.Body.Text != "" {
			fmt.Fprintf(buf, "%s\r\n", msg.Body.Text)
		}
	}

	_, err := writer.Write(buf.Bytes())
	return err
}

// FromMbox converts an mbox file to multiple AMF messages
func (c *Converter) FromMbox(reader io.Reader) ([]*Message, error) {
	var messages []*Message
	scanner := &mboxScanner{reader: reader}

	for scanner.Next() {
		emlData := scanner.Message()
		msg, err := c.FromEML(bytes.NewReader(emlData))
		if err != nil {
			continue // Skip malformed messages
		}
		messages = append(messages, msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("mbox scan error: %w", err)
	}

	return messages, nil
}

// ToMbox converts multiple AMF messages to mbox format
func (c *Converter) ToMbox(messages []*Message, writer io.Writer) error {
	for _, msg := range messages {
		// Write mbox separator
		from := "MAILER-DAEMON"
		if msg.Envelope != nil && msg.Envelope.From != nil {
			from = msg.Envelope.From.Address
		}
		timestamp := time.Now()
		if msg.Envelope != nil && !msg.Envelope.Date.IsZero() {
			timestamp = msg.Envelope.Date
		}

		fmt.Fprintf(writer, "From %s %s\r\n", from, timestamp.Format(time.ANSIC))

		// Write message in EML format
		if err := c.ToEML(msg, writer); err != nil {
			return fmt.Errorf("failed to convert message: %w", err)
		}

		fmt.Fprintf(writer, "\r\n")
	}

	return nil
}

// parseMultipart parses a multipart MIME message
func (c *Converter) parseMultipart(body io.Reader, boundary string, msg *Message) error {
	mr := multipart.NewReader(body, boundary)

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		contentType := part.Header.Get("Content-Type")
		disposition := part.Header.Get("Content-Disposition")

		partData, err := io.ReadAll(part)
		if err != nil {
			return err
		}

		// Check if it's a text body part
		if strings.HasPrefix(contentType, "text/plain") && disposition == "" {
			if msg.Body == nil {
				msg.Body = &Body{}
			}
			msg.Body.Text = string(partData)
			msg.Body.Size = int64(len(partData))
		} else if strings.HasPrefix(contentType, "text/html") && disposition == "" {
			if msg.Body == nil {
				msg.Body = &Body{}
			}
			msg.Body.HTML = string(partData)
		} else {
			// It's an attachment
			att := &Attachment{
				ID:          uuid.New().String(),
				ContentType: contentType,
				Size:        int64(len(partData)),
				Disposition: DispositionAttachment,
			}

			// Parse filename
			if disposition != "" {
				_, params, err := mime.ParseMediaType(disposition)
				if err == nil {
					att.Filename = params["filename"]
				}
			}

			// Parse content ID
			if contentID := part.Header.Get("Content-ID"); contentID != "" {
				att.ContentID = strings.Trim(contentID, "<>")
				att.Disposition = DispositionInline
			}

			// Calculate hash
			hash := sha256.Sum256(partData)
			att.Hash = fmt.Sprintf("sha256:%x", hash)

			// Determine storage strategy
			if int64(len(partData)) <= c.options.InlineAttachmentThreshold {
				att.Storage = StorageInline
				att.Data = base64.StdEncoding.EncodeToString(partData)
			} else {
				att.Storage = StorageReference
				att.Reference = &AttachmentReference{
					Type:  "content-addressable",
					URI:   att.Hash,
					Store: "local",
				}
			}

			msg.Attachments = append(msg.Attachments, att)
		}
	}

	return nil
}

// parseSecurityHeaders extracts security information from headers
func (c *Converter) parseSecurityHeaders(header mail.Header) *Security {
	security := &Security{
		Authentication: &AuthenticationResults{},
	}

	// Parse Authentication-Results header
	if authResults := header.Get("Authentication-Results"); authResults != "" {
		if strings.Contains(authResults, "spf=pass") {
			security.Authentication.SPF = &SPFResult{Result: "pass"}
		}
		if strings.Contains(authResults, "dkim=pass") {
			security.Authentication.DKIM = &DKIMResult{Result: "pass"}
		}
		if strings.Contains(authResults, "dmarc=pass") {
			security.Authentication.DMARC = &DMARCResult{Result: "pass"}
		}
	}

	return security
}

// isStandardHeader checks if a header is a standard RFC 5322 header
func (c *Converter) isStandardHeader(key string) bool {
	standardHeaders := map[string]bool{
		"From":       true,
		"To":         true,
		"Cc":         true,
		"Bcc":        true,
		"Subject":    true,
		"Date":       true,
		"Message-ID": true,
		"In-Reply-To": true,
		"References": true,
		"Reply-To":   true,
		"MIME-Version": true,
		"Content-Type": true,
		"Content-Transfer-Encoding": true,
	}
	return standardHeaders[textproto.CanonicalMIMEHeaderKey(key)]
}

// generateThreadID generates a thread ID from message references
func (c *Converter) generateThreadID(env *Envelope) string {
	if env.InReplyTo != "" {
		// Use the root message ID as thread ID
		return env.InReplyTo
	}
	if len(env.References) > 0 {
		// Use the first reference as thread ID
		return env.References[0]
	}
	// New thread, use message ID
	return env.MessageID
}

// mboxScanner scans mbox files
type mboxScanner struct {
	reader  io.Reader
	buffer  []byte
	message []byte
	err     error
	eof     bool
}

// Next advances to the next message
func (s *mboxScanner) Next() bool {
	if s.eof {
		return false
	}

	s.message = nil
	var line []byte
	inMessage := false

	data, err := io.ReadAll(s.reader)
	if err != nil {
		s.err = err
		return false
	}

	lines := bytes.Split(data, []byte("\n"))
	for _, line = range lines {
		if bytes.HasPrefix(line, []byte("From ")) {
			if inMessage {
				// Start of new message, return current
				return true
			}
			inMessage = true
			continue
		}

		if inMessage {
			s.message = append(s.message, line...)
			s.message = append(s.message, '\n')
		}
	}

	s.eof = true
	return len(s.message) > 0
}

// Message returns the current message
func (s *mboxScanner) Message() []byte {
	return s.message
}

// Err returns any error encountered
func (s *mboxScanner) Err() error {
	return s.err
}
