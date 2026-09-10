package store

import (
	"database/sql"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

func (s *Store) sqliteGetCredential(accountID string) (model.AccountCredential, error) {
	var c model.AccountCredential
	err := s.db.QueryRow(`
SELECT account_id, access_token, refresh_token, extra_json, expires_at, updated_at
FROM account_credentials WHERE account_id=?`, accountID).Scan(
		&c.AccountID, &c.AccessToken, &c.RefreshToken, &c.ExtraJSON, &c.ExpiresAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return model.AccountCredential{AccountID: accountID}, ErrNotFound
	}
	return c, err
}

// sqliteUpsertCredential merges fields. Empty access/refresh leave the prior
// value unchanged; use clearAccess/clearRefresh to wipe explicitly.
func (s *Store) sqliteUpsertCredential(accountID, access, refresh, extraJSON, expiresAt string, setAccess, setRefresh, setExtra, setExpires bool) error {
	cur, err := s.sqliteGetCredential(accountID)
	if err != nil && err != ErrNotFound {
		return err
	}
	if err == ErrNotFound {
		cur = model.AccountCredential{AccountID: accountID}
	}
	if setAccess {
		cur.AccessToken = access
	}
	if setRefresh {
		cur.RefreshToken = refresh
	}
	if setExtra {
		cur.ExtraJSON = extraJSON
	}
	if setExpires {
		cur.ExpiresAt = expiresAt
	}
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(`
INSERT INTO account_credentials(account_id, access_token, refresh_token, extra_json, expires_at, updated_at)
VALUES(?,?,?,?,?,?)
ON CONFLICT(account_id) DO UPDATE SET
  access_token=excluded.access_token,
  refresh_token=excluded.refresh_token,
  extra_json=excluded.extra_json,
  expires_at=excluded.expires_at,
  updated_at=excluded.updated_at
`, cur.AccountID, cur.AccessToken, cur.RefreshToken, cur.ExtraJSON, cur.ExpiresAt, cur.UpdatedAt)
	return err
}

func (s *Store) sqliteDeleteCredential(accountID string) error {
	res, err := s.db.Exec(`DELETE FROM account_credentials WHERE account_id=?`, accountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) sqliteListCredentials() ([]model.AccountCredential, error) {
	rows, err := s.db.Query(`SELECT account_id, access_token, refresh_token, extra_json, expires_at, updated_at FROM account_credentials`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AccountCredential
	for rows.Next() {
		var c model.AccountCredential
		if err := rows.Scan(&c.AccountID, &c.AccessToken, &c.RefreshToken, &c.ExtraJSON, &c.ExpiresAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
