package policy

import (
	"path"
	"strings"
)

type svInterp struct {
	ctx   *EmailContext
	vars  map[string]string
	flags []string
}

func svExec(ctx *EmailContext, script *svScript) (*Action, error) {
	interp := &svInterp{ctx: ctx, vars: make(map[string]string)}
	result := &Action{Type: ActionKeep}
	interp.runBlock(script.cmds, result)
	if len(interp.flags) > 0 {
		result.Tags = interp.flags
	}
	return result, nil
}

// runBlock returns true if a terminal action (reject/discard/stop) was hit.
func (i *svInterp) runBlock(cmds []svCmd, result *Action) bool {
	for _, cmd := range cmds {
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
		body := strings.ToLower(string(i.ctx.Body))
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
			if i.ctx.Headers.Get(h) == "" {
				return false
			}
		}
		return true
	}
	for _, h := range v.headers {
		vals := i.ctx.Headers[strings.Title(strings.ToLower(h))]
		for _, k := range v.keys {
			for _, val := range vals {
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
		val := i.ctx.Headers.Get(h)
		for _, k := range v.keys {
			if i.match(v.match, val, k) {
				return true
			}
		}
	}
	return false
}

func (i *svInterp) evalEnvelope(v *svEnvelopeTest) bool {
	for _, part := range v.parts {
		var val string
		switch strings.ToLower(part) {
		case "from":
			val = i.ctx.From
		case "to":
			if len(i.ctx.To) > 0 {
				val = i.ctx.To[0]
			}
		}
		for _, k := range v.keys {
			if i.match(v.match, val, k) {
				return true
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
		ok, _ := path.Match(ndl, hay)
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
