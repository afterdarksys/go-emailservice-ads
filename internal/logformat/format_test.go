package logformat

import (
	"bytes"
	"strings"
	"testing"
)

func TestDetectAndConvert(t *testing.T) {
	for _, tc := range []struct{ raw, format string }{
		{"{\"message\":\"one\"}\n{\"message\":\"two\"}", "json"},
		{"---\nmessage: one\n---\nmessage: two\n", "yaml"},
		{"<134>1 2026-09-06T12:00:00Z hub app 1 id [audit x=\"a]b\"] hello", "syslog"},
		{"Sep  6 12:00:00 hub mailhub: hello", "syslog"},
	} {
		rows, f, e := Decode([]byte(tc.raw), "auto")
		if e != nil || f != tc.format || len(rows) == 0 {
			t.Fatalf("%s: %s %v", tc.format, f, e)
		}
		for _, output := range []string{"json", "yaml", "syslog"} {
			var buf bytes.Buffer
			if e := Encode(&buf, output, rows); e != nil {
				t.Fatal(e)
			}
			if _, _, e := Decode(buf.Bytes(), "auto"); e != nil {
				t.Fatalf("%s roundtrip: %v", output, e)
			}
		}
	}
	for _, raw := range []string{"random prose", "{bad", "<134>1 t h a p m [broken"} {
		if _, _, e := Decode([]byte(raw), "auto"); e == nil {
			t.Fatalf("accepted malformed %q", raw)
		}
	}
}

func TestYAMLConversionPreservesLargeInteger(t *testing.T) {
	rows, _, err := Decode([]byte(`{"sequence":9007199254740993}`), "auto")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Encode(&buf, "yaml", rows); err != nil {
		t.Fatal(err)
	}
	rows, _, err = Decode(buf.Bytes(), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := Encode(&buf, "json", rows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"sequence":9007199254740993`) {
		t.Fatal("numeric field changed", buf.String())
	}
}
