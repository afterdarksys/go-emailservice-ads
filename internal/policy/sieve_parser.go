package policy

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ---- tokens ----

type svTokKind int

const (
	svTokIdent    svTokKind = iota
	svTokString             // "..."
	svTokNumber             // 123[KMG]
	svTokTag                // :name
	svTokLBrace             // {
	svTokRBrace             // }
	svTokLBracket           // [
	svTokRBracket           // ]
	svTokLParen             // (
	svTokRParen             // )
	svTokComma              // ,
	svTokSemi               // ;
	svTokEOF
)

type svTok struct {
	kind svTokKind
	val  string
	num  int64
}

type svLexer struct {
	src []rune
	pos int
}

func svTokenize(src string) ([]svTok, error) {
	lx := &svLexer{src: []rune(src)}
	var out []svTok
	for {
		tok, err := lx.next()
		if err != nil {
			return nil, err
		}
		out = append(out, tok)
		if tok.kind == svTokEOF {
			break
		}
	}
	return out, nil
}

func (lx *svLexer) peek() rune {
	if lx.pos >= len(lx.src) {
		return 0
	}
	return lx.src[lx.pos]
}

func (lx *svLexer) adv() rune {
	c := lx.src[lx.pos]
	lx.pos++
	return c
}

func (lx *svLexer) next() (svTok, error) {
	for lx.pos < len(lx.src) {
		c := lx.peek()
		if unicode.IsSpace(c) {
			lx.adv()
			continue
		}
		if c == '#' {
			for lx.pos < len(lx.src) && lx.src[lx.pos] != '\n' {
				lx.pos++
			}
			continue
		}
		if c == '/' && lx.pos+1 < len(lx.src) && lx.src[lx.pos+1] == '*' {
			lx.pos += 2
			closed := false
			for lx.pos+1 < len(lx.src) {
				if lx.src[lx.pos] == '*' && lx.src[lx.pos+1] == '/' {
					lx.pos += 2
					closed = true
					break
				}
				lx.pos++
			}
			if !closed {
				return svTok{}, fmt.Errorf("unterminated comment")
			}
			continue
		}
		break
	}
	if lx.pos >= len(lx.src) {
		return svTok{kind: svTokEOF}, nil
	}
	c := lx.adv()
	switch c {
	case '{':
		return svTok{kind: svTokLBrace, val: "{"}, nil
	case '}':
		return svTok{kind: svTokRBrace, val: "}"}, nil
	case '[':
		return svTok{kind: svTokLBracket, val: "["}, nil
	case ']':
		return svTok{kind: svTokRBracket, val: "]"}, nil
	case '(':
		return svTok{kind: svTokLParen, val: "("}, nil
	case ')':
		return svTok{kind: svTokRParen, val: ")"}, nil
	case ',':
		return svTok{kind: svTokComma, val: ","}, nil
	case ';':
		return svTok{kind: svTokSemi, val: ";"}, nil
	case '"':
		closed := false
		var sb strings.Builder
		for lx.pos < len(lx.src) {
			ch := lx.adv()
			if ch == '"' {
				closed = true
				break
			}
			if ch == '\\' && lx.pos < len(lx.src) {
				esc := lx.adv()
				switch esc {
				case 'n':
					sb.WriteRune('\n')
				case 't':
					sb.WriteRune('\t')
				default:
					sb.WriteRune(esc)
				}
				continue
			}
			sb.WriteRune(ch)
		}
		if !closed {
			return svTok{}, fmt.Errorf("unterminated string")
		}
		return svTok{kind: svTokString, val: sb.String()}, nil
	case ':':
		var sb strings.Builder
		for lx.pos < len(lx.src) && (unicode.IsLetter(lx.peek()) || lx.peek() == '_' || unicode.IsDigit(lx.peek())) {
			sb.WriteRune(lx.adv())
		}
		return svTok{kind: svTokTag, val: strings.ToLower(sb.String())}, nil
	}
	if unicode.IsDigit(c) {
		var sb strings.Builder
		sb.WriteRune(c)
		for lx.pos < len(lx.src) && unicode.IsDigit(lx.peek()) {
			sb.WriteRune(lx.adv())
		}
		n, err := strconv.ParseInt(sb.String(), 10, 64)
		if err != nil {
			return svTok{}, fmt.Errorf("invalid size: %w", err)
		}
		if lx.pos < len(lx.src) {
			switch unicode.ToUpper(lx.peek()) {
			case 'K':
				if n > (1<<63-1)/1024 {
					return svTok{}, fmt.Errorf("size overflow")
				}
				n *= 1024
				lx.adv()
			case 'M':
				if n > (1<<63-1)/(1024*1024) {
					return svTok{}, fmt.Errorf("size overflow")
				}
				n *= 1024 * 1024
				lx.adv()
			case 'G':
				if n > (1<<63-1)/(1024*1024*1024) {
					return svTok{}, fmt.Errorf("size overflow")
				}
				n *= 1024 * 1024 * 1024
				lx.adv()
			}
		}
		return svTok{kind: svTokNumber, num: n}, nil
	}
	if unicode.IsLetter(c) || c == '_' {
		var sb strings.Builder
		sb.WriteRune(c)
		for lx.pos < len(lx.src) && (unicode.IsLetter(lx.peek()) || lx.peek() == '_' || unicode.IsDigit(lx.peek())) {
			sb.WriteRune(lx.adv())
		}
		return svTok{kind: svTokIdent, val: strings.ToLower(sb.String())}, nil
	}
	return svTok{}, fmt.Errorf("sieve: unexpected character %q", c)
}

// ---- AST ----

type svScript struct{ cmds []svCmd }

type svCmd interface{ svCmd() }

type svIfCmd struct {
	branches []svBranch
	elseCmds []svCmd
}

func (*svIfCmd) svCmd() {}

type svBranch struct {
	test svTest
	body []svCmd
}

type svActionCmd struct {
	name string
	tags map[string]string
	args []string
}

func (*svActionCmd) svCmd() {}

type svTest interface{ svTest() }

type svHeaderTest struct {
	match   string // "is","contains","matches","exists"
	headers []string
	keys    []string
}

func (*svHeaderTest) svTest() {}

type svAddressTest struct {
	match   string
	headers []string
	keys    []string
}

func (*svAddressTest) svTest() {}

type svEnvelopeTest struct {
	match string
	parts []string
	keys  []string
}

func (*svEnvelopeTest) svTest() {}

type svBodyTest struct {
	match string
	keys  []string
}

func (*svBodyTest) svTest() {}

type svSizeTest struct {
	over  bool
	limit int64
}

func (*svSizeTest) svTest() {}

type svAllofTest struct{ tests []svTest }

func (*svAllofTest) svTest() {}

type svAnyofTest struct{ tests []svTest }

func (*svAnyofTest) svTest() {}

type svNotTest struct{ inner svTest }

func (*svNotTest) svTest() {}

type svTrueTest struct{}

func (*svTrueTest) svTest() {}

type svFalseTest struct{}

func (*svFalseTest) svTest() {}

type svHasflagTest struct{ flags []string }

func (*svHasflagTest) svTest() {}

// ---- Parser ----

type svParser struct {
	toks []svTok
	pos  int
	err  error
}

func svParse(src string) (*svScript, error) {
	if len(src) > 1<<20 {
		return nil, fmt.Errorf("Sieve script exceeds 1 MiB")
	}
	toks, err := svTokenize(src)
	if err != nil {
		return nil, err
	}
	depth := 0
	for _, t := range toks {
		switch t.kind {
		case svTokLBrace, svTokLParen, svTokLBracket:
			depth++
		case svTokRBrace, svTokRParen, svTokRBracket:
			depth--
		}
		if depth < 0 || depth > 64 {
			return nil, fmt.Errorf("invalid Sieve nesting")
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced Sieve delimiters")
	}
	p := &svParser{toks: toks}
	cmds, err := p.block()
	if err != nil {
		return nil, err
	}
	if p.err != nil {
		return nil, p.err
	}
	if p.peek().kind != svTokEOF {
		return nil, fmt.Errorf("unexpected trailing Sieve input")
	}
	return &svScript{cmds: cmds}, nil
}

func (p *svParser) peek() svTok {
	if p.pos >= len(p.toks) {
		return svTok{kind: svTokEOF}
	}
	return p.toks[p.pos]
}

func (p *svParser) adv() svTok {
	t := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *svParser) expect(k svTokKind) (svTok, error) {
	t := p.adv()
	if t.kind != k {
		return t, fmt.Errorf("sieve: expected %d got %q", k, t.val)
	}
	return t, nil
}

func (p *svParser) block() ([]svCmd, error) {
	var cmds []svCmd
	for {
		t := p.peek()
		if t.kind == svTokEOF || t.kind == svTokRBrace {
			break
		}
		cmd, err := p.cmd()
		if err != nil {
			return nil, err
		}
		cmds = append(cmds, cmd)
	}
	return cmds, nil
}

func (p *svParser) braceBlock() ([]svCmd, error) {
	if _, err := p.expect(svTokLBrace); err != nil {
		return nil, err
	}
	cmds, err := p.block()
	if err != nil {
		return nil, err
	}
	_, err = p.expect(svTokRBrace)
	return cmds, err
}

func (p *svParser) cmd() (svCmd, error) {
	t := p.peek()
	if t.kind != svTokIdent {
		return nil, fmt.Errorf("sieve: expected command got %q", t.val)
	}
	if t.val == "if" {
		return p.ifCmd()
	}
	return p.actionCmd()
}

func (p *svParser) ifCmd() (*svIfCmd, error) {
	p.adv() // consume "if"
	c := &svIfCmd{}
	tst, err := p.test()
	if err != nil {
		return nil, err
	}
	body, err := p.braceBlock()
	if err != nil {
		return nil, err
	}
	c.branches = append(c.branches, svBranch{test: tst, body: body})
	for {
		next := p.peek()
		if next.kind == svTokIdent && next.val == "elsif" {
			p.adv()
			tst, err := p.test()
			if err != nil {
				return nil, err
			}
			body, err := p.braceBlock()
			if err != nil {
				return nil, err
			}
			c.branches = append(c.branches, svBranch{test: tst, body: body})
		} else if next.kind == svTokIdent && next.val == "else" {
			p.adv()
			body, err := p.braceBlock()
			if err != nil {
				return nil, err
			}
			c.elseCmds = body
			break
		} else {
			break
		}
	}
	return c, nil
}

func (p *svParser) actionCmd() (*svActionCmd, error) {
	name := p.adv().val
	cmd := &svActionCmd{name: name, tags: map[string]string{}}
	for p.peek().kind == svTokTag {
		tag := p.adv().val
		if p.peek().kind == svTokString || p.peek().kind == svTokNumber {
			cmd.tags[tag] = p.adv().val
		} else {
			cmd.tags[tag] = ""
		}
	}
	for p.peek().kind == svTokString || p.peek().kind == svTokLBracket {
		cmd.args = append(cmd.args, p.strList()...)
	}
	if _, err := p.expect(svTokSemi); err != nil {
		return nil, err
	}
	if len(cmd.tags) > 0 {
		return nil, fmt.Errorf("unsupported Sieve action tags")
	}
	min, max := 0, 0
	switch name {
	case "keep", "discard", "stop":
	case "fileinto", "reject", "ereject":
		min, max = 1, 1
	case "set":
		min, max = 2, 2
	case "setflag", "addflag", "removeflag", "require":
		min, max = 1, 100
	default:
		return nil, fmt.Errorf("unsupported Sieve action %q", name)
	}
	if len(cmd.args) < min || len(cmd.args) > max {
		return nil, fmt.Errorf("invalid arguments for %s", name)
	}
	if name == "require" {
		for _, cap := range cmd.args {
			if !svCapability(cap) {
				return nil, fmt.Errorf("unsupported Sieve capability %q", cap)
			}
		}
	}

	return cmd, nil
}

func (p *svParser) test() (svTest, error) {
	t := p.peek()
	if t.kind != svTokIdent {
		return nil, fmt.Errorf("sieve: expected test, got %q", t.val)
	}
	switch t.val {
	case "allof":
		p.adv()
		tests, err := p.testList()
		if err != nil {
			return nil, err
		}
		return &svAllofTest{tests: tests}, nil
	case "anyof":
		p.adv()
		tests, err := p.testList()
		if err != nil {
			return nil, err
		}
		return &svAnyofTest{tests: tests}, nil
	case "not":
		p.adv()
		inner, err := p.test()
		if err != nil {
			return nil, err
		}
		return &svNotTest{inner: inner}, nil
	case "true":
		p.adv()
		return &svTrueTest{}, nil
	case "false":
		p.adv()
		return &svFalseTest{}, nil
	case "header":
		return p.headerTest()
	case "address":
		return p.addressTest()
	case "envelope":
		return p.envelopeTest()
	case "body":
		return p.bodyTest()
	case "size":
		return p.sizeTest()
	case "hasflag":
		return p.hasflagTest()
	case "exists":
		p.adv()
		headers := p.strList()
		return &svHeaderTest{match: "exists", headers: headers}, nil
	default:
		return nil, fmt.Errorf("unsupported Sieve test %q", t.val)
	}
}

func (p *svParser) testList() ([]svTest, error) {
	if _, err := p.expect(svTokLParen); err != nil {
		return nil, err
	}
	var tests []svTest
	for p.peek().kind != svTokRParen && p.peek().kind != svTokEOF {
		tst, err := p.test()
		if err != nil {
			return nil, err
		}
		tests = append(tests, tst)
		if p.peek().kind == svTokComma {
			p.adv()
		}
	}
	if _, err := p.expect(svTokRParen); err != nil {
		return nil, err
	}
	if len(tests) == 0 {
		return nil, fmt.Errorf("empty test list")
	}
	return tests, nil
}

func (p *svParser) consumeTags() string {
	match := ""
	for p.peek().kind == svTokTag {
		tag := p.adv().val
		switch tag {
		case "is":
			match = "is"
		case "contains":
			match = "contains"
		case "matches":
			match = "matches"
		case "comparator":
			if p.peek().kind == svTokString || p.peek().kind == svTokTag {
				if p.adv().val != "i;ascii-casemap" {
					p.err = fmt.Errorf("unsupported comparator")
				}
			} else {
				p.err = fmt.Errorf("missing comparator")
			}
		default:
			p.err = fmt.Errorf("unsupported Sieve tag %q", tag)
		}
	}
	return match
}

func (p *svParser) headerTest() (svTest, error) {
	p.adv()
	match := p.consumeTags()
	if match == "" {
		match = "is"
	}
	headers := p.strList()
	keys := p.strList()
	return &svHeaderTest{match: match, headers: headers, keys: keys}, nil
}

func (p *svParser) addressTest() (svTest, error) {
	p.adv()
	match := p.consumeTags()
	if match == "" {
		match = "is"
	}
	headers := p.strList()
	keys := p.strList()
	return &svAddressTest{match: match, headers: headers, keys: keys}, nil
}

func (p *svParser) envelopeTest() (svTest, error) {
	p.adv()
	match := p.consumeTags()
	if match == "" {
		match = "is"
	}
	parts := p.strList()
	keys := p.strList()
	return &svEnvelopeTest{match: match, parts: parts, keys: keys}, nil
}

func (p *svParser) bodyTest() (svTest, error) {
	p.adv()
	match := p.consumeTags()
	if match == "" {
		match = "contains"
	}
	keys := p.strList()
	return &svBodyTest{match: match, keys: keys}, nil
}

func (p *svParser) sizeTest() (svTest, error) {
	p.adv()
	over := true
	if p.peek().kind == svTokTag {
		tag := p.adv().val
		if tag != "over" && tag != "under" {
			return nil, fmt.Errorf("invalid size tag")
		}
		over = (tag == "over")
	}
	var limit int64
	if p.peek().kind == svTokNumber {
		limit = p.adv().num
	} else {
		return nil, fmt.Errorf("missing size limit")
	}
	return &svSizeTest{over: over, limit: limit}, nil
}

func (p *svParser) hasflagTest() (svTest, error) {
	p.adv()

	if p.peek().kind == svTokTag {
		return nil, fmt.Errorf("unsupported hasflag tag")
	}
	flags := p.strList()
	return &svHasflagTest{flags: flags}, nil
}

// strList parses a single string or a bracketed list of strings.
func (p *svParser) strList() []string {
	if p.peek().kind == svTokString {
		return []string{p.adv().val}
	}
	if p.peek().kind != svTokLBracket {
		p.err = fmt.Errorf("expected string or string list")
		return nil
	}
	p.adv()
	var out []string
	for {
		token, err := p.expect(svTokString)
		if err != nil {
			p.err = err
			return nil
		}
		out = append(out, token.val)
		if p.peek().kind == svTokRBracket {
			p.adv()
			return out
		}
		if _, err = p.expect(svTokComma); err != nil {
			p.err = err
			return nil
		}
	}
}

func svCapability(name string) bool {
	switch name {
	case "fileinto", "reject", "envelope", "body", "variables", "imap4flags":
		return true
	}
	return false
}
