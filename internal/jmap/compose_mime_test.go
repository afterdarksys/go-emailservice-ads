package jmap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

func TestAdvancedCompositionRoundTripAndBlobOwnership(t *testing.T) {
	j, s, folder := compositionServer(t)
	ctx := context.Background()
	// Non-UTF-8 text is intentionally preserved as octets for reuse/download.
	source, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: source\r\nContent-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\ncaf=E9\r\n--x\r\nContent-Type: image/png\r\nContent-Transfer-Encoding: base64\r\n\r\nAAH/\r\n--x--\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	textBlob, err := s.GetBlob(ctx, "alice", source+".part.1")
	if err != nil || !bytes.Equal(textBlob.Data, []byte{'c', 'a', 'f', 0xe9}) {
		t.Fatal(textBlob, err)
	}
	raw, _ := s.FetchMessage(ctx, source)
	view := emailObject(source, raw, MessageOwnedSummary{})
	if view["bodyValues"].(map[string]interface{})["1"].(map[string]interface{})["value"] != "café" {
		t.Fatal(view)
	}
	_, err = s.GetBlob(ctx, "bob", source+".part.2")
	if !errors.Is(err, mailstate.ErrBlobNotFound) {
		t.Fatal(err)
	}
	leaf := func(v map[string]interface{}) interface{} { return v }
	obj := map[string]interface{}{"mailboxIds": map[string]interface{}{folder: true}, "subject": "nested café", "bodyValues": map[string]interface{}{"html": map[string]interface{}{"value": "<img src=\"cid:logo@example.test\">"}}, "bodyStructure": map[string]interface{}{"type": "multipart/mixed", "subParts": []interface{}{
		map[string]interface{}{"type": "multipart/alternative", "subParts": []interface{}{
			leaf(map[string]interface{}{"blobId": source + ".part.1", "type": "text/plain", "charset": "iso-8859-1"}),
			map[string]interface{}{"type": "multipart/related", "subParts": []interface{}{
				leaf(map[string]interface{}{"partId": "html", "type": "text/html"}),
				leaf(map[string]interface{}{"blobId": source + ".part.2", "type": "image/png", "cid": "logo@example.test", "disposition": "inline", "name": "café.png"}),
			}},
		}},
		leaf(map[string]interface{}{"blobId": source + ".part.2", "type": "application/octet-stream", "name": "copy.bin", "disposition": "attachment"}),
	}}}
	creation, kind, missing := j.buildEmail(ctx, "alice", obj)
	if kind != "" {
		t.Fatal(kind, missing)
	}
	result, err := s.SetEmailsWithCreates(ctx, "alice", "", map[string]mailstate.EmailCreation{"draft": creation}, nil, nil)
	if err != nil || len(result.Created) != 1 {
		t.Fatal(result, err)
	}
	mid := result.Created["draft"].ID
	snapshot := emailObject(mid, creation.Data, MessageOwnedSummary{})
	if snapshot["bodyStructure"].(map[string]interface{})["type"] != "multipart/mixed" {
		t.Fatal(snapshot)
	}
	attachments := snapshot["attachments"].([]any)
	if len(attachments) != 2 {
		t.Fatal(snapshot)
	}
	first := attachments[0].(map[string]interface{})
	if first["cid"] != "logo@example.test" || first["disposition"] != "inline" || first["name"] != "café.png" {
		t.Fatal(first)
	}
	for _, item := range attachments {
		blob, err := s.GetBlob(ctx, "alice", item.(map[string]interface{})["blobId"].(string))
		if err != nil || !bytes.Equal(blob.Data, []byte{0, 1, 255}) {
			t.Fatal(blob, err)
		}
	}
	// The new email holds an independent copy after its source is destroyed.
	if _, err = s.SetEmails(ctx, "alice", "", nil, []string{source}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetBlob(ctx, "alice", source+".part.2"); !errors.Is(err, mailstate.ErrBlobNotFound) {
		t.Fatal(err)
	}
	if blob, err := s.GetBlob(ctx, "alice", mid+".part.3"); err != nil || !bytes.Equal(blob.Data, []byte{0, 1, 255}) {
		t.Fatal(blob, err)
	}
}

func TestConvenienceInlineComposition(t *testing.T) {
	j, s, folder := compositionServer(t)
	ctx := context.Background()
	blob, err := s.UploadBlob(ctx, "alice", "image/png", []byte{0, 255})
	if err != nil {
		t.Fatal(err)
	}
	obj := draftObject(folder)
	obj["attachments"] = []interface{}{map[string]interface{}{"blobId": blob.ID, "type": "image/png", "disposition": "inline", "cid": "logo@example.test"}}
	creation, kind, _ := j.buildEmail(ctx, "alice", obj)
	if kind != "" {
		t.Fatal(kind)
	}
	view := emailObject("email", creation.Data, MessageOwnedSummary{})
	if view["bodyStructure"].(map[string]interface{})["type"] != "multipart/related" || len(view["attachments"].([]any)) != 1 || len(view["textBody"].([]any)) != 1 || len(view["htmlBody"].([]any)) != 1 {
		t.Fatal(view)
	}
}

func TestCompositionTreeValidationAndLimits(t *testing.T) {
	j, s, folder := compositionServer(t)
	ctx := context.Background()
	blob, _ := s.UploadBlob(ctx, "alice", "image/png", []byte{0, 255})
	body := func() map[string]interface{} { return map[string]interface{}{"partId": "text", "type": "text/plain"} }
	multipart := func(children ...interface{}) map[string]interface{} {
		return map[string]interface{}{"type": "multipart/mixed", "subParts": children}
	}
	deep := body()
	for i := 0; i < mailstate.MaxMIMEDepth; i++ {
		deep = multipart(deep)
	}
	children := []interface{}{}
	for i := 0; i < mailstate.MaxMIMEParts; i++ {
		children = append(children, map[string]interface{}{"blobId": blob.ID})
	}
	bad := []interface{}{
		map[string]interface{}{}, deep, multipart(children...), multipart(),
		map[string]interface{}{"partId": "text", "blobId": blob.ID, "type": "text/plain"},
		map[string]interface{}{"partId": "text", "type": "image/png"},
		map[string]interface{}{"partId": "text", "type": "text/plain", "size": float64(0)},
		map[string]interface{}{"partId": "text", "type": "text/plain", "cid": "x\r\nBcc:bad"},
		map[string]interface{}{"partId": "text", "type": "text/plain", "subParts": []interface{}{}},
		multipart(body(), body()),
		multipart(map[string]interface{}{"blobId": blob.ID, "cid": "same"}, map[string]interface{}{"blobId": blob.ID, "cid": "same"}),
		map[string]interface{}{"type": "multipart/mixed", "blobId": blob.ID, "subParts": []interface{}{body()}},
		map[string]interface{}{"partId": "text", "type": "text/plain", "header:Content-Transfer-Encoding": "binary"},
	}
	for i, structure := range bad {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			obj := map[string]interface{}{"mailboxIds": map[string]interface{}{folder: true}, "bodyStructure": structure, "bodyValues": map[string]interface{}{"text": map[string]interface{}{"value": "text"}}}
			if _, kind, _ := j.buildEmail(ctx, "alice", obj); kind != "invalidProperties" {
				t.Fatal(kind)
			}
		})
	}
	obj := map[string]interface{}{"mailboxIds": map[string]interface{}{folder: true}, "bodyStructure": multipart(map[string]interface{}{"blobId": "missing-b"}, map[string]interface{}{"blobId": "missing-a"}, map[string]interface{}{"blobId": "missing-a"})}
	if _, kind, missing := j.buildEmail(ctx, "alice", obj); kind != "blobNotFound" || !reflect.DeepEqual(missing, []string{"missing-a", "missing-b"}) {
		t.Fatal(kind, missing)
	}
	large, _ := s.UploadBlob(ctx, "alice", "application/octet-stream", []byte(strings.Repeat("x", mailstate.MaxUploadBytes/2)))
	obj["bodyStructure"] = multipart(map[string]interface{}{"blobId": large.ID}, map[string]interface{}{"blobId": large.ID})
	if _, kind, _ := j.buildEmail(ctx, "alice", obj); kind != "tooLarge" {
		t.Fatal(kind)
	}
}

func TestUnreadableMIMEIsNotReportedAsEmptyEmail(t *testing.T) {
	j, s, _ := compositionServer(t)
	ctx := context.Background()
	mid, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: malformed\r\nContent-Type: multipart/mixed; boundary=missing\r\n\r\nno parts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []MethodCall{
		{Name: "Email/get", Arguments: map[string]interface{}{"ids": []interface{}{mid}}},
		{Name: "Email/query", Arguments: map[string]interface{}{"filter": map[string]interface{}{"text": "body"}}},
	} {
		r := j.processMethodCall(ctx, "alice", call)
		if r.Name != "error" || r.Arguments["type"] != "serverFail" {
			t.Fatal(r)
		}
	}
	// Original bytes remain available for inspection and recovery.
	if _, err = s.GetBlob(ctx, "alice", mid); err != nil {
		t.Fatal(err)
	}
}
