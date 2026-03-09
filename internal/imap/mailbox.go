package imap

import (
	"context"
	"io"
	"time"

	"github.com/emersion/go-imap"
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

	status := &imap.MailboxStatus{
		Name:        m.name,
		Messages:    total,
		Recent:      recent,
		Unseen:      unseen,
		UidNext:     total + 1,
		UidValidity: uint32(time.Now().Unix()),
	}

	return status, nil
}

// SetSubscribed sets the subscription status
func (m *Mailbox) SetSubscribed(subscribed bool) error {
	m.logger.Info("Subscription changed",
		zap.String("mailbox", m.name),
		zap.Bool("subscribed", subscribed))
	return nil
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
		// Create IMAP message
		imapMsg := imap.NewMessage(uint32(seqNum+1), items)
		imapMsg.Uid = uint32(seqNum + 1) // In production, use proper UID tracking

		// Populate requested items
		for _, item := range items {
			switch item {
			case imap.FetchEnvelope:
				// Would parse envelope from message
				imapMsg.Envelope = &imap.Envelope{
					Subject: "Message " + msg.ID,
				}
			case imap.FetchBody, imap.FetchBodyStructure:
				// Would parse body structure
			case imap.FetchFlags:
				for _, flag := range msg.Flags {
					imapMsg.Flags = append(imapMsg.Flags, imap.CanonicalFlag(flag))
				}
			case imap.FetchInternalDate:
				imapMsg.InternalDate = time.Now()
			case imap.FetchRFC822Size:
				imapMsg.Size = uint32(msg.Size)
			case imap.FetchUid:
				imapMsg.Uid = uint32(seqNum + 1)
			}
		}

		ch <- imapMsg
	}

	return nil
}

// SearchMessages searches for messages matching criteria
// RFC 3501 Section 6.4.4 - SEARCH Command
func (m *Mailbox) SearchMessages(uid bool, criteria *imap.SearchCriteria) ([]uint32, error) {
	ctx := context.Background()
	messages, err := m.store.GetMessages(ctx, m.username, m.name)
	if err != nil {
		return nil, err
	}

	// In production, would filter based on criteria
	// For now, return all message IDs
	var ids []uint32
	for i := range messages {
		ids = append(ids, uint32(i+1))
	}

	return ids, nil
}

// CreateMessage creates a new message
// RFC 3501 Section 6.3.11 - APPEND Command
func (m *Mailbox) CreateMessage(flags []string, date time.Time, body imap.Literal) error {
	ctx := context.Background()

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

// UpdateMessagesFlags updates flags for messages
// RFC 3501 Section 6.4.6 - STORE Command
func (m *Mailbox) UpdateMessagesFlags(uid bool, seqSet *imap.SeqSet, op imap.FlagsOp, flags []string) error {
	m.logger.Info("Flags updated",
		zap.String("mailbox", m.name),
		zap.Strings("flags", flags))
	// In production, persist flag changes
	return nil
}

// CopyMessages copies messages to another mailbox
// RFC 3501 Section 6.4.7 - COPY Command
func (m *Mailbox) CopyMessages(uid bool, seqSet *imap.SeqSet, destName string) error {
	m.logger.Info("Messages copied",
		zap.String("from", m.name),
		zap.String("to", destName))
	// In production, implement actual copy
	return nil
}

// Expunge permanently removes messages flagged for deletion
// RFC 3501 Section 6.4.3 - EXPUNGE Command
func (m *Mailbox) Expunge() error {
	m.logger.Info("Expunge called", zap.String("mailbox", m.name))
	// In production, delete messages with \Deleted flag
	return nil
}
