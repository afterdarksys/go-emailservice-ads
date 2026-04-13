package imap

import (
	"context"
	"errors"

	imap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
)

// Backend implements go-imap backend.Backend and backend.BackendUpdater.
// RFC 3501 - INTERNET MESSAGE ACCESS PROTOCOL - VERSION 4rev1
type Backend struct {
	logger    *zap.Logger
	store     Store
	validator *auth.Validator
	updatesCh chan backend.Update
}

// NewBackend creates a new IMAP backend. If store implements Updater, IMAP
// IDLE push notifications are automatically wired up.
func NewBackend(logger *zap.Logger, store Store, validator *auth.Validator) *Backend {
	b := &Backend{
		logger:    logger,
		store:     store,
		validator: validator,
		updatesCh: make(chan backend.Update, 64),
	}
	if upd, ok := store.(Updater); ok {
		go b.relayDeliveryUpdates(upd.DeliveryUpdates())
	}
	return b
}

// relayDeliveryUpdates converts [username, mailbox] delivery notifications
// from the store into go-imap MailboxUpdate events for connected IDLE clients.
func (b *Backend) relayDeliveryUpdates(ch <-chan [2]string) {
	for pair := range ch {
		username, mailbox := pair[0], pair[1]
		msgs, err := b.store.GetMessages(context.Background(), username, mailbox)
		count := uint32(0)
		if err == nil {
			count = uint32(len(msgs))
		}
		upd := &backend.MailboxUpdate{
			Update: backend.NewUpdate(username, mailbox),
			MailboxStatus: &imap.MailboxStatus{
				Name:     mailbox,
				Messages: count,
			},
		}
		select {
		case b.updatesCh <- upd:
		default: // drop — client will catch up on next NOOP/CHECK
		}
	}
}

// Updates implements backend.BackendUpdater for IMAP IDLE (RFC 2177) support.
func (b *Backend) Updates() <-chan backend.Update {
	return b.updatesCh
}

// Login authenticates a user.
// RFC 3501 Section 6.2.3 - LOGIN Command
func (b *Backend) Login(_ *imap.ConnInfo, username, password string) (backend.User, error) {
	_, err := b.validator.Authenticate(username, password)
	if err != nil {
		b.logger.Warn("IMAP authentication failed",
			zap.String("username", username),
			zap.Error(err))
		return nil, errors.New("authentication failed")
	}
	b.logger.Info("IMAP user authenticated", zap.String("username", username))
	return NewUser(b.logger, b.store, username), nil
}
