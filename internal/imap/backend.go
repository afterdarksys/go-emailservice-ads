package imap

import (
	"errors"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
)

// Backend implements the go-imap backend.Backend interface
// RFC 3501 - INTERNET MESSAGE ACCESS PROTOCOL - VERSION 4rev1
type Backend struct {
	logger    *zap.Logger
	store     Store
	validator *auth.Validator
}

// NewBackend creates a new IMAP backend
func NewBackend(logger *zap.Logger, store Store, validator *auth.Validator) *Backend {
	return &Backend{
		logger:    logger,
		store:     store,
		validator: validator,
	}
}

// Login authenticates a user
// RFC 3501 Section 6.2.3 - LOGIN Command
func (b *Backend) Login(_ *imap.ConnInfo, username, password string) (backend.User, error) {
	// Use our existing auth validator
	_, err := b.validator.Authenticate(username, password)
	if err != nil {
		b.logger.Warn("IMAP authentication failed",
			zap.String("username", username),
			zap.Error(err))
		return nil, errors.New("authentication failed")
	}

	b.logger.Info("IMAP user authenticated",
		zap.String("username", username))

	return NewUser(b.logger, b.store, username), nil
}
