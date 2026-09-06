package imap

import (
	"context"
	"fmt"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap"
)

// MutableStore persists mailbox membership and APPEND metadata as one operation.
// Backends without this extension continue to fail unsupported mutations.
type MutableStore interface {
	ListFolders(context.Context, string, bool) ([]string, error)
	CreateFolder(context.Context, string, string) error
	DeleteFolder(context.Context, string, string) error
	RenameFolder(context.Context, string, string, string) error
	SubscribeFolder(context.Context, string, string, bool) error
	AppendMessage(context.Context, string, string, []byte, []string, time.Time) (string, error)
	CopyMessageIDs(context.Context, string, string, string, []string) error
}

func NormalizeMailbox(name string) (string, error) {
	if strings.EqualFold(name, "INBOX") {
		return "INBOX", nil
	}
	if name == "" || len(name) > 255 || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return "", fmt.Errorf("invalid mailbox name")
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid mailbox hierarchy")
		}
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			return "", fmt.Errorf("invalid mailbox name")
		}
	}
	return name, nil
}

// Resolve '*' against the current mailbox snapshot, including reversed n:* ranges.
func resolveSet(set *goimap.SeqSet, max uint32) *goimap.SeqSet {
	if set == nil {
		return nil
	}
	out := new(goimap.SeqSet)
	if max == 0 {
		return out
	}
	for _, seq := range set.Set {
		a, b := seq.Start, seq.Stop
		if a == 0 {
			a = max
		}
		if b == 0 {
			b = max
		}
		out.AddRange(a, b)
	}
	return out
}
func selectedIDs(messages []MessageSummary, uid bool, set *goimap.SeqSet) []string {
	max := uint32(len(messages))
	if uid && len(messages) > 0 {
		max = messages[len(messages)-1].UID
	}
	set = resolveSet(set, max)
	var ids []string
	for i, msg := range messages {
		n := uint32(i + 1)
		if uid {
			n = msg.UID
		}
		if set == nil || set.Contains(n) {
			ids = append(ids, msg.ID)
		}
	}
	return ids
}
