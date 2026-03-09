package msgfmt

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Writer provides functionality to write AMF messages
type Writer struct {
	options WriterOptions
}

// WriterOptions configures writer behavior
type WriterOptions struct {
	// Indent enables pretty-printing with indentation
	Indent bool
	// IndentPrefix is the prefix for each indentation level
	IndentPrefix string
	// IndentValue is the indentation string (e.g., "  " or "\t")
	IndentValue string
	// Compression specifies the compression algorithm
	Compression CompressionType
	// EncryptFunc is called to encrypt messages
	EncryptFunc func(msg *Message) (*EncryptedMessage, error)
	// ValidateBeforeWrite validates the message before writing
	ValidateBeforeWrite bool
}

// NewWriter creates a new AMF writer
func NewWriter(opts *WriterOptions) *Writer {
	if opts == nil {
		opts = &WriterOptions{
			IndentPrefix: "",
			IndentValue:  "  ",
		}
	}
	return &Writer{options: *opts}
}

// WriteFile writes a message to a file
func (w *Writer) WriteFile(path string, msg *Message) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	return w.Write(file, msg)
}

// Write writes a message to a writer
func (w *Writer) Write(writer io.Writer, msg *Message) error {
	// Validate if requested
	if w.options.ValidateBeforeWrite {
		if err := w.validate(msg); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
	}

	// Encrypt if function provided
	if w.options.EncryptFunc != nil {
		encMsg, err := w.options.EncryptFunc(msg)
		if err != nil {
			return fmt.Errorf("encryption failed: %w", err)
		}
		return w.writeJSON(writer, encMsg)
	}

	// Apply compression
	switch w.options.Compression {
	case CompressionGzip:
		return w.WriteCompressed(writer, msg)
	case CompressionNone, "":
		return w.writeJSON(writer, msg)
	default:
		return fmt.Errorf("unsupported compression: %s", w.options.Compression)
	}
}

// WriteCompressed writes a gzip-compressed message
func (w *Writer) WriteCompressed(writer io.Writer, msg *Message) error {
	gzWriter := gzip.NewWriter(writer)
	defer gzWriter.Close()

	if err := w.writeJSON(gzWriter, msg); err != nil {
		return err
	}

	return gzWriter.Close()
}

// WriteExtended writes an ExtendedMessage
func (w *Writer) WriteExtended(writer io.Writer, msg *ExtendedMessage) error {
	if w.options.ValidateBeforeWrite {
		if err := w.validate(msg.Message); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
	}

	return w.writeJSON(writer, msg)
}

// WriteExtendedFile writes an ExtendedMessage to a file
func (w *Writer) WriteExtendedFile(path string, msg *ExtendedMessage) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	return w.WriteExtended(file, msg)
}

// writeJSON writes JSON to a writer
func (w *Writer) writeJSON(writer io.Writer, v interface{}) error {
	encoder := json.NewEncoder(writer)

	if w.options.Indent {
		encoder.SetIndent(w.options.IndentPrefix, w.options.IndentValue)
	}

	if err := encoder.Encode(v); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	return nil
}

// StreamWriter writes messages in streaming format (JSONL)
type StreamWriter struct {
	writer  *bufio.Writer
	options WriterOptions
}

// NewStreamWriter creates a new streaming writer
func NewStreamWriter(writer io.Writer, opts *WriterOptions) *StreamWriter {
	if opts == nil {
		opts = &WriterOptions{}
	}
	return &StreamWriter{
		writer:  bufio.NewWriter(writer),
		options: *opts,
	}
}

// WriteStream writes a message in streaming format
func (sw *StreamWriter) WriteStream(msg *Message) error {
	// Write header chunk
	headerChunk := StreamChunk{
		Chunk: "header",
		Data: map[string]interface{}{
			"version":     msg.Version,
			"type":        msg.Type,
			"id":          msg.ID,
			"encoding":    msg.Encoding,
			"compression": msg.Compression,
			"encrypted":   msg.Encrypted,
		},
	}
	if err := sw.writeChunk(&headerChunk); err != nil {
		return err
	}

	// Write envelope
	if msg.Envelope != nil {
		chunk := StreamChunk{Chunk: "envelope", Data: msg.Envelope}
		if err := sw.writeChunk(&chunk); err != nil {
			return err
		}
	}

	// Write headers
	if msg.Headers != nil {
		chunk := StreamChunk{Chunk: "headers", Data: msg.Headers}
		if err := sw.writeChunk(&chunk); err != nil {
			return err
		}
	}

	// Write body
	if msg.Body != nil {
		chunk := StreamChunk{Chunk: "body", Data: msg.Body}
		if err := sw.writeChunk(&chunk); err != nil {
			return err
		}
	}

	// Write attachments
	for i, att := range msg.Attachments {
		chunk := StreamChunk{Chunk: "attachment", Index: i, Data: att}
		if err := sw.writeChunk(&chunk); err != nil {
			return err
		}
	}

	// Write metadata
	if msg.Metadata != nil {
		chunk := StreamChunk{Chunk: "metadata", Data: msg.Metadata}
		if err := sw.writeChunk(&chunk); err != nil {
			return err
		}
	}

	// Write security
	if msg.Security != nil {
		chunk := StreamChunk{Chunk: "security", Data: msg.Security}
		if err := sw.writeChunk(&chunk); err != nil {
			return err
		}
	}

	// Write end marker
	endChunk := StreamChunk{Chunk: "end"}
	if err := sw.writeChunk(&endChunk); err != nil {
		return err
	}

	return sw.writer.Flush()
}

// writeChunk writes a single chunk
func (sw *StreamWriter) writeChunk(chunk *StreamChunk) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("failed to marshal chunk: %w", err)
	}

	if _, err := sw.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write chunk: %w", err)
	}

	if _, err := sw.writer.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// validate performs basic validation
func (w *Writer) validate(msg *Message) error {
	if msg.Version == "" {
		msg.Version = Version
	}

	if msg.Type == "" {
		msg.Type = TypeMessage
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
