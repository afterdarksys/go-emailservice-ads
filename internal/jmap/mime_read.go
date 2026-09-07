package jmap

import (
	"bytes"
	"io"
	"mime"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"golang.org/x/net/html/charset"
)

func addMIMEObjects(obj map[string]interface{}, id string, raw []byte) error {
	root, err := mailstate.ParseMIME(raw)
	if err != nil {
		return err
	}
	values := map[string]interface{}{}
	texts, htmls, attachments := []any{}, []any{}, []any{}
	var walk func(*mailstate.MIMEPart) map[string]interface{}
	walk = func(p *mailstate.MIMEPart) map[string]interface{} {
		optional := func(s string) interface{} {
			if s == "" {
				return nil
			}
			return s
		}
		disposition, params, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		name := params["filename"]
		if name == "" {
			name = p.Params["name"]
		}
		if decoded, err := new(mime.WordDecoder).DecodeHeader(name); err == nil {
			name = decoded
		}
		item := map[string]interface{}{"partId": nil, "blobId": nil, "type": p.Type, "size": len(p.Data), "name": optional(name), "charset": optional(p.Params["charset"]), "disposition": optional(disposition), "cid": optional(strings.Trim(p.Header.Get("Content-ID"), "<>")), "location": optional(p.Header.Get("Content-Location")), "language": nil, "subParts": nil}
		if language := p.Header.Get("Content-Language"); language != "" {
			langs := strings.Split(language, ",")
			for i := range langs {
				langs[i] = strings.TrimSpace(langs[i])
			}
			item["language"] = langs
		}
		if strings.HasPrefix(p.Type, "multipart/") {
			children := []interface{}{}
			for _, child := range p.Children {
				children = append(children, walk(child))
			}
			item["subParts"] = children
			return item
		}
		pid := strconv.Itoa(p.Number)
		item["partId"], item["blobId"] = pid, id+".part."+pid
		if (p.Type == "text/plain" || p.Type == "text/html") && disposition != "attachment" {
			data := p.Data
			problem := false
			if label := p.Params["charset"]; label != "" && !strings.EqualFold(label, "utf-8") {
				reader, err := charset.NewReaderLabel(label, bytes.NewReader(data))
				if err != nil {
					problem = true
				} else {
					decoded, err := io.ReadAll(io.LimitReader(reader, mailstate.MaxMIMEReadBytes+1))
					if err != nil || len(decoded) > mailstate.MaxMIMEReadBytes {
						problem = true
					} else {
						data = decoded
					}
				}
			}
			problem = problem || !utf8.Valid(data)
			values[pid] = map[string]interface{}{"value": strings.ToValidUTF8(string(data), "\uFFFD"), "isEncodingProblem": problem, "isTruncated": false}
			if p.Type == "text/html" {
				htmls = append(htmls, item)
			} else {
				texts = append(texts, item)
			}
		} else {
			attachments = append(attachments, item)
		}
		return item
	}
	obj["bodyStructure"] = walk(root)
	obj["textBody"], obj["htmlBody"], obj["attachments"], obj["bodyValues"], obj["hasAttachment"] = texts, htmls, attachments, values, len(attachments) > 0
	return nil
}
