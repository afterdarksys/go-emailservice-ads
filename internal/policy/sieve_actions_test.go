package policy

import (
	"context"
	"strings"
	"testing"
)

func TestSieveCopyRedirectVacationAndMultipleActions(t *testing.T) {
	e, _ := newSieveEngine()
	ctx, err := NewEmailContext("sender@example.test", []string{"alice@example.test"}, "127.0.0.1", "mail.test", []byte("From: sender@example.test\r\nTo: alice@example.test\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		script string
		types  []ActionType
	}{
		{`require ["fileinto","copy"]; fileinto :copy "Archive";`, []ActionType{ActionFileinto, ActionKeep}},
		{`require "copy"; redirect :copy "other@example.test";`, []ActionType{ActionRedirect, ActionKeep}},
		{`redirect "other@example.test";`, []ActionType{ActionRedirect}},
		{`require "vacation"; vacation :days 2 :subject "Away" "back soon";`, []ActionType{ActionVacation, ActionKeep}},
		{`fileinto "A"; fileinto "B"; discard;`, []ActionType{ActionFileinto, ActionFileinto}},
	} {
		a, err := e.Evaluate(context.Background(), ctx, tc.script)
		if err != nil || len(a.Actions) != len(tc.types) {
			t.Fatal(tc.script, a, err)
		}
		for i, want := range tc.types {
			if a.Actions[i].Type != want {
				t.Fatal(a)
			}
		}
	}
	a, err := e.Evaluate(context.Background(), ctx, `require "imap4flags"; addflag "\\Seen"; fileinto "A"; addflag "\\Flagged"; fileinto "B";`)
	if err != nil || len(a.Actions[0].Tags) != 1 || len(a.Actions[1].Tags) != 2 {
		t.Fatal(a, err)
	}
	for _, script := range []string{`redirect "bad address";`, `redirect :copy "a@test";`, `require "copy"; keep :copy;`, `require "vacation"; vacation :unknown "x" "y";`, `require "vacation"; vacation "one"; vacation "two";`, `keep; reject "no";`, strings.Repeat(`keep;`, 33)} {
		if _, err = e.Evaluate(context.Background(), ctx, script); err == nil {
			t.Fatal("accepted invalid action", script)
		}
	}
}
