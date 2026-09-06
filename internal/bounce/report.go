package bounce

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
)

type Report struct {
	Recipient  string `json:"recipient"`
	Action     string `json:"action"`
	Status     string `json:"status"`
	EnvelopeID string `json:"envelope_id"`
}

func ParseReport(raw []byte) ([]Report, error) {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, nil
	}
	typ, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || typ != "multipart/report" || params["report-type"] != "delivery-status" {
		return nil, nil
	}
	reader := multipart.NewReader(message.Body, params["boundary"])
	for i := 0; i < 10; i++ {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		kind, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if kind != "message/delivery-status" {
			part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, 65537))
		part.Close()
		if err != nil || len(data) > 65536 {
			return nil, fmt.Errorf("DSN report exceeds limit")
		}
		headers := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
		top, err := headers.ReadMIMEHeader()
		if err != nil {
			return nil, err
		}
		reports := []Report{}
		for len(reports) < 100 {
			h, err := headers.ReadMIMEHeader()
			if err == io.EOF && len(h) == 0 {
				break
			}
			if err != nil && err != io.EOF {
				return nil, err
			}
			_, address, ok := strings.Cut(h.Get("Final-Recipient"), ";")
			if !ok {
				return nil, fmt.Errorf("missing final recipient")
			}
			address = strings.TrimSpace(address)
			a, err := mail.ParseAddress(address)
			if err != nil || a.Address != address {
				return nil, fmt.Errorf("invalid DSN recipient")
			}
			status := h.Get("Status")
			if len(status) < 5 || (status[0] != '2' && status[0] != '4' && status[0] != '5') {
				return nil, fmt.Errorf("invalid DSN status")
			}
			reports = append(reports, Report{Recipient: address, Status: status, Action: h.Get("Action"), EnvelopeID: top.Get("Original-Envelope-Id")})
		}
		return reports, nil
	}
	return nil, fmt.Errorf("missing delivery-status part")
}
