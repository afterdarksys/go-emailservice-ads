package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

func (s *MailboxStore) SaveSnapshot(ctx context.Context, user, kind, signature, token string, ids []string) error {
	if len(ids) > 10000 {
		return mailstate.ErrCannotCalculate
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO jmap_snapshots(username,kind,signature,token,ids) VALUES(?,?,?,?,?)`, user, kind, signature, token, string(data)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM jmap_snapshots WHERE username=? AND kind=? AND seq NOT IN (SELECT seq FROM jmap_snapshots WHERE username=? AND kind=? ORDER BY seq DESC LIMIT 64)`, user, kind, user, kind); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *MailboxStore) LoadSnapshot(ctx context.Context, user, kind, signature, token string) ([]string, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT ids FROM jmap_snapshots WHERE username=? AND kind=? AND signature=? AND token=?`, user, kind, signature, token).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, mailstate.ErrCannotCalculate
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	err = json.Unmarshal([]byte(data), &ids)
	return ids, err
}
