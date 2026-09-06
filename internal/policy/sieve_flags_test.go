package policy

import (
	"context"
	"reflect"
	"testing"
)

func TestSieveFlagCompatibility(t *testing.T) {
	engine, _ := newSieveEngine()
	email, err := NewEmailContext("sender@test", []string{"recipient@test"}, "127.0.0.1", "test", []byte("Subject: CaseSensitive\r\n\r\nBody"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, script, target string
		kind                 ActionType
		flags                []string
	}{
		{"explicit delivery overrides defaults", `addflag "\\Seen"; fileinto :flags ["\\Flagged", "customer"] "Projects";`, "Projects", ActionFileinto, []string{`\Flagged`, "customer"}},
		{"empty override", `addflag "\\Seen"; keep :flags "";`, "INBOX", ActionKeep, nil},
		{"default flags snapshot", `addflag "\\Seen"; keep; addflag "\\Flagged";`, "INBOX", ActionKeep, []string{`\Seen`}},
		{"explicit flags snapshot", `set "f" "\\Seen"; fileinto :flags "${f}" "Projects"; set "f" "\\Flagged";`, "Projects", ActionFileinto, []string{`\Seen`}},
		{"named flags separate", `setflag "f" "\\Seen"; addflag "f" ["\\Flagged", "\\seen"]; removeflag "F" "\\seen"; fileinto :flags "${F}" "Projects";`, "Projects", ActionFileinto, []string{`\Flagged`}},
		{"named flags do not change defaults", `addflag "\\Seen"; setflag "named" "\\Flagged"; keep;`, "INBOX", ActionKeep, []string{`\Seen`}},
		{"set then flag operation", `set "f" "Old Old"; addflag "f" "New"; removeflag "f" "old"; keep :flags "${f}";`, "INBOX", ActionKeep, []string{"New"}},
		{"ignore invalid flags", `setflag ["\\Recent", "\\Bogus", "bad*flag", "café", "", "\\Seen"]; keep;`, "INBOX", ActionKeep, []string{`\Seen`}},
		{"contains variable union", `set "one" "Other"; set "two" "NonJunk $Forwarded"; if hasflag :contains ["one", "TWO"] ["absent", "forward"] { fileinto "Matched"; }`, "Matched", ActionFileinto, nil},
		{"default ascii comparator", `addflag "Customer"; if hasflag :is "customer" { fileinto "Matched"; }`, "Matched", ActionFileinto, []string{"Customer"}},
		{"octet respects case", `addflag "Customer"; if hasflag :comparator "i;octet" :is "customer" { discard; } keep;`, "INBOX", ActionKeep, []string{"Customer"}},
		{"octet exact", `addflag "Customer"; if hasflag :is :comparator "i;octet" "Customer" { fileinto "Matched"; }`, "Matched", ActionFileinto, []string{"Customer"}},
		{"wildcard capture preserves case", `setflag "f" "Label-Blue"; if hasflag :matches "f" "label-*" { fileinto "${1}"; }`, "Blue", ActionFileinto, nil},
		{"literal wildcard escape", `setflag "f" "Label-Blue"; if hasflag :matches "f" "Label-\\*" { discard; } keep;`, "INBOX", ActionKeep, nil},
		{"system flag wildcard", `addflag "\\Seen"; if hasflag :matches "\\\\Se*" { fileinto "${1}"; }`, "en", ActionFileinto, []string{`\Seen`}},
		{"undefined named variable", `if hasflag :contains "missing" "Seen" { discard; } keep;`, "INBOX", ActionKeep, nil},
		{"short circuit preserves captures", `setflag "f" "Label-Blue"; if hasflag :matches "f" "Label-*" { if anyof (true, hasflag :matches "f" "*") { fileinto "${1}"; } }`, "Blue", ActionFileinto, nil},
		{"captures reset on next success", `setflag "f" "Label-Blue"; if hasflag :matches "f" "*-*" { if hasflag :matches "f" "*" { fileinto "${2}empty"; } }`, "empty", ActionFileinto, nil},
		{"single pass variable expansion", `set "literal" "wrong"; set "dollar" "$"; set "f" "${dollar}{literal}"; fileinto "${f}";`, "${literal}", ActionFileinto, nil},
		{"base quoting is not C escaping", `fileinto "n\n";`, "nn", ActionFileinto, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := `require ["fileinto", "imap4flags", "variables"]; ` + tc.script
			action, err := engine.Evaluate(context.Background(), email, script)
			if err != nil {
				t.Fatal(err)
			}
			if action.Type != tc.kind || action.Target != tc.target || !reflect.DeepEqual(action.Tags, tc.flags) {
				t.Fatalf("got %+v; want %s %q %v", action, tc.kind, tc.target, tc.flags)
			}
		})
	}
	// Without variables, ${...} remains literal; each evaluation gets fresh state.
	action, err := engine.Evaluate(context.Background(), email, `require ["fileinto", "imap4flags"]; fileinto :flags "\\Seen" "${literal}";`)
	if err != nil || action.Target != "${literal}" {
		t.Fatalf("non-variable expansion: %+v %v", action, err)
	}
	compiled, err := engine.Compile(`require ["variables", "imap4flags"]; if hasflag "f" "leaked" { discard; } setflag "f" "leaked"; keep;`)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		action, err = engine.ExecuteCompiled(context.Background(), email, compiled)
		if err != nil || action.Type != ActionKeep {
			t.Fatalf("evaluation leaked state: %+v %v", action, err)
		}
	}
}

func TestSieveFlagSyntaxAndRequirements(t *testing.T) {
	engine, _ := newSieveEngine()
	for _, script := range []string{
		`keep :flags "\\Seen";`, `addflag "\\Seen";`, `if hasflag "x" { keep; }`,
		`require "imap4flags"; setflag "named" "\\Seen";`,
		`require "imap4flags"; if hasflag "named" "\\Seen" { keep; }`,
		`require ["imap4flags","variables"]; setflag ["named"] "\\Seen";`,
		`require ["imap4flags","variables"]; setflag "1" "\\Seen";`,
		`require ["imap4flags","variables"]; setflag "${dynamic}" "\\Seen";`,
		`require ["imap4flags","variables"]; if hasflag "bad.name" "x" {keep;}`,
		`require "variables"; set "1" "x";`, `require "variables"; set "" "x";`,
		`keep; require "variables";`, `if false { require "variables"; }`,
		`require "imap4flags"; keep :flags "\\Seen" :flags "\\Flagged";`,
		`require "imap4flags"; keep :flags;`, `require "imap4flags"; keep :flags ["a",];`,
		`require "imap4flags"; fileinto :flags "a";`,
		`require "imap4flags"; addflag :flags "x";`,
		`require "imap4flags"; if hasflag :is :contains "x" {keep;}`,
		`require "imap4flags"; if hasflag :comparator "i;octet" :comparator "i;octet" "x" {keep;}`,
		`require "imap4flags"; if hasflag :comparator "i;unicode-casemap" "x" {keep;}`,
		`require "imap4flags"; if hasflag :comparator {keep;}`,
		`require "imap4flags"; if hasflag :count "gt" "0" {keep;}`,
		`require ["imap4flags","variables"]; addflag "one" "two" "three";`,
	} {
		if err := engine.Validate(script); err == nil {
			t.Errorf("accepted %q", script)
		}
	}
}
