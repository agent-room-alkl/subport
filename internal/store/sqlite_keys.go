package store

import (
	"database/sql"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

func scanSQLiteKey(rows interface{ Scan(...any) error }) (model.APIKey, error) {
	var k model.APIKey
	var enabled int
	var created string
	err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.SecretSHA, &enabled, &created, &k.LastUsed)
	if err != nil {
		return k, err
	}
	k.Enabled = enabled != 0
	k.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		k.CreatedAt, err = time.Parse(time.RFC3339, created)
	}
	return k, err
}

func scanSQLiteUsage(rows interface{ Scan(...any) error }) (model.UsageLog, error) {
	var l model.UsageLog
	var streamBroken, compensated int
	var created string
	err := rows.Scan(&l.ID, &l.UserID, &l.KeyID, &l.Model, &l.Tokens, &l.Cost, &l.Status, &l.AccountID, &l.Attempts, &streamBroken, &compensated, &created)
	if err != nil {
		return l, err
	}
	l.StreamBroken = streamBroken != 0
	l.Compensated = compensated != 0
	l.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		l.CreatedAt, err = time.Parse(time.RFC3339, created)
	}
	return l, err
}

func (s *Store) sqliteEnsureKeys(keys []model.APIKey) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM api_keys`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	for _, k := range keys {
		if err := s.sqliteInsertKey(k); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) sqliteEnsureUsage(logs []model.UsageLog) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_logs`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	for _, l := range logs {
		if _, err := s.db.Exec(
			`INSERT INTO usage_logs(id,user_id,key_id,model,tokens,cost,status,account_id,attempts,stream_broken,compensated,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			l.ID, l.UserID, l.KeyID, l.Model, l.Tokens, l.Cost, l.Status, l.AccountID, l.Attempts, boolInt(l.StreamBroken), boolInt(l.Compensated), l.CreatedAt.Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) sqliteInsertKey(k model.APIKey) error {
	_, err := s.db.Exec(
		`INSERT INTO api_keys(id,user_id,name,secret_hash,prefix,enabled,created_at,last_used_at) VALUES(?,?,?,?,?,?,?,?)`,
		k.ID, k.UserID, k.Name, k.SecretSHA, k.Prefix, boolInt(k.Enabled), k.CreatedAt.Format(time.RFC3339Nano), k.LastUsed,
	)
	return err
}

func (s *Store) sqliteKeysOf(userID string) ([]model.APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id,user_id,name,prefix,secret_hash,enabled,created_at,last_used_at FROM api_keys WHERE user_id=? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.APIKey{}
	for rows.Next() {
		k, err := scanSQLiteKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) sqliteKeyBySecretHash(sum string) (model.APIKey, error) {
	return scanSQLiteKey(s.db.QueryRow(
		`SELECT id,user_id,name,prefix,secret_hash,enabled,created_at,last_used_at FROM api_keys WHERE secret_hash=?`,
		sum,
	))
}

func (s *Store) sqliteTouchKey(keyID, when string) error {
	_, err := s.db.Exec(`UPDATE api_keys SET last_used_at=? WHERE id=?`, when, keyID)
	return err
}

func (s *Store) sqliteKeyByID(userID, keyID string) (model.APIKey, error) {
	k, err := scanSQLiteKey(s.db.QueryRow(
		`SELECT id,user_id,name,prefix,secret_hash,enabled,created_at,last_used_at FROM api_keys WHERE id=? AND user_id=?`,
		keyID, userID,
	))
	if err == sql.ErrNoRows {
		return model.APIKey{}, ErrNotFound
	}
	if err != nil {
		return model.APIKey{}, err
	}
	return k, nil
}

func (s *Store) sqliteSetKeyEnabled(userID, keyID string, enabled bool) error {
	res, err := s.db.Exec(`UPDATE api_keys SET enabled=? WHERE id=? AND user_id=?`, boolInt(enabled), keyID, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) sqliteDeleteKey(userID, keyID string) error {
	res, err := s.db.Exec(`DELETE FROM api_keys WHERE id=? AND user_id=?`, keyID, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) sqliteUsageOf(userID string, limit int) ([]model.UsageLog, error) {
	rows, err := s.db.Query(
		`SELECT id,user_id,key_id,model,tokens,cost,status,account_id,attempts,stream_broken,compensated,created_at FROM usage_logs WHERE user_id=? ORDER BY created_at DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.UsageLog{}
	for rows.Next() {
		l, err := scanSQLiteUsage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) sqliteAddUsage(l model.UsageLog) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`INSERT INTO usage_logs(id,user_id,key_id,model,tokens,cost,status,account_id,attempts,stream_broken,compensated,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.ID, l.UserID, l.KeyID, l.Model, l.Tokens, l.Cost, l.Status, l.AccountID, l.Attempts, boolInt(l.StreamBroken), boolInt(l.Compensated), l.CreatedAt.Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET quota_used = quota_used + ? WHERE id=?`, l.Cost, l.UserID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) sqliteMarkUsageCompensated(ids []string) error {
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE usage_logs SET compensated=1 WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) sqliteCreditQuota(userID string, credit int64) error {
	_, err := s.db.Exec(`UPDATE users SET quota_used = CASE WHEN quota_used > ? THEN quota_used - ? ELSE 0 END WHERE id=?`, credit, credit, userID)
	return err
}

func (s *Store) sqliteCompensations() (pending, completed []model.UsageLog, err error) {
	rows, err := s.db.Query(
		`SELECT id,user_id,key_id,model,tokens,cost,status,account_id,attempts,stream_broken,compensated,created_at FROM usage_logs WHERE stream_broken=1 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		l, err := scanSQLiteUsage(rows)
		if err != nil {
			return nil, nil, err
		}
		if l.Compensated {
			completed = append(completed, l)
		} else {
			pending = append(pending, l)
		}
	}
	return pending, completed, rows.Err()
}

var _ interface{ Scan(...any) error } = (*sql.Row)(nil)