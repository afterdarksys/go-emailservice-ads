package mailstate

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
)

const MaxMIMEParts = 128
const MaxMIMEDepth = 16
const MaxMIMEReadBytes = 64 << 20

// MIMEPart retains transfer-decoded octets without charset conversion. Leaf
// numbering is depth-first, matching the existing JMAP .part.N blob IDs.
type MIMEPart struct {
	Header   textproto.MIMEHeader
	Type     string
	Params   map[string]string
	Number   int
	Data     []byte
	Children []*MIMEPart
}

func ParseMIME(raw []byte) (*MIMEPart, error) {
	if len(raw) > MaxMIMEReadBytes {
		return nil, fmt.Errorf("MIME message exceeds read limit")
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	nodes, leaves, remaining := 0, 0, MaxMIMEReadBytes
	var parse func(textproto.MIMEHeader, io.Reader, int) (*MIMEPart, error)
	parse = func(h textproto.MIMEHeader, r io.Reader, depth int) (*MIMEPart, error) {
		nodes++
		if nodes > MaxMIMEParts || depth > MaxMIMEDepth {
			return nil, fmt.Errorf("MIME tree exceeds limits")
		}
		media, params, err := mime.ParseMediaType(h.Get("Content-Type"))
		if h.Get("Content-Type") == "" {
			media, params, err = "text/plain", map[string]string{}, nil
		}
		if err != nil {
			return nil, err
		}
		p := &MIMEPart{Header: h, Type: media, Params: params}
		if strings.HasPrefix(media, "multipart/") {
			mr := multipart.NewReader(r, params["boundary"])
			for {
				child, err := mr.NextRawPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, err
				}
				parsed, err := parse(child.Header, child, depth+1)
				if err != nil {
					return nil, err
				}
				p.Children = append(p.Children, parsed)
			}
			return p, nil
		}
		switch strings.ToLower(strings.TrimSpace(h.Get("Content-Transfer-Encoding"))) {
		case "base64":
			r = base64.NewDecoder(base64.StdEncoding, r)
		case "quoted-printable":
			r = quotedprintable.NewReader(r)
		case "", "7bit", "8bit", "binary":
		default:
			return nil, fmt.Errorf("unsupported MIME transfer encoding")
		}
		p.Data, err = io.ReadAll(io.LimitReader(r, int64(remaining)+1))
		if err != nil {
			return nil, err
		}
		remaining -= len(p.Data)
		if remaining < 0 {
			return nil, fmt.Errorf("decoded MIME exceeds read limit")
		}
		leaves++
		p.Number = leaves
		return p, nil
	}
	return parse(textproto.MIMEHeader(msg.Header), msg.Body, 1)
}

func SplitPartBlob(id string) (string, int, error) {
	i := strings.LastIndex(id, ".part.")
	if i < 0 {
		return id, 0, nil
	}
	n, err := strconv.Atoi(id[i+6:])
	if err != nil || n < 1 || n > MaxMIMEParts || strconv.Itoa(n) != id[i+6:] {
		return "", 0, ErrBlobNotFound
	}
	return id[:i], n, nil
}

func MIMEBlob(raw []byte, number int) ([]byte, string, error) {
	root, err := ParseMIME(raw)
	if err != nil {
		return nil, "", err
	}
	var find func(*MIMEPart) *MIMEPart
	find = func(p *MIMEPart) *MIMEPart {
		if p.Number == number {
			return p
		}
		for _, c := range p.Children {
			if found := find(c); found != nil {
				return found
			}
		}
		return nil
	}
	p := find(root)
	if p == nil {
		return nil, "", ErrBlobNotFound
	}
	return p.Data, mime.FormatMediaType(p.Type, p.Params), nil
}
