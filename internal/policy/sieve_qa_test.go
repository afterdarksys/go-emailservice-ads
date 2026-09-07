package policy

import (
	"context"
	"strings"
	"testing"
)

func TestSieveRejectsInvalidAndUnsupportedPrograms(t *testing.T) {
	engine, err := newSieveEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{
		`if imaginary { discard; }`, `unknown_action;`, `keep`, `fileinto;`,
		`require "enotify";`, `vacation "away";`, `fileinto :copy "Archive";`,
		`if header :unknown "Subject" "x" { discard; }`,
		`if header "Subject" { keep; }`, `fileinto "unterminated`, `/* unterminated`,
		`if size :over 999999999999999999999999 { keep; }`, `if size :over 9223372036854775807G { keep; }`, `keep; }`, `if true { keep;`, `require ["fileinto" "reject"];`,
		strings.Repeat("if true {", 65) + "keep;" + strings.Repeat("}", 65),
	} {
		if err := engine.Validate(script); err == nil {
			t.Errorf("accepted invalid program %q", script)
		}
	}
	if _, err := engine.ExecuteCompiled(context.Background(), nil, "wrong-type"); err == nil {
		t.Fatal("invalid compiled script acknowledged")
	}
}
func TestSieveRoutingFlagsEnvelopeAndFailureSemantics(t *testing.T) {
	engine, _ := newSieveEngine()
	email, err := NewEmailContext("sender@example.test", []string{"first@example.test", "second@example.test"}, "127.0.0.1", "test", []byte("Subject: project\r\nFrom: Sender <sender@example.test>\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	script := `require ["fileinto","envelope","imap4flags"]; if envelope :is "to" "second@example.test" { addflag "\\Seen"; fileinto "Projects/2026"; stop; }`
	action, err := engine.Evaluate(context.Background(), email, script)
	if err != nil || action.Type != ActionFileinto || action.Target != "Projects/2026" || len(action.Tags) != 1 {
		t.Fatalf("%+v %v", action, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = engine.Evaluate(ctx, email, `discard;`); err == nil {
		t.Fatal("ignored cancellation")
	}
	if action, err = engine.Evaluate(context.Background(), email, `fileinto "A"; fileinto "B";`); err != nil || len(action.Actions) != 2 {
		t.Fatal("lost a delivery action", action, err)
	}
	explosive := `require "variables"; set "x" "xxxxxxxx";` + strings.Repeat(`set "x" "${x}${x}";`, 30) + `keep;`
	if _, err = engine.Evaluate(context.Background(), email, explosive); err == nil {
		t.Fatal("unbounded expansion accepted")
	}
}

func TestSieveBodyDecodesMIMEAndExcludesHeaders(t *testing.T) {
	engine, _ := newSieveEngine()
	email, err := NewEmailContext("a@test", []string{"b@test"}, "127.0.0.1", "test", []byte("Subject: headeronly\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nc2VhcmNoYWJsZQ=="))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key  string
		want ActionType
	}{{"headeronly", ActionKeep}, {"searchable", ActionDiscard}} {
		action, err := engine.Evaluate(context.Background(), email, `require "body"; if body :contains "`+tc.key+`" { discard; }`)
		if err != nil || action.Type != tc.want {
			t.Fatalf("%s: %+v %v", tc.key, action, err)
		}
	}
}

func TestSieveFlagListsAndAnyMatch(t *testing.T) {
	engine, _ := newSieveEngine()
	email, err := NewEmailContext("sender@test", []string{"recipient@test"}, "127.0.0.1", "test", []byte("Subject: test\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, script string
		want         ActionType
		flags        []string
	}{
		{"any flag matches", `addflag "\\Seen"; if hasflag ["\\Flagged", "\\seen"] { discard; }`, ActionDiscard, []string{`\Seen`}},
		{"no flags match", `addflag "\\Seen"; if hasflag "\\Flagged" { discard; }`, ActionKeep, []string{`\Seen`}},
		{"empty list matches nothing", `addflag "\\Seen"; if hasflag " " { discard; }`, ActionKeep, []string{`\Seen`}},
		{"space separated and duplicate flags", `addflag ["  \\Seen   \\Flagged ", "\\seen", ""];`, ActionKeep, []string{`\Seen`, `\Flagged`}},
		{"set replaces and remove splits", `addflag "\\Answered"; setflag "\\Seen \\Flagged customer"; removeflag "\\seen CUSTOMER";`, ActionKeep, []string{`\Flagged`}},
		{"empty set clears", `addflag "\\Seen"; setflag "";`, ActionKeep, nil},
		{"expanded flags", `set "f" "\\Seen \\Flagged"; addflag "${f}"; if hasflag "\\Draft \\seen" { discard; }`, ActionDiscard, []string{`\Seen`, `\Flagged`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			action, err := engine.Evaluate(context.Background(), email, `require ["imap4flags", "variables"]; `+tc.script)
			if err != nil {
				t.Fatal(err)
			}
			if action.Type != tc.want || len(action.Tags) != len(tc.flags) {
				t.Fatalf("got %+v; want %s %v", action, tc.want, tc.flags)
			}
			for idx, flag := range tc.flags {
				if action.Tags[idx] != flag {
					t.Fatalf("flags %v; want %v", action.Tags, tc.flags)
				}
			}
		})
	}
}

func TestSieveConditionListSeparators(t *testing.T) {
	engine, _ := newSieveEngine()
	for _, script := range []string{
		`if anyof (true false) { discard; }`, `if anyof (true,) { discard; }`,
		`if allof () { keep; }`, `if allof (,true) { keep; }`,
		`if allof (true,,false) { keep; }`, `if anyof (true`,
		`if allof (true, anyof (false true)) { discard; }`,
	} {
		if err := engine.Validate(script); err == nil {
			t.Errorf("accepted %q", script)
		}
	}
	for _, script := range []string{
		`if anyof (true) { keep; }`, `if allof (true, false) { keep; }`,
		`if allof (true, anyof (false, true)) { keep; }`,
	} {
		if err := engine.Validate(script); err != nil {
			t.Errorf("rejected %q: %v", script, err)
		}
	}
}
