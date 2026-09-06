package filtering

import (
	"bytes"
	"fmt"
	"strings"
)

// ApplyHeaders preserves the message body and folded fields byte-for-byte.
func ApplyHeaders(data []byte, v Verdict) ([]byte, error) {
	sep := []byte("\r\n\r\n")
	eol := "\r\n"
	n := bytes.Index(data, sep)
	if n < 0 {
		sep = []byte("\n\n")
		eol = "\n"
		n = bytes.Index(data, sep)
	}
	if n < 0 {
		return nil, fmt.Errorf("message has no header boundary")
	}
	var fields []string
	for _, line := range strings.Split(string(data[:n]), eol) {
		if len(fields) > 0 && strings.HasPrefix(line, " ") || len(fields) > 0 && strings.HasPrefix(line, "\t") {
			fields[len(fields)-1] += eol + line
		} else {
			fields = append(fields, line)
		}
	}
	for name, order := range v.RemoveHeaders {
		var matches []int
		for i, f := range fields {
			key, _, ok := strings.Cut(f, ":")
			if ok && strings.EqualFold(key, name) {
				matches = append(matches, i)
			}
		}
		remove := map[int]bool{}
		if order == 0 {
			for _, i := range matches {
				remove[i] = true
			}
		} else {
			idx := order - 1
			if order < 0 {
				idx = len(matches) + order
			}
			if idx >= 0 && idx < len(matches) {
				remove[matches[idx]] = true
			}
		}
		kept := fields[:0]
		for i, f := range fields {
			if !remove[i] {
				kept = append(kept, f)
			}
		}
		fields = kept
	}
	for _, h := range v.Headers {
		at := h.Order
		if at > len(fields) {
			at = len(fields)
		}
		if at < 0 {
			return nil, fmt.Errorf("invalid header order")
		}
		fields = append(fields, "")
		copy(fields[at+1:], fields[at:])
		fields[at] = h.Name + ": " + h.Value
	}
	out := []byte(strings.Join(fields, eol))
	out = append(out, sep...)
	out = append(out, data[n+len(sep):]...)
	return out, nil
}
