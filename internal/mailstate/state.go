// Package mailstate defines durable email synchronization operations shared by
// the mailbox store and JMAP, without coupling storage to HTTP handlers.
package mailstate

import (
	"context"
	"errors"
	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
)

var ErrStateMismatch = errors.New("stateMismatch")
var ErrCannotCalculate = errors.New("cannotCalculateChanges")

type Message struct {
	imap.MessageSummary
	Folder string
}
type Patch struct {
	Replace     *[]string
	Add, Remove []string
}
type SetResult struct {
	OldState, NewState string
	Updated            []string
	NotFound           []string
}
type Changes struct {
	OldState, NewState          string
	HasMore                     bool
	Created, Updated, Destroyed []string
}
type Store interface {
	EmailSnapshot(context.Context, string) (map[string]Message, string, error)
	SetEmailKeywords(context.Context, string, string, map[string]Patch) (SetResult, error)
	EmailChanges(context.Context, string, string, int) (Changes, error)
}
