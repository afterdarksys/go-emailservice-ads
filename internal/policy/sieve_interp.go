package policy

import (
	"bytes"
	"context"
	"io"

	"fmt"
	_ "github.com/emersion/go-message/charset"
	messagemail "github.com/emersion/go-message/mail"
	"mime"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"
)

type svInterp struct {
	execution context.Context
	err       error
	actions   int
	ctx       *EmailContext
	vars      map[string]string
	flags     []string
}

func svExec(execution context.Context, ctx *EmailContext, script *svScript) (*Action, error) {
	interp := &svInterp{execution: execution, ctx: ctx, vars: make(map[string]string)}
	result := &Action{Type: ActionKeep}
	interp.runBlock(script.cmds, result)
	if len(interp.flags) > 0 {
		result.Tags = interp.flags
	}
	return result, interp.err
}

// runBlock returns true if a terminal action (reject/discard/stop) was hit.
func (i *svInterp) runBlock(cmds []svCmd, result *Action) bool {
	for _, cmd := range cmds {
		if i.err != nil {
			return true
		}
		if err := i.execution.Err(); err != nil {
			i.err = err
			return true
		}
		if i.runCmd(cmd, result) {
			return true
		}
	}
	return false
}

func (i *svInterp) runCmd(cmd svCmd, result *Action) bool {
	switch c := cmd.(type) {
	case *svIfCmd:
		for _, br := range c.branches {
			if i.evalTest(br.test) {
				return i.runBlock(br.body, result)
			}
		}
		if c.elseCmds != nil {
			return i.runBlock(c.elseCmds, result)
		}
	case *svActionCmd:
		return i.runAction(c, result)
	}
	return false
}

func (i *svInterp) runAction(c *svActionCmd, result *Action) bool {
	switch c.name {
	case "keep", "fileinto", "discard", "reject", "ereject":
		i.actions++
		if i.actions > 1 {
			i.err = fmt.Errorf("multiple delivery actions are not supported")
			return true
		}
	}
	switch c.name {
	case "keep":
		result.Type = ActionKeep
		if result.Target == "" {
			result.Target = "INBOX"
		}
	case "fileinto":
		if len(c.args) > 0 {
			result.Type = ActionFileinto
			result.Target = i.expand(c.args[0])
		}
	case "reject", "ereject":
		reason := "Message rejected by recipient's mail filter"
		if len(c.args) > 0 {
			reason = i.expand(c.args[0])
		}
		result.Type = ActionReject
		result.Reason = reason
		return true
	case "discard":
		result.Type = ActionDiscard
		return true
	case "stop":
		return true
	case "vacation":
		// Delivery continues; vacation auto-reply is advisory
		subject := c.tags["subject"]
		if subject == "" && len(c.args) > 0 {
			subject = i.expand(c.args[0])
		}
		result.Vacation = &Vacation{Subject: subject}
	case "set":
		// variables extension: set "name" "value"
		if len(c.args) >= 2 {
			i.vars[strings.ToLower(c.args[0])] = i.expand(c.args[1])
		}
	case "setflag":
		i.flags = c.args
	case "addflag":
		i.flags = svUnion(i.flags, c.args)
	case "removeflag":
		i.flags = svSubtract(i.flags, c.args)
	case "require":
		// no-op at execution time
	}
	return false
}

func (i *svInterp) evalTest(t svTest) bool {
	switch v := t.(type) {
	case *svTrueTest:
		return true
	case *svFalseTest:
		return false
	case *svNotTest:
		return !i.evalTest(v.inner)
	case *svAllofTest:
		for _, sub := range v.tests {
			if !i.evalTest(sub) {
				return false
			}
		}
		return true
	case *svAnyofTest:
		for _, sub := range v.tests {
			if i.evalTest(sub) {
				return true
			}
		}
		return false
	case *svHeaderTest:
		return i.evalHeader(v)
	case *svAddressTest:
		return i.evalAddress(v)
	case *svEnvelopeTest:
		return i.evalEnvelope(v)
	case *svBodyTest:
		body, err := sieveBody(i.ctx.Body)
		if err != nil {
			i.err = err
			return false
		}
		for _, k := range v.keys {
			if i.match(v.match, body, k) {
				return true
			}
		}
		return false
	case *svSizeTest:
		if v.over {
			return i.ctx.Size > v.limit
		}
		return i.ctx.Size < v.limit
	case *svHasflagTest:
		for _, f := range v.flags {
			if !svContains(i.flags, f) {
				return false
			}
		}
		return true
	}
	return false
}

func (i *svInterp) evalHeader(v *svHeaderTest) bool {
	if v.match == "exists" {
		for _, h := range v.headers {
			if _, exists := i.ctx.Headers[textproto.CanonicalMIMEHeaderKey(h)]; !exists {
				return false
			}
		}
		return true
	}
	for _, h := range v.headers {
		vals := i.ctx.Headers[textproto.CanonicalMIMEHeaderKey(h)]
		for _, k := range v.keys {
			for _, val := range vals {
				decoded, err := new(mime.WordDecoder).DecodeHeader(val)
				if err == nil {
					val = decoded
				}
				if i.match(v.match, val, k) {
					return true
				}
			}
		}
	}
	return false
}

func (i *svInterp) evalAddress(v *svAddressTest) bool {
	for _, h := range v.headers {
		addresses, err := mail.ParseAddressList(i.ctx.Headers.Get(h))
		if err != nil {
			continue
		}
		for _, a := range addresses {
			for _, key := range v.keys {
				if i.match(v.match, a.Address, key) {
					return true
				}
			}
		}
	}
	return false
}
func (i *svInterp) evalEnvelope(v *svEnvelopeTest) bool {
	for _, part := range v.parts {
		var values []string
		switch strings.ToLower(part) {
		case "from":
			values = []string{i.ctx.From}
		case "to":
			values = i.ctx.To
		default:
			i.err = fmt.Errorf("unsupported envelope part")
			return false
		}
		for _, value := range values {
			for _, key := range v.keys {
				if i.match(v.match, value, key) {
					return true
				}
			}
		}
	}
	return false
}

func (i *svInterp) match(matchType, haystack, needle string) bool {
	hay := strings.ToLower(haystack)
	ndl := strings.ToLower(i.expand(needle))
	switch matchType {
	case "is":
		return hay == ndl
	case "contains":
		return strings.Contains(hay, ndl)
	case "matches":
		pattern := regexp.QuoteMeta(ndl)
		pattern = strings.ReplaceAll(pattern, `\*`, ".*")
		pattern = strings.ReplaceAll(pattern, `\?`, ".")
		ok, _ := regexp.MatchString("(?s)^"+pattern+"$", hay)
		return ok
	}
	return false
}

func (i *svInterp) expand(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	var b strings.Builder
	rest := s
	for {
		if b.Len()+len(rest) > 1<<20 {
			i.err = fmt.Errorf("Sieve expansion exceeds 1 MiB")
			return ""
		}
		start := strings.Index(rest, "${")
		if start < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:start])
		end := strings.Index(rest[start:], "}")
		if end < 0 {
			b.WriteString(rest[start:])
			break
		}
		name := strings.ToLower(rest[start+2 : start+end])
		b.WriteString(i.vars[name])
		rest = rest[start+end+1:]
	}
	if b.Len() > 1<<20 {
		i.err = fmt.Errorf("Sieve expansion exceeds 1 MiB")
		return ""
	}
	return b.String()
}

func svUnion(a, b []string) []string {
	set := make(map[string]bool)
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	return out
}

func svSubtract(a, b []string) []string {
	rm := make(map[string]bool)
	for _, s := range b {
		rm[s] = true
	}
	var out []string
	for _, s := range a {
		if !rm[s] {
			out = append(out, s)
		}
	}
	return out
}

func svContains(ss []string, s string) bool {
	for _, v := range ss {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// Body tests inspect decoded MIME text parts, excluding message headers.
func sieveBody(raw []byte) (string, error) {
	reader, err := messagemail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	defer reader.Close()
	var body strings.Builder
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if _, ok := part.Header.(*messagemail.InlineHeader); !ok {
			continue
		}
		data, err := io.ReadAll(part.Body)
		if err != nil {
			return "", err
		}
		body.Write(data)
		body.WriteByte('\n')
	}
	return strings.ToLower(body.String()), nil
}
