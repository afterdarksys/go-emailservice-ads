package logformat

import (
	"io"
	"sync"
)

// Writer converts complete JSON entries emitted by zap. Zap serializes each
// entry in one Write; a mutex keeps YAML documents and syslog entries intact.
type Writer struct {
	Output io.Writer
	Format string
	mu     sync.Mutex
}

func (w *Writer) Write(raw []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	records, _, err := Decode(raw, "json")
	if err != nil {
		return 0, err
	}
	if err = Encode(w.Output, w.Format, records); err != nil {
		return 0, err
	}
	return len(raw), nil
}
