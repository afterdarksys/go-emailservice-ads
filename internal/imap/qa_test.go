package imap

import (
	"context"
	"errors"
	"net/textproto"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap"
	"go.uber.org/zap"
)

type failingFlagStore struct{ mailboxTestStore }

func (s *failingFlagStore) UpdateMessageFlags(context.Context, string, string, string, goimap.FlagsOp, []string) error {
	return errors.New("disk failure")
}
func TestStorePropagatesPersistenceFailure(t *testing.T) {
	s := &failingFlagStore{mailboxTestStore: mailboxTestStore{messages: []MessageSummary{{ID: "a", UID: 5}}}}
	set := new(goimap.SeqSet)
	set.Add("*")
	if err := NewMailbox(zap.NewNop(), s, "alice", "INBOX").UpdateMessagesFlags(true, set, goimap.AddFlags, []string{goimap.SeenFlag}); err == nil {
		t.Fatal("STORE acknowledged failed write")
	}
}
func TestSearchHonorsMIMEHeadersDatesAndWildcards(t *testing.T) {
	raw := []byte("From: sender@example.test\r\nTo: alice@example.test\r\nDate: Mon, 02 Jan 2006 15:04:05 -0700\r\nSubject: =?UTF-8?Q?caf=C3=A9?=\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8=\r\n")
	s := &mailboxTestStore{messages: []MessageSummary{{ID: "a", UID: 10, Size: int64(len(raw)), Date: time.Date(2020, 2, 3, 0, 0, 0, 0, time.UTC)}}, raw: map[string][]byte{"a": raw}}
	box := NewMailbox(zap.NewNop(), s, "alice", "INBOX")
	for _, tc := range []struct {
		c    *goimap.SearchCriteria
		want int
	}{
		{&goimap.SearchCriteria{Header: textproto.MIMEHeader{"To": []string{"alice"}}, Body: []string{"HELLO"}}, 1},
		{&goimap.SearchCriteria{Header: textproto.MIMEHeader{"To": []string{"bob"}}}, 0},
		{&goimap.SearchCriteria{Text: []string{"CAFÉ"}}, 1},
		{&goimap.SearchCriteria{SentBefore: time.Date(2007, 1, 1, 0, 0, 0, 0, time.UTC)}, 1},
		{&goimap.SearchCriteria{Before: time.Date(2007, 1, 1, 0, 0, 0, 0, time.UTC)}, 0},
	} {
		ids, err := box.SearchMessages(true, tc.c)
		if err != nil || len(ids) != tc.want {
			t.Fatalf("search %+v = %v %v", tc.c, ids, err)
		}
	}
	set := new(goimap.SeqSet)
	set.Add("*")
	ch := make(chan *goimap.Message, 1)
	if err := box.ListMessages(true, set, []goimap.FetchItem{goimap.FetchUid}, ch); err != nil {
		t.Fatal(err)
	}
	if msg := <-ch; msg == nil || msg.Uid != 10 {
		t.Fatal("UID * did not select last message")
	}
	set = new(goimap.SeqSet)
	set.Add("50:*")
	if ids := selectedIDs(s.messages, true, set); len(ids) != 1 {
		t.Fatal("reversed dynamic range failed")
	}
}
