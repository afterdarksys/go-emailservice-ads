package bounce

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBounceConfigurationAndInjection(t *testing.T) {
	bg := NewBounceGenerator("mail.test", "postmaster@mail.test")
	no := false
	bg.Configure(Config{IncludeOriginalHeaders: &no, Postmaster: "dsn@mail.test"})
	raw, err := bg.GenerateBounce("sender@mail.test", &BounceReason{SMTPCode: 550, EnhancedCode: "5.1.1", Recipient: "bad@mail.test", Message: "missing\r\nBcc: injected@mail.test", IsPermanent: true}, []byte("Subject: secret\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("\r\nBcc:")) || bytes.Contains(raw, []byte("Subject: secret")) || !bytes.Contains(raw, []byte("dsn@mail.test")) {
		t.Fatal("bounce configuration or injection protection failed")
	}
	bg.Configure(Config{MaxHeaderBytes: 64})
	if len(bg.extractHeaders([]byte(strings.Repeat("X", 1024)))) > 64 {
		t.Fatal("header budget ignored")
	}
	if err := (Config{InitialDelay: time.Hour, MaxDelay: time.Minute}).Validate(); err == nil {
		t.Fatal("invalid retry delays accepted")
	}
}
