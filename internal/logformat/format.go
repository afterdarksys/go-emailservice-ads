// Package logformat converts bounded log batches without guessing unknown text.
package logformat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Record map[string]any

var modern = regexp.MustCompile(`^<([0-9]{1,3})>1 ([^ ]+) ([^ ]+) ([^ ]+) ([^ ]+) ([^ ]+) (.*)$`)
var legacy = regexp.MustCompile(`^(?:<([0-9]{1,3})>)?([A-Z][a-z]{2} +[0-9]{1,2} [0-9]{2}:[0-9]{2}:[0-9]{2}) ([^ ]+) (.*)$`)

func Decode(raw []byte, format string) ([]Record, string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, "", fmt.Errorf("empty log input")
	}
	if format == "auto" || format == "" {
		switch {
		case raw[0] == '{' || raw[0] == '[':
			format = "json"
		case modern.Match(raw) || legacy.Match(raw) || modern.Match(bytes.SplitN(raw, []byte("\n"), 2)[0]) || legacy.Match(bytes.SplitN(raw, []byte("\n"), 2)[0]):
			format = "syslog"
		default:
			format = "yaml"
		}
	}
	out := []Record{}
	add := func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			out = append(out, Record(x))
		case []any:
			for _, item := range x {
				m, ok := item.(map[string]any)
				if !ok {
					return fmt.Errorf("log array must contain objects")
				}
				out = append(out, Record(m))
			}
		default:
			return fmt.Errorf("log records must be objects; unknown text format")
		}
		return nil
	}
	switch format {
	case "json":
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		for {
			var v any
			e := d.Decode(&v)
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, format, e
			}
			if e = add(v); e != nil {
				return nil, format, e
			}
		}
	case "yaml":
		d := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var v any
			e := d.Decode(&v)
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, format, e
			}
			if e = add(v); e != nil {
				return nil, format, e
			}
		}
	case "syslog":
		for i, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if line == "" {
				continue
			}
			if m := modern.FindStringSubmatch(line); m != nil {
				sd, msg, e := splitStructured(m[7])
				if e != nil {
					return nil, format, fmt.Errorf("syslog line %d: %w", i+1, e)
				}
				out = append(out, Record{"priority": m[1], "time": m[2], "host": m[3], "app": m[4], "pid": m[5], "message_id": m[6], "structured_data": sd, "message": msg})
			} else if m := legacy.FindStringSubmatch(line); m != nil {
				out = append(out, Record{"priority": m[1], "time": m[2], "host": m[3], "message": m[4], "timestamp_year_unknown": true})
			} else {
				return nil, format, fmt.Errorf("unrecognized syslog line %d", i+1)
			}
		}
	default:
		return nil, format, fmt.Errorf("unsupported input format %q", format)
	}
	return out, format, nil
}
func splitStructured(s string) (string, string, error) {
	if s == "-" {
		return "-", "", nil
	}
	if strings.HasPrefix(s, "- ") {
		return "-", s[2:], nil
	}
	if !strings.HasPrefix(s, "[") {
		return "", "", fmt.Errorf("missing structured data")
	}
	quoted, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && quoted {
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
		}
		if c == ']' && !quoted {
			if i+1 == len(s) {
				return s, "", nil
			}
			if s[i+1] == ' ' {
				return s[:i+1], s[i+2:], nil
			}
			if s[i+1] != '[' {
				break
			}
		}
	}
	return "", "", fmt.Errorf("unterminated structured data")
}
func Encode(w io.Writer, format string, records []Record) error {
	for _, r := range records {
		switch format {
		case "json":
			if err := json.NewEncoder(w).Encode(r); err != nil {
				return err
			}
		case "yaml":
			// Convert JSON numbers to YAML numeric nodes instead of quoting their
			// lexical representation as strings (including large sequence IDs).
			jsonRaw, e := json.Marshal(r)
			if e != nil {
				return e
			}
			var node yaml.Node
			if e = yaml.Unmarshal(jsonRaw, &node); e != nil {
				return e
			}
			raw, e := yaml.Marshal(&node)
			if e != nil {
				return e
			}
			if _, e = fmt.Fprintf(w, "---\n%s", raw); e != nil {
				return e
			}
		case "syslog":
			raw, e := json.Marshal(r)
			if e != nil {
				return e
			}
			stamp := "-"
			if t, ok := r["time"].(string); ok && !strings.ContainsAny(t, " \r\n") {
				stamp = t
			}
			if _, e = fmt.Fprintf(w, "<134>1 %s - mailhub - - - %s\n", stamp, raw); e != nil {
				return e
			}
		default:
			return fmt.Errorf("unsupported output format %q", format)
		}
	}
	return nil
}
