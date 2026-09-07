package policy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"net/textproto"
	"strings"

	_ "github.com/emersion/go-message/charset"
	messagemail "github.com/emersion/go-message/mail"
)

type svInterp struct {
	plan                  []*Action
	implicitKeep          bool
	vacationCount         int
	execution             context.Context
	err                   error
	actions               int
	ctx                   *EmailContext
	vars                  map[string]string
	flags                 []string
	variablesEnabled      bool
	deliveryFlagsCaptured bool
}

func svExec(execution context.Context, ctx *EmailContext, script *svScript) (*Action, error) {
	interp := &svInterp{execution: execution, ctx: ctx, vars: make(map[string]string), variablesEnabled: script.variables, implicitKeep: true}
	result := &Action{Type: ActionKeep}
	interp.runBlock(script.cmds, result)
	if interp.err != nil {
		return nil, interp.err
	}
	if interp.implicitKeep {
		interp.plan = append(interp.plan, &Action{Type: ActionKeep, Target: "INBOX", Tags: svValidFlags(interp.flags)})
	}
	if len(interp.plan) == 0 {
		return &Action{Type: ActionDiscard, Tags: svValidFlags(interp.flags)}, nil
	}
	out := *interp.plan[0]
	out.Actions = interp.plan
	return &out, nil
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
	case "keep", "fileinto", "redirect", "vacation", "discard", "reject", "ereject":
		i.actions++
		if i.actions > 32 {
			i.err = fmt.Errorf("Sieve action limit exceeded")
			return true
		}
	}
	switch c.name {
	case "keep", "fileinto", "redirect":
		a := &Action{Type: ActionKeep, Target: "INBOX"}
		if c.name == "fileinto" {
			a.Type = ActionFileinto
			a.Target = i.expand(c.args[0])
		}
		if c.name == "redirect" {
			a.Type = ActionRedirect
			a.Target = i.expand(c.args[0])
			p, err := mail.ParseAddress(a.Target)
			if err != nil || p.Address != a.Target || strings.ContainsAny(a.Target, "\r\n\x00") {
				i.err = fmt.Errorf("invalid redirect address")
				return true
			}
		} else {
			i.captureDeliveryFlags(c, a)
		}
		i.plan = append(i.plan, a)
		if !c.copy {
			i.implicitKeep = false
		}
	case "vacation":
		i.vacationCount++
		if i.vacationCount > 1 {
			i.err = fmt.Errorf("multiple vacation actions")
			return true
		}
		v := *c.vacation
		v.Subject = i.expand(v.Subject)
		v.Message = i.expand(v.Message)
		v.From = i.expand(v.From)
		v.Handle = i.expand(v.Handle)
		v.Addresses = append([]string{}, v.Addresses...)
		for n, a := range v.Addresses {
			v.Addresses[n] = i.expand(a)
		}
		if strings.ContainsAny(v.Subject+v.From, "\r\n\x00") {
			i.err = fmt.Errorf("invalid vacation header")
			return true
		}
		i.plan = append(i.plan, &Action{Type: ActionVacation, Vacation: &v})
	case "reject", "ereject":
		if len(i.plan) > 0 {
			i.err = fmt.Errorf("reject conflicts with delivery actions")
			return true
		}
		i.implicitKeep = false
		i.plan = append(i.plan, &Action{Type: ActionReject, Reason: i.expand(c.args[0])})
		return true
	case "discard":
		i.implicitKeep = false
	case "stop":
		return true
	case "set":
		// variables extension: set "name" "value"
		if len(c.args) >= 2 {
			i.vars[strings.ToLower(c.args[0])] = i.expand(c.args[1])
		}
	case "setflag", "addflag", "removeflag":
		flags := i.flags
		key := strings.ToLower(c.variable)
		if c.variable != "" {
			flags = svValidFlags(strings.Fields(i.vars[key]))
		}
		operand := i.flagList(c.args)
		switch c.name {
		case "setflag":
			flags = operand
		case "addflag":
			flags = svUnion(flags, operand)
		case "removeflag":
			flags = svSubtract(flags, operand)
		}
		if c.variable != "" {
			value := strings.Join(flags, " ")
			if len(value) > 1<<20 {
				i.err = fmt.Errorf("Sieve flag variable exceeds 1 MiB")
				return true
			}
			i.vars[key] = value
		} else {
			i.flags = flags
		}
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
		flags := i.flags
		if v.variables != nil {
			flags = nil
			for _, name := range v.variables {
				flags = append(flags, svValidFlags(strings.Fields(i.vars[strings.ToLower(name)]))...)
			}
		}
		var keys []string
		for _, value := range v.flags {
			keys = append(keys, strings.Fields(i.expand(value))...)
		}
		for _, flag := range flags {
			for _, key := range keys {
				if i.compare(v.match, v.comparator, flag, key) {
					return true
				}
			}
		}
		return false
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
	return i.compare(matchType, "i;ascii-casemap", haystack, i.expand(needle))
}

// Flag arguments may each contain a space-separated list. Expand variables
// before splitting and preserve case-insensitive set semantics across actions.
func (i *svInterp) flagList(args []string) []string {
	var flags []string
	for _, arg := range args {
		flags = append(flags, strings.Fields(i.expand(arg))...)
	}
	return svValidFlags(flags)
}

func svUnion(a, b []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, flags := range [][]string{a, b} {
		for _, flag := range flags {
			key := strings.ToLower(flag)
			if !seen[key] {
				out = append(out, flag)
				seen[key] = true
			}
		}
	}
	return out
}

func svSubtract(a, b []string) []string {
	remove := make(map[string]bool, len(b))
	for _, flag := range b {
		remove[strings.ToLower(flag)] = true
	}
	var out []string
	for _, flag := range a {
		if !remove[strings.ToLower(flag)] {
			out = append(out, flag)
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
	return body.String(), nil
}
