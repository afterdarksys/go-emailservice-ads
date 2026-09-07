package jmap

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/textproto"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	mm "github.com/emersion/go-message/mail"
	"github.com/google/uuid"
)

type composedPart struct {
	header   textproto.MIMEHeader
	data     []byte
	children []*composedPart
}

func (j *JMAPServer) composeMIME(ctx context.Context, user string, obj map[string]interface{}, header mm.Header) ([]byte, string, []string) {
	values := map[string]interface{}{}
	if v, exists := obj["bodyValues"]; exists {
		var ok bool
		values, ok = v.(map[string]interface{})
		if !ok {
			return nil, "invalidProperties", nil
		}
	}
	used, cids := map[string]bool{}, map[string]bool{}
	missing := map[string]bool{}
	nodes, size := 0, 0
	var prepare func(interface{}, int) (*composedPart, string)
	prepare = func(value interface{}, depth int) (*composedPart, string) {
		nodes++
		if nodes > mailstate.MaxMIMEParts || depth > mailstate.MaxMIMEDepth {
			return nil, "invalidProperties"
		}
		part, ok := value.(map[string]interface{})
		if !ok {
			return nil, "invalidProperties"
		}
		for key := range part {
			switch key {
			case "partId", "blobId", "type", "charset", "name", "disposition", "cid", "subParts", "size", "language", "location":
			default:
				return nil, "invalidProperties"
			}
		}
		str := func(key string) (string, bool) {
			if part[key] == nil {
				return "", true
			}
			v, ok := part[key].(string)
			return v, ok && safeHeader(v)
		}
		media, ok := str("type")
		if !ok {
			return nil, "invalidProperties"
		}
		if media == "" {
			media = "application/octet-stream"
		}
		media, params, err := mime.ParseMediaType(media)
		if err != nil || params["boundary"] != "" {
			return nil, "invalidProperties"
		}
		charset, ok := str("charset")
		if !ok {
			return nil, "invalidProperties"
		}
		if charset != "" {
			if params["charset"] != "" && !strings.EqualFold(params["charset"], charset) {
				return nil, "invalidProperties"
			}
			params["charset"] = charset
		}
		p := &composedPart{header: textproto.MIMEHeader{}}
		name, ok := str("name")
		if !ok {
			return nil, "invalidProperties"
		}
		disposition, ok := str("disposition")
		if !ok || (disposition != "" && disposition != "inline" && disposition != "attachment") {
			return nil, "invalidProperties"
		}
		if name != "" && disposition == "" {
			disposition = "attachment"
		}
		if disposition != "" {
			dp := map[string]string{}
			if name != "" {
				dp["filename"] = name
			}
			p.header.Set("Content-Disposition", mime.FormatMediaType(disposition, dp))
		}
		cid, ok := str("cid")
		if !ok || strings.IndexFunc(cid, func(r rune) bool { return r <= 32 || r >= 127 || r == '<' || r == '>' }) >= 0 || (cid != "" && cids[cid]) {
			return nil, "invalidProperties"
		}
		if cid != "" {
			cids[cid] = true
			p.header.Set("Content-ID", "<"+cid+">")
		}
		location, ok := str("location")
		if !ok {
			return nil, "invalidProperties"
		}
		if location != "" {
			p.header.Set("Content-Location", location)
		}
		if part["language"] != nil {
			langs := toStringSlice(part["language"])
			if len(langs) == 0 {
				return nil, "invalidProperties"
			}
			for _, lang := range langs {
				if lang == "" || strings.IndexFunc(lang, func(r rune) bool {
					return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
				}) >= 0 {
					return nil, "invalidProperties"
				}
			}
			p.header.Set("Content-Language", strings.Join(langs, ", "))
		}
		if strings.HasPrefix(media, "multipart/") {
			if media != "multipart/mixed" && media != "multipart/alternative" && media != "multipart/related" {
				return nil, "invalidProperties"
			}
			if part["partId"] != nil || part["blobId"] != nil || part["size"] != nil || params["charset"] != "" {
				return nil, "invalidProperties"
			}
			params["boundary"] = uuid.NewString()
			children, ok := part["subParts"].([]interface{})
			if !ok || len(children) == 0 {
				return nil, "invalidProperties"
			}
			for _, child := range children {
				c, kind := prepare(child, depth+1)
				if kind != "" {
					return nil, kind
				}
				p.children = append(p.children, c)
			}
		} else {
			if part["subParts"] != nil {
				return nil, "invalidProperties"
			}
			pid, ok := str("partId")
			if !ok {
				return nil, "invalidProperties"
			}
			blobID, ok := str("blobId")
			if !ok || (pid == "") == (blobID == "") {
				return nil, "invalidProperties"
			}
			if pid != "" {
				if used[pid] || !strings.HasPrefix(media, "text/") || part["size"] != nil || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
					return nil, "invalidProperties"
				}
				body, ok := values[pid].(map[string]interface{})
				if !ok {
					return nil, "invalidProperties"
				}
				for k, v := range body {
					if k != "value" && ((k != "isTruncated" && k != "isEncodingProblem") || v != false) {
						return nil, "invalidProperties"
					}
				}
				text, ok := body["value"].(string)
				if !ok || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
					return nil, "invalidProperties"
				}
				used[pid] = true
				p.data = []byte(text)
				params["charset"] = "utf-8"
			} else {
				if v := part["size"]; v != nil {
					n, ok := v.(float64)
					if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 9007199254740991 || math.Trunc(n) != n {
						return nil, "invalidProperties"
					}
				}
				store, ok := j.store.(mailstate.ImportStore)
				if !ok {
					return nil, "forbidden"
				}
				blob, err := store.GetBlob(ctx, user, blobID)
				if errors.Is(err, mailstate.ErrBlobNotFound) {
					missing[blobID] = true
				} else if err != nil {
					return nil, "serverFail"
				} else {
					p.data = blob.Data
				}
			}
			size += len(p.data)
			if size > mailstate.MaxUploadBytes {
				return nil, "tooLarge"
			}
			p.header.Set("Content-Transfer-Encoding", "base64")
		}
		p.header.Set("Content-Type", mime.FormatMediaType(media, params))
		return p, ""
	}
	var root interface{}
	if structure, exists := obj["bodyStructure"]; exists {
		for _, key := range []string{"textBody", "htmlBody", "attachments"} {
			if _, exists := obj[key]; exists {
				return nil, "invalidProperties", nil
			}
		}
		root = structure
	} else {
		bodies, inline, attachments := []interface{}{}, []interface{}{}, []interface{}{}
		for _, key := range []string{"textBody", "htmlBody"} {
			if v, exists := obj[key]; exists {
				list, ok := v.([]interface{})
				if !ok || len(list) > 1 {
					return nil, "invalidProperties", nil
				}
				for _, v := range list {
					part, ok := v.(map[string]interface{})
					if !ok {
						return nil, "invalidProperties", nil
					}
					copy := map[string]interface{}{}
					for k, v := range part {
						copy[k] = v
					}
					media := "text/plain"
					if key == "htmlBody" {
						media = "text/html"
					}
					if copy["type"] != nil && copy["type"] != media {
						return nil, "invalidProperties", nil
					}
					copy["type"] = media
					bodies = append(bodies, copy)
				}
			}
		}
		if v, exists := obj["attachments"]; exists {
			list, ok := v.([]interface{})
			if !ok {
				return nil, "invalidProperties", nil
			}
			for _, v := range list {
				part, ok := v.(map[string]interface{})
				if !ok {
					return nil, "invalidProperties", nil
				}
				copy := map[string]interface{}{}
				for k, v := range part {
					copy[k] = v
				}
				if copy["disposition"] == nil {
					copy["disposition"] = "attachment"
				}
				if copy["disposition"] == "inline" {
					inline = append(inline, copy)
				} else {
					attachments = append(attachments, copy)
				}
			}
		}
		// Empty convenience bodies remain compatible with existing draft creation.
		if len(bodies) == 0 {
			id := "__empty"
			for {
				if _, exists := values[id]; !exists {
					break
				}
				id += "_"
			}
			copy := map[string]interface{}{}
			for k, v := range values {
				copy[k] = v
			}
			copy[id] = map[string]interface{}{"value": ""}
			values = copy
			bodies = append(bodies, map[string]interface{}{"partId": id, "type": "text/plain"})
		}
		root = bodies[0]
		if len(bodies) > 1 {
			root = map[string]interface{}{"type": "multipart/alternative", "subParts": bodies}
		}
		if len(inline) > 0 {
			root = map[string]interface{}{"type": "multipart/related", "subParts": append([]interface{}{root}, inline...)}
		}
		if len(attachments) > 0 {
			root = map[string]interface{}{"type": "multipart/mixed", "subParts": append([]interface{}{root}, attachments...)}
		}
	}
	p, kind := prepare(root, 1)
	if kind != "" {
		return nil, kind, nil
	}
	if len(used) != len(values) {
		return nil, "invalidProperties", nil
	}
	if len(missing) > 0 {
		ids := []string{}
		for id := range missing {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return nil, "blobNotFound", ids
	}
	for fields := header.Fields(); fields.Next(); {
		p.header.Add(fields.Key(), fields.Value())
	}
	p.header.Set("MIME-Version", "1.0")
	var buffer bytes.Buffer
	bounded := &mimeLimitWriter{w: &buffer, remaining: mailstate.MaxUploadBytes}
	if err := writeComposedPart(bounded, p, true); err != nil {
		if errors.Is(err, errMIMESize) {
			return nil, "tooLarge", nil
		}
		return nil, "serverFail", nil
	}
	return buffer.Bytes(), "", nil
}

var errMIMESize = errors.New("encoded MIME exceeds limit")

type mimeLimitWriter struct {
	w         io.Writer
	remaining int
}

func (w *mimeLimitWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, errMIMESize
	}
	n, err := w.w.Write(p)
	w.remaining -= n
	return n, err
}

// Base64 lines must be wrapped for SMTP, including binary attachment payloads.
type mimeLineWriter struct {
	w      io.Writer
	column int
}

func (w *mimeLineWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		if w.column == 76 {
			if _, err := io.WriteString(w.w, "\r\n"); err != nil {
				return total, err
			}
			w.column = 0
		}
		n := min(76-w.column, len(p))
		written, err := w.w.Write(p[:n])
		total += written
		w.column += written
		p = p[written:]
		if err != nil {
			return total, err
		}
		if written != n {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
func writeComposedPart(w io.Writer, p *composedPart, headers bool) error {
	var mw *multipart.Writer
	if len(p.children) > 0 {
		mw = multipart.NewWriter(w)
		_, params, _ := mime.ParseMediaType(p.header.Get("Content-Type"))
		if err := mw.SetBoundary(params["boundary"]); err != nil {
			return err
		}
	}
	if headers {
		keys := []string{}
		for key := range p.header {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for _, value := range p.header[key] {
				if _, err := io.WriteString(w, key+": "+value+"\r\n"); err != nil {
					return err
				}
			}
		}
		if _, err := io.WriteString(w, "\r\n"); err != nil {
			return err
		}
	}
	if mw != nil {
		for _, child := range p.children {
			part, err := mw.CreatePart(child.header)
			if err != nil {
				return err
			}
			if err := writeComposedPart(part, child, false); err != nil {
				return err
			}
		}
		return mw.Close()
	}
	encoder := base64.NewEncoder(base64.StdEncoding, &mimeLineWriter{w: w})
	if _, err := encoder.Write(p.data); err != nil {
		return err
	}
	return encoder.Close()
}
