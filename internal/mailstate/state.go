// Package mailstate defines durable email synchronization operations shared by
// the mailbox store and JMAP, without coupling storage to HTTP handlers.
package mailstate

import (
	"context"
	"errors"
	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
	"time"
)

var ErrStateMismatch = errors.New("stateMismatch")
var ErrCannotCalculate = errors.New("cannotCalculateChanges")

type Message struct {
	imap.MessageSummary
	Folder    string
	MailboxID string
}

type Mailbox struct {
	ID, Path, Role           string
	Subscribed               bool
	SortOrder, Total, Unread int
}
type MailboxPatch struct {
	Name       *string
	Parent     *string // Empty means top-level; nil means unchanged.
	Subscribed *bool
	SortOrder  *int
}
type MailboxSetResult struct {
	OldState, NewState                   string
	Created                              map[string]string
	Updated, Destroyed                   []string
	NotCreated, NotUpdated, NotDestroyed map[string]string
}
type MailboxStore interface {
	MailboxSnapshot(context.Context, string) ([]Mailbox, string, error)
	SetMailboxes(context.Context, string, string, map[string]MailboxPatch, map[string]MailboxPatch, []string) (MailboxSetResult, error)
	MailboxChanges(context.Context, string, string, int) (Changes, error)
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

// EmailPatch describes keyword and mailbox membership changes to one email.
// A nil Mailboxes leaves membership unchanged; otherwise it replaces the set.
type EmailPatch struct {
	Keywords                      Patch
	Mailboxes                     *[]string
	AddMailboxes, RemoveMailboxes []string
}
type EmailSetResult struct {
	Created                  map[string]ImportedEmail
	NotCreated               map[string]string
	OldState, NewState       string
	Updated, Destroyed       []string
	NotUpdated, NotDestroyed map[string]string
}
type EmailMutator interface {
	SetEmails(context.Context, string, string, map[string]EmailPatch, []string) (EmailSetResult, error)
}

const MaxUploadBytes = 10 * 1024 * 1024

var ErrBlobNotFound = errors.New("blobNotFound")
var ErrUploadQuota = errors.New("uploadQuota")

type Blob struct {
	ID, MediaType string
	Data          []byte
}
type EmailImport struct {
	BlobID, MailboxID string
	Flags             []string
	ReceivedAt        time.Time
}
type ImportedEmail struct {
	ID   string
	Size int
}
type ImportResult struct {
	OldState, NewState string
	Created            map[string]ImportedEmail
	NotCreated         map[string]string
}
type ImportStore interface {
	UploadBlob(context.Context, string, string, []byte) (Blob, error)
	GetBlob(context.Context, string, string) (Blob, error)
	ImportEmails(context.Context, string, string, map[string]EmailImport) (ImportResult, error)
}

type EmailCreation struct {
	Data       []byte
	MailboxID  string
	Flags      []string
	ReceivedAt time.Time
}
type EmailCreator interface {
	SetEmailsWithCreates(context.Context, string, string, map[string]EmailCreation, map[string]EmailPatch, []string) (EmailSetResult, error)
}

type Submission struct {
	ID             string                 `json:"id"`
	EmailID        string                 `json:"emailId"`
	IdentityID     string                 `json:"identityId"`
	ThreadID       string                 `json:"threadId"`
	SendAt         string                 `json:"sendAt"`
	UndoStatus     string                 `json:"undoStatus"`
	Envelope       map[string]interface{} `json:"envelope"`
	DeliveryStatus interface{}            `json:"deliveryStatus"`
}
type Submitter interface {
	Submit(context.Context, string, string, string, string, []string, []byte) (Submission, error)
	Submissions(context.Context, string) ([]Submission, error)
}
