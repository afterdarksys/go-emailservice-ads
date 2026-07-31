package imap

import (
	"errors"

	"github.com/emersion/go-imap/backend"
	"go.uber.org/zap"
)

var errMailboxMutationUnsupported = errors.New("mailbox mutation is not supported by this storage backend")

// User implements the go-imap backend.User interface
type User struct {
	logger   *zap.Logger
	store    Store
	username string
}

// NewUser creates a new IMAP user session
func NewUser(logger *zap.Logger, store Store, username string) *User {
	return &User{
		logger:   logger,
		store:    store,
		username: username,
	}
}

// Username returns the username
func (u *User) Username() string {
	return u.username
}

// ListMailboxes returns a list of mailboxes available to this user
// RFC 3501 Section 6.3.8 - LIST Command
func (u *User) ListMailboxes(subscribed bool) ([]backend.Mailbox, error) {
	// Standard mailboxes for each user
	mailboxNames := []string{"INBOX", "Sent", "Drafts", "Trash", "Spam"}

	mailboxes := make([]backend.Mailbox, 0, len(mailboxNames))
	for _, name := range mailboxNames {
		mailboxes = append(mailboxes, NewMailbox(u.logger, u.store, u.username, name))
	}

	return mailboxes, nil
}

// GetMailbox returns a specific mailbox
func (u *User) GetMailbox(name string) (backend.Mailbox, error) {
	// Validate mailbox exists
	validMailboxes := map[string]bool{
		"INBOX":  true,
		"Sent":   true,
		"Drafts": true,
		"Trash":  true,
		"Spam":   true,
	}

	if !validMailboxes[name] {
		return nil, backend.ErrNoSuchMailbox
	}

	return NewMailbox(u.logger, u.store, u.username, name), nil
}

// CreateMailbox creates a new mailbox
// RFC 3501 Section 6.3.3 - CREATE Command
func (u *User) CreateMailbox(name string) error {
	return errMailboxMutationUnsupported
}

// DeleteMailbox deletes a mailbox
// RFC 3501 Section 6.3.4 - DELETE Command
func (u *User) DeleteMailbox(name string) error {
	if name == "INBOX" {
		return errors.New("cannot delete INBOX")
	}
	return errMailboxMutationUnsupported
}

// RenameMailbox renames a mailbox
// RFC 3501 Section 6.3.5 - RENAME Command
func (u *User) RenameMailbox(existingName, newName string) error {
	if existingName == "INBOX" {
		return errors.New("cannot rename INBOX")
	}
	return errMailboxMutationUnsupported
}

// Logout closes the user session
func (u *User) Logout() error {
	u.logger.Info("User logged out", zap.String("user", u.username))
	return nil
}
