package msgfmt

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Reader provides functionality to read AMF messages from various sources
type Reader struct {
	options ReaderOptions
}

// ReaderOptions configures the reader behavior
type ReaderOptions struct {
	// ValidateSchema enables JSON schema validation
	ValidateSchema bool
	// DecryptFunc is called to decrypt encrypted messages
	DecryptFunc func(encrypted *EncryptedMessage) (*Message, error)
	// MaxAttachmentSize limits inline attachment size (0 = unlimited)
	MaxAttachmentSize int64
	// LoadExternalAttachments attempts to load external attachments
	LoadExternalAttachments bool
}

// NewReader creates a new AMF reader
func NewReader(opts *ReaderOptions) *Reader {
	if opts == nil {
		opts = &ReaderOptions{}
	}
	return &Reader{options: *opts}
}

// ReadFile reads an AMF message from a file
func (r *Reader) ReadFile(path string) (*Message, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Detect file format by extension
	if strings.HasSuffix(path, ".amfz") {
		return r.ReadCompressed(file)
	}

	return r.Read(file)
}

// Read reads an AMF message from a reader
func (r *Reader) Read(reader io.Reader) (*Message, error) {
	var msg Message
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&msg); err != nil {
		return nil, fmt.Errorf("failed to decode message: %w", err)
	}

	// Check if it's an encrypted message
	if msg.Type == TypeEncryptedMessage {
		var encMsg EncryptedMessage
		if err := json.Unmarshal([]byte(jsonMustMarshal(&msg)), &encMsg); err != nil {
			return nil, fmt.Errorf("failed to parse encrypted message: %w", err)
		}

		if r.options.DecryptFunc == nil {
			return nil, fmt.Errorf("encrypted message but no decrypt function provided")
		}

		return r.options.DecryptFunc(&encMsg)
	}

	// Validate if requested
	if r.options.ValidateSchema {
		if err := r.validate(&msg); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	return &msg, nil
}

// ReadCompressed reads a compressed AMF message
func (r *Reader) ReadCompressed(reader io.Reader) (*Message, error) {
	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	return r.Read(gzReader)
}

// ReadExtended reads an ExtendedMessage with all enhancements
func (r *Reader) ReadExtended(reader io.Reader) (*ExtendedMessage, error) {
	var msg ExtendedMessage
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&msg); err != nil {
		return nil, fmt.Errorf("failed to decode extended message: %w", err)
	}

	if r.options.ValidateSchema {
		if err := r.validate(msg.Message); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	return &msg, nil
}

// ReadExtendedFile reads an ExtendedMessage from a file
func (r *Reader) ReadExtendedFile(path string) (*ExtendedMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	if strings.HasSuffix(path, ".amfz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		return r.ReadExtended(gzReader)
	}

	return r.ReadExtended(file)
}

// StreamReader reads messages in streaming format (JSONL)
type StreamReader struct {
	scanner *bufio.Scanner
	options ReaderOptions
}

// NewStreamReader creates a new streaming reader
func NewStreamReader(reader io.Reader, opts *ReaderOptions) *StreamReader {
	if opts == nil {
		opts = &ReaderOptions{}
	}
	return &StreamReader{
		scanner: bufio.NewScanner(reader),
		options: *opts,
	}
}

// ReadStream reads a complete message from a stream
func (sr *StreamReader) ReadStream() (*Message, error) {
	msg := &Message{}
	var headerRead bool

	for sr.scanner.Scan() {
		line := sr.scanner.Bytes()

		var chunk StreamChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			return nil, fmt.Errorf("failed to parse chunk: %w", err)
		}

		switch chunk.Chunk {
		case "header":
			if !headerRead {
				data, _ := json.Marshal(chunk.Data)
				if err := json.Unmarshal(data, msg); err != nil {
					return nil, fmt.Errorf("failed to parse header: %w", err)
				}
				headerRead = true
			}
		case "envelope":
			data, _ := json.Marshal(chunk.Data)
			var env Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				return nil, err
			}
			msg.Envelope = &env
		case "headers":
			data, _ := json.Marshal(chunk.Data)
			var headers Headers
			if err := json.Unmarshal(data, &headers); err != nil {
				return nil, err
			}
			msg.Headers = &headers
		case "body":
			data, _ := json.Marshal(chunk.Data)
			var body Body
			if err := json.Unmarshal(data, &body); err != nil {
				return nil, err
			}
			msg.Body = &body
		case "attachment":
			data, _ := json.Marshal(chunk.Data)
			var att Attachment
			if err := json.Unmarshal(data, &att); err != nil {
				return nil, err
			}
			msg.Attachments = append(msg.Attachments, &att)
		case "metadata":
			data, _ := json.Marshal(chunk.Data)
			var meta Metadata
			if err := json.Unmarshal(data, &meta); err != nil {
				return nil, err
			}
			msg.Metadata = &meta
		case "security":
			data, _ := json.Marshal(chunk.Data)
			var sec Security
			if err := json.Unmarshal(data, &sec); err != nil {
				return nil, err
			}
			msg.Security = &sec
		case "end":
			return msg, nil
		}
	}

	if err := sr.scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	return msg, nil
}

// validate performs basic validation
func (r *Reader) validate(msg *Message) error {
	if msg.Version == "" {
		return fmt.Errorf("missing version")
	}

	if msg.ID == "" {
		return fmt.Errorf("missing message ID")
	}

	if msg.Envelope == nil {
		return fmt.Errorf("missing envelope")
	}

	if msg.Envelope.MessageID == "" {
		return fmt.Errorf("missing envelope message_id")
	}

	if msg.Envelope.From == nil {
		return fmt.Errorf("missing sender")
	}

	if len(msg.Envelope.To) == 0 {
		return fmt.Errorf("missing recipients")
	}

	return nil
}

// Helper function to marshal JSON
func jsonMustMarshal(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}
