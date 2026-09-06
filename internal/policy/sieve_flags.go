package policy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func (i *svInterp) captureDeliveryFlags(command *svActionCmd, result *Action) {
	i.deliveryFlagsCaptured = true
	if command.explicitFlags {
		result.Tags = i.flagList(command.flags)
	} else {
		result.Tags = append([]string(nil), i.flags...)
	}
}

// Ignore flags that cannot be stored, including Recent and unknown system flags.
// Sieve must not defer an otherwise valid delivery because of these values.
func svValidFlags(flags []string) []string {
	var out []string
	for _, flag := range flags {
		if flag == "" {
			continue
		}
		valid := true
		for idx, c := range flag {
			if c <= 32 || c >= 127 || strings.ContainsRune("(){%*\" ]", c) || c == '\\' && idx != 0 {
				valid = false
				break
			}
		}
		if strings.HasPrefix(flag, "\\") {
			switch strings.ToLower(flag) {
			case `\seen`, `\answered`, `\flagged`, `\deleted`, `\draft`:
			default:
				valid = false
			}
		}
		if valid {
			out = append(out, flag)
		}
	}
	return svUnion(nil, out)
}

func svASCIIFold(value string) string {
	raw := []byte(value)
	for idx, c := range raw {
		if c >= 'A' && c <= 'Z' {
			raw[idx] = c + ('a' - 'A')
		}
	}
	return string(raw)
}

// Match keys have already been expanded exactly once. Preserve source case in
// wildcard captures, even when comparison is case-insensitive.
func (i *svInterp) compare(kind, comparator, source, key string) bool {
	hay, needle := source, key
	if comparator == "i;ascii-casemap" {
		hay = svASCIIFold(hay)
		needle = svASCIIFold(needle)
	}
	switch kind {
	case "is":
		return hay == needle
	case "contains":
		return strings.Contains(hay, needle)
	case "matches":
		var pattern strings.Builder
		pattern.WriteString("(?s)^")
		literal := func(c rune) {
			if comparator == "i;ascii-casemap" && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
				lower := svASCIIFold(string(c))[0]
				fmt.Fprintf(&pattern, "[%c%c]", lower, lower-('a'-'A'))
			} else {
				pattern.WriteString(regexp.QuoteMeta(string(c)))
			}
		}
		escaped := false
		for _, c := range key {
			if escaped {
				literal(c)
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '*':
				pattern.WriteString("(.*?)")
			case '?':
				pattern.WriteString("(.)")
			default:
				literal(c)
			}
		}
		if escaped {
			literal('\\')
		}
		pattern.WriteByte('$')
		re, err := regexp.Compile(pattern.String())
		if err != nil {
			i.err = fmt.Errorf("invalid Sieve wildcard pattern: %w", err)
			return false
		}
		matches := re.FindStringSubmatch(source)
		if matches == nil {
			return false
		}
		if i.variablesEnabled {
			for name := range i.vars {
				if _, err := strconv.Atoi(name); err == nil {
					delete(i.vars, name)
				}
			}
			for idx, value := range matches {
				i.vars[strconv.Itoa(idx)] = value
			}
		}
		return true
	}
	return false
}

var svVariableReference = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*|[0-9]+)\}`)

func (i *svInterp) expand(value string) string {
	if !i.variablesEnabled {
		return value
	}
	var out strings.Builder
	previous := 0
	for _, loc := range svVariableReference.FindAllStringSubmatchIndex(value, -1) {
		name := strings.ToLower(value[loc[2]:loc[3]])
		if n, err := strconv.ParseUint(name, 10, 32); err == nil {
			name = strconv.FormatUint(n, 10)
		}
		out.WriteString(value[previous:loc[0]])
		out.WriteString(i.vars[name])
		if out.Len() > 1<<20 {
			i.err = fmt.Errorf("Sieve expansion exceeds 1 MiB")
			return ""
		}
		previous = loc[1]
	}
	out.WriteString(value[previous:])
	if out.Len() > 1<<20 {
		i.err = fmt.Errorf("Sieve expansion exceeds 1 MiB")
		return ""
	}
	return out.String()
}
