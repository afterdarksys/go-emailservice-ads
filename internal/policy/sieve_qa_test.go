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
		`require "vacation";`, `require "copy";`, `fileinto :copy "Archive";`,
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
	if _, err = engine.Evaluate(context.Background(), email, `fileinto "A"; fileinto "B";`); err == nil {
		t.Fatal("silently dropped a delivery action")
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
