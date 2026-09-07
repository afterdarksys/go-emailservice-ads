package mailstate

import (
	"fmt"
	"strings"
	"testing"
)

func TestMIMEParserRejectsMalformedAndExcessiveTrees(t *testing.T) {
	for _, raw := range []string{
		"Content-Type: multipart/mixed\r\n\r\nmissing boundary",
		"Content-Transfer-Encoding: base64\r\n\r\n@@@@",
		"Content-Transfer-Encoding: unsupported\r\n\r\ntext",
	} {
		if _, err := ParseMIME([]byte(raw)); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	raw := "Content-Type: text/plain\r\n\r\nleaf"
	for i := 0; i < MaxMIMEDepth; i++ {
		boundary := fmt.Sprintf("boundary%d", i)
		raw = "Content-Type: multipart/mixed; boundary=" + boundary + "\r\n\r\n--" + boundary + "\r\n" + raw + "\r\n--" + boundary + "--\r\n"
	}
	if _, err := ParseMIME([]byte(raw)); err == nil {
		t.Fatal("accepted excessive nesting")
	}
	raw = "Content-Type: multipart/mixed; boundary=x\r\n\r\n" + strings.Repeat("--x\r\nContent-Type: text/plain\r\n\r\nbody\r\n", MaxMIMEParts) + "--x--\r\n"
	if _, err := ParseMIME([]byte(raw)); err == nil {
		t.Fatal("accepted excessive part count")
	}
	for _, id := range []string{"email.part.0", "email.part.-1", "email.part.01", "email.part.99999999999999999999", "email.part.text"} {
		if _, _, err := SplitPartBlob(id); err == nil {
			t.Fatal(id)
		}
	}
}
