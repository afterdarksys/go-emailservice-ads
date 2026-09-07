package smtpd

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/security"
	"net/mail"
	"strings"
	"time"
)

func (qm *QueueManager) enqueueDMARCReport(ctx context.Context, to, id string, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a, err := mail.ParseAddress(to)
	if err != nil || a.Address != to || strings.ContainsAny(to+id, "\r\n") {
		return fmt.Errorf("invalid report destination")
	}
	var report security.DMARCReport
	if err = xml.Unmarshal(raw, &report); err != nil {
		return err
	}
	if strings.ContainsAny(report.Published.Domain, "\r\n") {
		return fmt.Errorf("invalid report domain")
	}
	filename := fmt.Sprintf("%s!%s!%d!%d!%s.xml", qm.hostname, report.Published.Domain, report.Metadata.Range.Begin, report.Metadata.Range.End, id)
	encoded := base64.StdEncoding.EncodeToString(raw)
	var body strings.Builder
	for len(encoded) > 76 {
		body.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	body.WriteString(encoded + "\r\n")
	header := fmt.Sprintf("From: postmaster@%s\r\nTo: %s\r\nDate: %s\r\nSubject: Report Domain: %s Submitter: %s Report-ID: %s\r\nMessage-ID: <%s@%s>\r\nAuto-Submitted: auto-generated\r\nMIME-Version: 1.0\r\nContent-Type: application/xml\r\nContent-Disposition: attachment; filename=%q\r\nContent-Transfer-Encoding: base64\r\n\r\n", qm.hostname, to, time.Now().Format(time.RFC1123Z), report.Published.Domain, qm.hostname, id, id, qm.hostname, filename)
	return qm.Enqueue(&Message{From: "", To: []string{to}, Data: []byte(header + body.String()), Tier: TierEmergency, IsBounce: true, IsTLSReport: true})
}
