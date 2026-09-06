package imap

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}
func resolveCriteria(c *goimap.SearchCriteria, maxSeq, maxUID uint32) *goimap.SearchCriteria {
	if c == nil {
		return &goimap.SearchCriteria{}
	}
	out := *c
	out.SeqNum = resolveSet(c.SeqNum, maxSeq)
	out.Uid = resolveSet(c.Uid, maxUID)
	out.Not = nil
	for _, sub := range c.Not {
		out.Not = append(out.Not, resolveCriteria(sub, maxSeq, maxUID))
	}
	out.Or = nil
	for _, pair := range c.Or {
		out.Or = append(out.Or, [2]*goimap.SearchCriteria{resolveCriteria(pair[0], maxSeq, maxUID), resolveCriteria(pair[1], maxSeq, maxUID)})
	}
	return &out
}
func decodedBody(e *message.Entity, depth int) (string, error) {
	if depth > 50 {
		return "", fmt.Errorf("MIME nesting limit exceeded")
	}
	if reader := e.MultipartReader(); reader != nil {
		var out strings.Builder
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			text, err := decodedBody(part, depth+1)
			if err != nil {
				return "", err
			}
			out.WriteString(text)
			out.WriteByte('\n')
		}
		return out.String(), nil
	}
	raw, err := io.ReadAll(e.Body)
	return string(raw), err
}
func matchMessage(seq uint32, summary MessageSummary, raw []byte, c *goimap.SearchCriteria) (bool, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		return false, err
	}
	body, err := decodedBody(entity, 0)
	if err != nil {
		return false, err
	}
	headers := map[string][]string{}
	var text strings.Builder
	for field := entity.Header.Fields(); field.Next(); {
		value, err := field.Text()
		if err != nil {
			value = field.Value()
		}
		key := strings.ToLower(field.Key())
		headers[key] = append(headers[key], strings.ToLower(value))
		text.WriteString(key + ": " + strings.ToLower(value) + "\n")
	}
	body = strings.ToLower(body)
	text.WriteString(body)
	header := mail.Header{Header: entity.Header}
	sent, _ := header.Date()
	return matchCriteria(seq, summary, headers, body, text.String(), sent, c), nil
}
func day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
func matchCriteria(seq uint32, m MessageSummary, headers map[string][]string, body, text string, sent time.Time, c *goimap.SearchCriteria) bool {
	if c.SeqNum != nil && !c.SeqNum.Contains(seq) {
		return false
	}
	if c.Uid != nil && !c.Uid.Contains(m.UID) {
		return false
	}
	if !c.Since.IsZero() && day(m.Date).Before(day(c.Since)) {
		return false
	}
	if !c.Before.IsZero() && !day(m.Date).Before(day(c.Before)) {
		return false
	}
	if !c.SentSince.IsZero() && (sent.IsZero() || day(sent).Before(day(c.SentSince))) {
		return false
	}
	if !c.SentBefore.IsZero() && (sent.IsZero() || !day(sent).Before(day(c.SentBefore))) {
		return false
	}
	if c.Larger > 0 && m.Size <= int64(c.Larger) {
		return false
	}
	if c.Smaller > 0 && m.Size >= int64(c.Smaller) {
		return false
	}
	for _, f := range c.WithFlags {
		if !hasFlag(m.Flags, f) {
			return false
		}
	}
	for _, f := range c.WithoutFlags {
		if hasFlag(m.Flags, f) {
			return false
		}
	}
	for key, values := range c.Header {
		for _, want := range values {
			found := false
			for _, got := range headers[strings.ToLower(key)] {
				if strings.Contains(got, strings.ToLower(want)) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	for _, want := range c.Body {
		if !strings.Contains(body, strings.ToLower(want)) {
			return false
		}
	}
	for _, want := range c.Text {
		if !strings.Contains(text, strings.ToLower(want)) {
			return false
		}
	}
	for _, sub := range c.Not {
		if matchCriteria(seq, m, headers, body, text, sent, sub) {
			return false
		}
	}
	for _, pair := range c.Or {
		if !matchCriteria(seq, m, headers, body, text, sent, pair[0]) && !matchCriteria(seq, m, headers, body, text, sent, pair[1]) {
			return false
		}
	}
	return true
}
