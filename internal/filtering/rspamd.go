package filtering

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type Header struct {
	Name, Value string
	Order       int
}

// ScanFinal requests the complete modified message and consumes ARC signature
// additions. Unsupported envelope mutations fail explicitly, never disappear.
func ScanFinal(ctx context.Context, endpoint, from, ip, helo, user string, to []string, data []byte, arc bool) (Verdict, []byte, error) {
	var v Verdict
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(endpoint, "/")+"/checkv2", bytes.NewReader(data))
	if err != nil {
		return v, nil, err
	}
	req.Header.Set("Content-Type", "message/rfc822")
	req.Header.Set("Flags", "body_block")
	req.Header.Set("From", from)
	req.Header.Set("IP", ip)
	req.Header.Set("Helo", helo)
	if user != "" {
		req.Header.Set("User", user)
	}
	if arc {
		req.Header.Set("PerformDkimSign", "yes")
	}
	for _, r := range to {
		req.Header.Add("Rcpt", r)
	}
	resp, err := client.Do(req)
	if err != nil {
		return v, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return v, nil, fmt.Errorf("scanner HTTP %d", resp.StatusCode)
	}
	limit := len(data) + (2 << 20)
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return v, nil, err
	}
	if len(raw) > limit {
		return v, nil, fmt.Errorf("scanner response too large")
	}
	var replacement []byte
	payload := raw
	if header := resp.Header.Get("Message-Offset"); header != "" {
		n, e := strconv.Atoi(header)
		if e != nil || n < 1 || n >= len(raw) {
			return v, nil, fmt.Errorf("invalid scanner body offset")
		}
		payload = raw[:n]
		replacement = raw[n:]
	}
	if err = json.Unmarshal(payload, &v); err != nil {
		return v, nil, err
	}
	switch v.Action {
	case "no action", "add header", "rewrite subject", "reject", "soft reject", "greylist", "quarantine", "discard":
	default:
		return v, nil, fmt.Errorf("unknown scanner action")
	}
	var extra struct {
		Milter map[string]json.RawMessage `json:"milter"`
	}
	if err = json.Unmarshal(payload, &extra); err != nil {
		return v, nil, err
	}
	for k, value := range extra.Milter {
		if k == "add_headers" || k == "remove_headers" || k == "reject" {
			continue
		}
		if string(value) != "null" && string(value) != "{}" && string(value) != "[]" && string(value) != "false" {
			return v, nil, fmt.Errorf("unsupported scanner milter operation %s", k)
		}
	}
	if raw := extra.Milter["reject"]; len(raw) > 0 && string(raw) != "null" {
		var action string
		if err := json.Unmarshal(raw, &action); err != nil {
			return v, nil, err
		}
		switch action {
		case "":
		case "reject", "soft reject", "quarantine", "discard":
			v.Action = action
		default:
			return v, nil, fmt.Errorf("unsupported custom scanner rejection")
		}
	}
	if raw := extra.Milter["remove_headers"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &v.RemoveHeaders); err != nil {
			return v, nil, err
		}
	}
	// A standard body_block is the complete, already modified message.
	if replacement != nil {
		return v, replacement, nil
	}
	var additions map[string]json.RawMessage
	if raw := extra.Milter["add_headers"]; len(raw) > 0 {
		if err = json.Unmarshal(raw, &additions); err != nil {
			return v, nil, err
		}
	}
	var keys []string
	for name := range additions {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	// Prepending in this order produces Seal, Message-Signature, Auth-Results.
	for _, name := range []string{"ARC-Authentication-Results", "ARC-Message-Signature", "ARC-Seal"} {
		for i, key := range keys {
			if strings.EqualFold(key, name) {
				keys = append(append(keys[:i:i], keys[i+1:]...), key)
				break
			}
		}
	}
	for _, name := range keys {
		if name == "" || strings.ContainsAny(name, ": \t\r\n") {
			return v, nil, fmt.Errorf("invalid scanner header name")
		}
		var values []Header
		var parse func(json.RawMessage) error
		parse = func(raw json.RawMessage) error {
			var str string
			if json.Unmarshal(raw, &str) == nil {
				values = append(values, Header{Value: str})
				return nil
			}
			var list []json.RawMessage
			if json.Unmarshal(raw, &list) == nil {
				for _, item := range list {
					if err := parse(item); err != nil {
						return err
					}
				}
				return nil
			}
			var item struct {
				Value string `json:"value"`
				Order int    `json:"order"`
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			if item.Value == "" {
				return fmt.Errorf("empty scanner header")
			}
			if item.Order < 0 {
				return fmt.Errorf("invalid scanner header insertion position")
			}
			values = append(values, Header{Value: item.Value, Order: item.Order})
			return nil
		}
		if err = parse(additions[name]); err != nil {
			return v, nil, err
		}
		for _, value := range values {
			// Only RFC header folding may contain line breaks.
			normalized := strings.ReplaceAll(value.Value, "\r\n", "\n")
			for i, ch := range normalized {
				if ch == '\r' || ch == 0 || ch == '\n' && (i+1 == len(normalized) || (normalized[i+1] != ' ' && normalized[i+1] != '\t')) {
					return v, nil, fmt.Errorf("invalid scanner header folding")
				}
			}
			v.Headers = append(v.Headers, Header{Name: name, Value: strings.ReplaceAll(normalized, "\n", "\r\n"), Order: value.Order})
		}
	}
	return v, replacement, nil
}
