package storage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"go.uber.org/zap"
)

const maxUploadCount = 20
const maxUploadAccountBytes = 100 * 1024 * 1024

func (s *MailboxStore) UploadBlob(ctx context.Context, user, media string, data []byte) (mailstate.Blob, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	out := mailstate.Blob{}
	if user == "" || len(data) > mailstate.MaxUploadBytes {
		return out, mailstate.ErrUploadQuota
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM jmap_uploads WHERE expires<=?`, time.Now().Unix()); err != nil {
		return out, err
	}
	var count, used int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(length(data)),0) FROM jmap_uploads WHERE username=?`, user).Scan(&count, &used); err != nil {
		return out, err
	}
	// Uploads are unreferenced: evict this owner's oldest blobs to make room.
	for count >= maxUploadCount || used+int64(len(data)) > maxUploadAccountBytes {
		var oldest string
		var size int64
		if err = tx.QueryRowContext(ctx, `SELECT id,length(data) FROM jmap_uploads WHERE username=? ORDER BY expires,rowid LIMIT 1`, user).Scan(&oldest, &size); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM jmap_uploads WHERE id=? AND username=?`, oldest, user); err != nil {
			return out, err
		}
		count--
		used -= size
	}
	if data == nil {
		data = []byte{}
	}
	out.ID = "upload-" + generateMessageID()
	out.MediaType = media
	if _, err = tx.ExecContext(ctx, `INSERT INTO jmap_uploads(id,username,media_type,data,expires) VALUES(?,?,?,?,?)`, out.ID, user, media, data, time.Now().Add(24*time.Hour).Unix()); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// getBlob checks ownership in SQLite before touching payload storage.
func (s *MailboxStore) getBlob(ctx context.Context, q syncQuery, user, id string) (mailstate.Blob, error) {
	out := mailstate.Blob{ID: id}
	if strings.HasPrefix(id, "upload-") {
		err := q.QueryRowContext(ctx, `SELECT media_type,data FROM jmap_uploads WHERE id=? AND username=? AND expires>?`, id, user, time.Now().Unix()).Scan(&out.MediaType, &out.Data)
		if errors.Is(err, sql.ErrNoRows) {
			err = mailstate.ErrBlobNotFound
		}
		return out, err
	}
	messageID, number, err := mailstate.SplitPartBlob(id)
	if err != nil {
		return out, err
	}
	var exists int
	err = q.QueryRowContext(ctx, `SELECT 1 FROM message_flags WHERE username=? AND msg_id=? AND expunged=0`, user, messageID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return out, mailstate.ErrBlobNotFound
	}
	if err != nil {
		return out, err
	}
	out.MediaType = "message/rfc822"
	out.Data, err = s.adapter.FetchMessage(ctx, messageID)
	if err == nil && number > 0 {
		out.Data, out.MediaType, err = mailstate.MIMEBlob(out.Data, number)
	}
	return out, err
}
func (s *MailboxStore) GetBlob(ctx context.Context, user, id string) (mailstate.Blob, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	return s.getBlob(ctx, s.db, user, id)
}
func (s *MailboxStore) importEmail(ctx context.Context, tx *sql.Tx, user string, p mailstate.EmailImport) (mailstate.ImportedEmail, string, string, error) {
	out := mailstate.ImportedEmail{}
	blob, err := s.getBlob(ctx, tx, user, p.BlobID)
	if errors.Is(err, mailstate.ErrBlobNotFound) {
		return out, "", "invalidProperties", nil
	}
	if err != nil {
		return out, "", "", err
	}
	return s.importData(ctx, tx, user, p, blob.Data)
}
func (s *MailboxStore) importData(ctx context.Context, tx *sql.Tx, user string, p mailstate.EmailImport, data []byte) (mailstate.ImportedEmail, string, string, error) {
	out := mailstate.ImportedEmail{}
	if len(data) > mailstate.MaxUploadBytes {
		return out, "", "tooLarge", nil
	}
	message, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil || len(message.Header) == 0 || bytes.IndexByte(data, 0) >= 0 {
		return out, "", "invalidEmail", nil
	}
	date := p.ReceivedAt
	if date.IsZero() {
		date = time.Now()
		if received := message.Header.Get("Received"); received != "" {
			if i := strings.LastIndex(received, ";"); i >= 0 {
				if parsed, e := mail.ParseDate(strings.TrimSpace(received[i+1:])); e == nil {
					date = parsed
				}
			}
		}
	}
	flags, err := validFlags(p.Flags)
	if err != nil {
		return out, "", "invalidProperties", nil
	}
	var folder string
	err = tx.QueryRowContext(ctx, `SELECT mailbox FROM mailbox_catalog WHERE username=? AND jmap_id=?`, user, p.MailboxID).Scan(&folder)
	if errors.Is(err, sql.ErrNoRows) {
		return out, "", "invalidProperties", nil
	}
	if err != nil {
		return out, "", "", err
	}
	var used int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0) FROM message_flags WHERE username=? AND expunged=0`, user).Scan(&used); err != nil {
		return out, "", "", err
	}
	if s.quotaBytes > 0 && used+int64(len(data)) > s.quotaBytes {
		return out, "", "overQuota", nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO mailbox_state(username,mailbox,uidvalidity,uidnext) VALUES(?,?,?,1)`, user, folder, time.Now().Unix()); err != nil {
		return out, "", "", err
	}
	var next uint64
	if err = tx.QueryRowContext(ctx, `SELECT uidnext FROM mailbox_state WHERE username=? AND mailbox=?`, user, folder).Scan(&next); err != nil {
		return out, "", "", err
	}
	if next >= uint64(^uint32(0)) {
		return out, "", "overQuota", nil
	}
	out.ID, err = s.adapter.StoreMessage(ctx, user, folder, data)
	if err != nil {
		return out, "", "", err
	}
	out.Size = len(data)
	sender, subject, _ := parseHeaders(data)
	_, err = tx.ExecContext(ctx, `INSERT INTO message_flags(msg_id,username,mailbox,uid,flags,sender,subject,size,sent_at,deleted) VALUES(?,?,?,?,?,?,?,?,?,?)`, out.ID, user, folder, next, strings.Join(flags, " "), sender, subject, out.Size, date.Unix(), boolInt(containsFlag(flags, `\Deleted`)))
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE mailbox_state SET uidnext=? WHERE username=? AND mailbox=?`, next+1, user, folder)
	}
	return out, folder, "", err
}

// ImportEmails serializes state/quota checks. Each object's savepoint includes
// metadata, UID allocation and both change journals. Orphan payloads are removed
// on failure and recovered after an interrupted commit at startup.
func (s *MailboxStore) ImportEmails(ctx context.Context, user, since string, emails map[string]mailstate.EmailImport) (mailstate.ImportResult, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	out := mailstate.ImportResult{Created: map[string]mailstate.ImportedEmail{}, NotCreated: map[string]string{}}
	if err := s.ensureUser(ctx, user); err != nil {
		return out, err
	}
	if err := s.ensureEmailSync(ctx, user); err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	prefix, revision, _, err := syncState(ctx, tx, user)
	if err != nil {
		return out, err
	}
	out.OldState = stateToken(prefix, revision)
	if since != "" && since != out.OldState {
		return out, mailstate.ErrStateMismatch
	}
	discard := func(id string) {
		if id != "" {
			if e := s.adapter.discardMessage(id); e != nil {
				s.adapter.store.logger.Warn("Deferred JMAP import payload cleanup", zap.Error(e))
			}
		}
	}
	committed := false
	payloads := []string{}
	defer func() {
		if !committed {
			for _, id := range payloads {
				discard(id)
			}
		}
	}()
	keys := []string{}
	for key := range emails {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	folders := map[string]bool{}
	for _, key := range keys {
		if _, err = tx.ExecContext(ctx, `SAVEPOINT import_email`); err != nil {
			return out, err
		}
		created, folder, kind, e := s.importEmail(ctx, tx, user, emails[key])
		if created.ID != "" {
			payloads = append(payloads, created.ID)
		}
		if e != nil {
			kind = "serverFail"
		}
		if kind != "" {
			if _, err = tx.ExecContext(ctx, `ROLLBACK TO import_email`); err != nil {
				return out, err
			}
			discard(created.ID)
			out.NotCreated[key] = kind
		} else {
			out.Created[key] = created
			folders[folder] = true
		}
		if _, err = tx.ExecContext(ctx, `RELEASE import_email`); err != nil {
			return out, err
		}
	}
	prefix, revision, _, err = syncState(ctx, tx, user)
	if err != nil {
		return out, err
	}
	out.NewState = stateToken(prefix, revision)
	if err = tx.Commit(); err != nil {
		return out, err
	}
	committed = true
	for folder := range folders {
		select {
		case s.deliveryCh <- [2]string{user, folder}:
		default:
		}
		s.notifyMailbox(user, folder)
	}
	return out, nil
}
