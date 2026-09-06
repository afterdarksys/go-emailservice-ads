package imap

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"strings"

	imap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
)

// MasterAuthConfig enables Dovecot-style master (proxy) logins of the form
// "<user><sep><master_user>" for trusted control-plane services.
//
// Threats: a master credential grants read access to EVERY mailbox. Protects
// against use from untrusted networks via a mandatory source-IP allowlist and
// against credential guessing via constant-time comparison; it does NOT
// protect against a compromised allowlisted host or a leaked master password —
// rotate the password if either is suspected. Fail-closed: missing user,
// missing password, or an empty allowlist disables the feature entirely.
type MasterAuthConfig struct {
	User       string
	Password   string
	AllowedIPs []string
	Separator  string
}

func (m MasterAuthConfig) enabled() bool {
	return m.User != "" && m.Password != "" && len(m.AllowedIPs) > 0
}

func (m MasterAuthConfig) separator() string {
	if m.Separator == "" {
		return "*"
	}
	return m.Separator
}

// Backend implements go-imap backend.Backend and backend.BackendUpdater.
// RFC 3501 - INTERNET MESSAGE ACCESS PROTOCOL - VERSION 4rev1
type Backend struct {
	logger    *zap.Logger
	store     Store
	validator *auth.Validator
	master    MasterAuthConfig
	updatesCh chan backend.Update
}

// NewBackend creates a new IMAP backend. If store implements Updater, IMAP
// IDLE push notifications are automatically wired up.
func NewBackend(logger *zap.Logger, store Store, validator *auth.Validator, master MasterAuthConfig) *Backend {
	b := &Backend{
		logger:    logger,
		store:     store,
		validator: validator,
		master:    master,
		updatesCh: make(chan backend.Update, 64),
	}
	if upd, ok := store.(interface{ ProtocolUpdates() chan backend.Update }); ok {
		b.updatesCh = upd.ProtocolUpdates()
	} else if upd, ok := store.(Updater); ok {
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
				Items:    map[imap.StatusItem]interface{}{imap.StatusMessages: nil},
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
func (b *Backend) Login(conn *imap.ConnInfo, username, password string) (backend.User, error) {
	// Master (proxy) login: when enabled and the username carries the
	// separator, this is exclusively a master attempt — it succeeds as the
	// target user or fails closed. It never falls through to normal auth (a
	// "<user><sep><master>" string is not a real account).
	if b.master.enabled() && strings.Contains(username, b.master.separator()) {
		user, err := b.masterLogin(conn, username, password)
		if err != nil {
			b.logger.Warn("IMAP master authentication failed",
				zap.String("username", username),
				zap.Error(err))
			return nil, errors.New("authentication failed")
		}
		return user, nil
	}

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

// masterLogin validates a "<target><sep><master_user>" login. Every check must
// pass; on any failure the caller returns a generic authentication error.
func (b *Backend) masterLogin(conn *imap.ConnInfo, username, password string) (backend.User, error) {
	sep := b.master.separator()
	idx := strings.LastIndex(username, sep)
	if idx < 0 {
		return nil, errors.New("malformed master login")
	}
	target := username[:idx]
	masterName := username[idx+len(sep):]
	if target == "" || masterName == "" {
		return nil, errors.New("malformed master login")
	}

	// Source IP must be on the allowlist (fail closed on missing conn info).
	if conn == nil || conn.RemoteAddr == nil {
		return nil, errors.New("master login without connection info")
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr.String())
	if err != nil {
		host = conn.RemoteAddr.String()
	}
	allowed := false
	for _, ip := range b.master.AllowedIPs {
		if ip == host {
			allowed = true
		}
	}
	if !allowed {
		return nil, errors.New("master login from non-allowlisted address")
	}

	// Constant-time credential check; evaluate both comparisons unconditionally.
	nameOK := subtle.ConstantTimeCompare([]byte(masterName), []byte(b.master.User))
	passOK := subtle.ConstantTimeCompare([]byte(password), []byte(b.master.Password))
	if nameOK&passOK != 1 {
		return nil, errors.New("bad master credentials")
	}

	// Target account must exist and be enabled.
	user, ok := b.validator.GetUserStore().GetUser(target)
	if !ok || user == nil || !user.Enabled {
		return nil, errors.New("master login target unavailable")
	}

	b.logger.Info("IMAP master authentication successful",
		zap.String("target", target),
		zap.String("remote", host))
	return NewUser(b.logger, b.store, target), nil
}
