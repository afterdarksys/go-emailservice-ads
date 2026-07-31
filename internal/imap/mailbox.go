package imap

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend/backendutil"
	"github.com/emersion/go-message/textproto"
	"go.uber.org/zap"
)

// Mailbox implements the go-imap backend.Mailbox interface
// RFC 3501 - IMAP4rev1 mailbox implementation
type Mailbox struct {
	logger   *zap.Logger
	store    Store
	username string
	name     string
}

// NewMailbox creates a new mailbox instance
func NewMailbox(logger *zap.Logger, store Store, username, name string) *Mailbox {
	return &Mailbox{
		logger:   logger,
		store:    store,
		username: username,
		name:     name,
	}
}

// Name returns the mailbox name
func (m *Mailbox) Name() string {
	return m.name
}

// Info returns mailbox information
// RFC 3501 Section 7.2.2 - SELECT and EXAMINE
func (m *Mailbox) Info() (*imap.MailboxInfo, error) {
	ctx := context.Background()

	// Get messages from store
	messages, err := m.store.GetMessages(ctx, m.username, m.name)
	if err != nil {
		m.logger.Error("Failed to get mailbox info",
			zap.String("user", m.username),
			zap.String("mailbox", m.name),
			zap.Error(err))
		return nil, err
	}

	// Count messages and unseen
	_ = uint32(len(messages)) // total - not used currently
	var unseen uint32 = 0
	for _, msg := range messages {
		hasSeen := false
		for _, flag := range msg.Flags {
			if flag == "\\Seen" {
				hasSeen = true
				break
			}
		}
		if !hasSeen {
			unseen++
		}
	}

	info := &imap.MailboxInfo{
		Attributes: []string{},
		Delimiter:  "/",
		Name:       m.name,
	}

	return info, nil
}

// Status returns mailbox status
// RFC 3501 Section 6.3.10 - STATUS Command
func (m *Mailbox) Status(items []imap.StatusItem) (*imap.MailboxStatus, error) {
	ctx := context.Background()

	messages, err := m.store.GetMessages(ctx, m.username, m.name)
	if err != nil {
		return nil, err
	}

	total := uint32(len(messages))
	var unseen, recent uint32

	for _, msg := range messages {
		hasSeen := false
		for _, flag := range msg.Flags {
			if flag == "\\Seen" {
				hasSeen = true
			}
		}
		if !hasSeen {
			unseen++
		}
	}

	uidValidity, err := m.store.GetUIDValidity(ctx, m.username, m.name)
	if err != nil {
		return nil, err
	}
	uidNext, err := m.store.GetUIDNext(ctx, m.username, m.name)
	if err != nil {
		return nil, err
	}

	status := &imap.MailboxStatus{
		Name:        m.name,
		Messages:    total,
		Recent:      recent,
		Unseen:      unseen,
		UidNext:     uidNext,
		UidValidity: uidValidity,
	}

	return status, nil
}

// SetSubscribed sets the subscription status
func (m *Mailbox) SetSubscribed(subscribed bool) error {
	return errMailboxMutationUnsupported
}

// Check requests a checkpoint of the mailbox
func (m *Mailbox) Check() error {
	return nil
}

// ListMessages returns a list of messages
// RFC 3501 Section 6.4.5 - FETCH Command
func (m *Mailbox) ListMessages(uid bool, seqSet *imap.SeqSet, items []imap.FetchItem, ch chan<- *imap.Message) error {
	defer close(ch)

	ctx := context.Background()
	messages, err := m.store.GetMessages(ctx, m.username, m.name)
	if err != nil {
		return err
	}

	// For each message in the sequence set
	for seqNum, msg := range messages {
		sequenceNumber := uint32(seqNum + 1)
		requestedID := sequenceNumber
		if uid {
			requestedID = msg.UID
		}
		if seqSet != nil && !seqSet.Contains(requestedID) {
			continue
		}

		// Create IMAP message
		imapMsg := imap.NewMessage(sequenceNumber, items)
		imapMsg.Uid = msg.UID
		var raw []byte
		loadRaw := func() ([]byte, error) {
			if raw != nil {
				return raw, nil
			}
			var fetchErr error
			raw, fetchErr = m.store.FetchMessage(ctx, msg.ID)
			return raw, fetchErr
		}

		// Populate requested items
		for _, item := range items {
			switch item {
			case imap.FetchEnvelope:
				data, err := loadRaw()
				if err != nil {
					return err
				}
				header, _, err := imapHeaderAndBody(data)
				if err != nil {
					return err
				}
				imapMsg.Envelope, err = backendutil.FetchEnvelope(header)
				if err != nil {
					return err
				}
			case imap.FetchBody, imap.FetchBodyStructure:
				data, err := loadRaw()
				if err != nil {
					return err
				}
				header, body, err := imapHeaderAndBody(data)
				if err != nil {
					return err
				}
				imapMsg.BodyStructure, err = backendutil.FetchBodyStructure(header, body, item == imap.FetchBodyStructure)
				if err != nil {
					return err
				}
			case imap.FetchFlags:
				for _, flag := range msg.Flags {
					imapMsg.Flags = append(imapMsg.Flags, imap.CanonicalFlag(flag))
				}
			case imap.FetchInternalDate:
				imapMsg.InternalDate = msg.Date
				if imapMsg.InternalDate.IsZero() {
					imapMsg.InternalDate = time.Now()
				}
			case imap.FetchRFC822Size:
				imapMsg.Size = uint32(msg.Size)
			case imap.FetchUid:
				imapMsg.Uid = msg.UID
			default:
				section, err := imap.ParseBodySectionName(item)
				if err != nil {
					continue
				}
				data, err := loadRaw()
				if err != nil {
					return err
				}
				header, body, err := imapHeaderAndBody(data)
				if err != nil {
					return err
				}
				literal, err := backendutil.FetchBodySection(header, body, section)
				if err != nil {
					return err
				}
				imapMsg.Body[section] = literal
			}
		}

		ch <- imapMsg
	}

	return nil
}

func imapHeaderAndBody(data []byte) (textproto.Header, io.Reader, error) {
	reader := bufio.NewReader(bytes.NewReader(data))
	header, err := textproto.ReadHeader(reader)
	return header, reader, err
}

// SearchMessages searches for messages matching criteria.
// RFC 3501 Section 6.4.4 - SEARCH Command
func (m *Mailbox) SearchMessages(uid bool, criteria *imap.SearchCriteria) ([]uint32, error) {
	ctx := context.Background()
	messages, err := m.store.GetMessages(ctx, m.username, m.name)
	if err != nil {
		return nil, err
	}

	var ids []uint32
	for seqNum, msg := range messages {
		if searchMatcher(uint32(seqNum+1), msg, criteria) {
			if uid {
				ids = append(ids, msg.UID)
			} else {
				ids = append(ids, uint32(seqNum+1))
			}
		}
	}
	return ids, nil
}

// searchMatcher returns true if msg satisfies all conditions in criteria.
func searchMatcher(seqNum uint32, msg MessageSummary, c *imap.SearchCriteria) bool {
	if c == nil {
		return true
	}

	// Sequence number set
	if c.SeqNum != nil && !c.SeqNum.Contains(seqNum) {
		return false
	}
	// UID set
	if c.Uid != nil && !c.Uid.Contains(msg.UID) {
		return false
	}

	// Date filters (against internal date)
	if !c.Since.IsZero() && !msg.Date.IsZero() && msg.Date.Before(c.Since) {
		return false
	}
	if !c.Before.IsZero() && !msg.Date.IsZero() && !msg.Date.Before(c.Before) {
		return false
	}
	if !c.SentSince.IsZero() && !msg.Date.IsZero() && msg.Date.Before(c.SentSince) {
		return false
	}
	if !c.SentBefore.IsZero() && !msg.Date.IsZero() && !msg.Date.Before(c.SentBefore) {
		return false
	}

	// Size filters
	if c.Larger > 0 && uint32(msg.Size) <= c.Larger {
		return false
	}
	if c.Smaller > 0 && uint32(msg.Size) >= c.Smaller {
		return false
	}

	// Flag filters
	flagSet := make(map[string]bool, len(msg.Flags))
	for _, f := range msg.Flags {
		flagSet[imap.CanonicalFlag(f)] = true
	}
	for _, f := range c.WithFlags {
		if !flagSet[imap.CanonicalFlag(f)] {
			return false
		}
	}
	for _, f := range c.WithoutFlags {
		if flagSet[imap.CanonicalFlag(f)] {
			return false
		}
	}

	// Header filters (Subject and From are stored; others unsupported → skip)
	for key, vals := range c.Header {
		var haystack string
		switch strings.ToLower(key) {
		case "subject":
			haystack = strings.ToLower(msg.Subject)
		case "from":
			haystack = strings.ToLower(msg.From)
		default:
			continue
		}
		for _, v := range vals {
			if !strings.Contains(haystack, strings.ToLower(v)) {
				return false
			}
		}
	}

	// NOT sub-criteria
	for _, sub := range c.Not {
		if searchMatcher(seqNum, msg, sub) {
			return false
		}
	}

	// OR sub-criteria (at least one pair must have a match)
	for _, pair := range c.Or {
		if !searchMatcher(seqNum, msg, pair[0]) && !searchMatcher(seqNum, msg, pair[1]) {
			return false
		}
	}

	return true
}

// CreateMessage creates a new message
// RFC 3501 Section 6.3.11 - APPEND Command
func (m *Mailbox) CreateMessage(flags []string, date time.Time, body imap.Literal) error {
	ctx := context.Background()
	if len(flags) > 0 || !date.IsZero() {
		return errMailboxMutationUnsupported
	}

	// Read message body
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	// Store message
	msgID, err := m.store.StoreMessage(ctx, m.username, m.name, data)
	if err != nil {
		m.logger.Error("Failed to store message",
			zap.String("user", m.username),
			zap.String("mailbox", m.name),
			zap.Error(err))
		return err
	}

	m.logger.Info("Message appended to mailbox",
		zap.String("user", m.username),
		zap.String("mailbox", m.name),
		zap.String("message_id", msgID),
		zap.Int("size", len(data)))

	return nil
}

// UpdateMessagesFlags updates flags for messages.
// RFC 3501 Section 6.4.6 - STORE Command
func (m *Mailbox) UpdateMessagesFlags(uid bool, seqSet *imap.SeqSet, op imap.FlagsOp, flags []string) error {
	ctx := context.Background()
	messages, err := m.store.GetMessages(ctx, m.username, m.name)
	if err != nil {
		return err
	}

	for seqNum, msg := range messages {
		var matched bool
		if uid {
			matched = seqSet.Contains(msg.UID)
		} else {
			matched = seqSet.Contains(uint32(seqNum + 1))
		}
		if !matched {
			continue
		}
		if err := m.store.UpdateMessageFlags(ctx, msg.ID, m.username, m.name, op, flags); err != nil {
			m.logger.Warn("Flag update failed",
				zap.String("msg_id", msg.ID),
				zap.Error(err))
		}
	}
	return nil
}

// CopyMessages copies messages to another mailbox
// RFC 3501 Section 6.4.7 - COPY Command
func (m *Mailbox) CopyMessages(uid bool, seqSet *imap.SeqSet, destName string) error {
	return errMailboxMutationUnsupported
}

// Expunge permanently removes messages flagged for deletion.
// RFC 3501 Section 6.4.3 - EXPUNGE Command
func (m *Mailbox) Expunge() error {
	ctx := context.Background()
	expunged, err := m.store.ExpungeDeleted(ctx, m.username, m.name)
	if err != nil {
		m.logger.Error("Expunge failed", zap.String("mailbox", m.name), zap.Error(err))
		return err
	}
	if len(expunged) > 0 {
		m.logger.Info("Expunge complete",
			zap.String("mailbox", m.name),
			zap.Int("count", len(expunged)))
	}
	return nil
}
