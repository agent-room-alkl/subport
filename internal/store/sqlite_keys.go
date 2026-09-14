package store

import (
	"database/sql"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

func scanSQLiteKey(rows interface{ Scan(...any) error }) (model.APIKey, error) {
	var k model.APIKey
	var enabled sqlBool
	var created sqlTime
	err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.SecretSHA, &enabled, &created, &k.LastUsed)
	if err != nil {
		return k, err
	}
	k.Enabled = enabled.Bool()
	k.CreatedAt = created.Time()
	return k, nil
}

func scanSQLiteUsage(rows interface{ Scan(...any) error }) (model.UsageLog, error) {
	var l model.UsageLog
	var streamBroken, compensated sqlBool
	var created sqlTime
	err := rows.Scan(&l.ID, &l.UserID, &l.KeyID, &l.Model, &l.Tokens, &l.Cost, &l.Status, &l.AccountID, &l.Attempts, &streamBroken, &compensated, &created,
		&l.TokenParts.Input, &l.TokenParts.Output, &l.TokenParts.CacheRead, &l.TokenParts.CacheWrite,
		&l.BilledAt.Input, &l.BilledAt.Output, &l.BilledAt.CacheRead, &l.BilledAt.CacheWrite, &l.BilledAt.Ratio)
	if err != nil {
		return l, err
	}
	l.TokenParts.Total = l.Tokens
	l.StreamBroken = streamBroken.Bool()
	l.Compensated = compensated.Bool()
	l.CreatedAt = created.Time()
	return l, nil
}

func (s *Store) sqliteEnsureKeys(keys []model.APIKey) error {
	var count int
	if err := s.queryRow(`SELECT COUNT(*) FROM api_keys`).Scan(&count); err != nil {
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
	if err := s.queryRow(`SELECT COUNT(*) FROM usage_logs`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	for _, l := range logs {
		if _, err := s.exec(
			`INSERT INTO usage_logs(id,user_id,key_id,model,tokens,cost,status,account_id,attempts,stream_broken,compensated,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			l.ID, l.UserID, l.KeyID, l.Model, l.Tokens, l.Cost, l.Status, l.AccountID, l.Attempts, s.boolArg(l.StreamBroken), s.boolArg(l.Compensated), l.CreatedAt.Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) sqliteInsertKey(k model.APIKey) error {
	_, err := s.exec(
		`INSERT INTO api_keys(id,user_id,name,secret_hash,prefix,enabled,created_at,last_used_at) VALUES(?,?,?,?,?,?,?,?)`,
		k.ID, k.UserID, k.Name, k.SecretSHA, k.Prefix, s.boolArg(k.Enabled), k.CreatedAt.Format(time.RFC3339Nano), k.LastUsed,
	)
	return err
}

func (s *Store) sqliteKeysOf(userID string) ([]model.APIKey, error) {
	rows, err := s.query(
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
	return scanSQLiteKey(s.queryRow(
		`SELECT id,user_id,name,prefix,secret_hash,enabled,created_at,last_used_at FROM api_keys WHERE secret_hash=?`,
		sum,
	))
}

func (s *Store) sqliteTouchKey(keyID, when string) error {
	_, err := s.exec(`UPDATE api_keys SET last_used_at=? WHERE id=?`, when, keyID)
	return err
}

func (s *Store) sqliteKeyByID(userID, keyID string) (model.APIKey, error) {
	k, err := scanSQLiteKey(s.queryRow(
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
	res, err := s.exec(`UPDATE api_keys SET enabled=? WHERE id=? AND user_id=?`, s.boolArg(enabled), keyID, userID)
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
	res, err := s.exec(`DELETE FROM api_keys WHERE id=? AND user_id=?`, keyID, userID)
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
	rows, err := s.query(
		`SELECT id,user_id,key_id,model,tokens,cost,status,account_id,attempts,stream_broken,compensated,created_at,tokens_input,tokens_output,tokens_cache_read,tokens_cache_write,rate_input,rate_output,rate_cache_read,rate_cache_write,rate_ratio FROM usage_logs WHERE user_id=? ORDER BY created_at DESC LIMIT ?`,
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

func (s *Store) sqliteMarkUsageCompensated(ids []string) error {
	for _, id := range ids {
		if _, err := s.exec(`UPDATE usage_logs SET compensated=1 WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) sqliteCreditQuota(userID string, credit int64) error {
	_, err := s.exec(`UPDATE users SET quota_used = CASE WHEN quota_used > ? THEN quota_used - ? ELSE 0 END WHERE id=?`, credit, credit, userID)
	return err
}

func (s *Store) sqliteCompensations() (pending, completed []model.UsageLog, err error) {
	rows, err := s.query(
		`SELECT id,user_id,key_id,model,tokens,cost,status,account_id,attempts,stream_broken,compensated,created_at,tokens_input,tokens_output,tokens_cache_read,tokens_cache_write,rate_input,rate_output,rate_cache_read,rate_cache_write,rate_ratio FROM usage_logs WHERE stream_broken=1 ORDER BY created_at DESC`,
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

func (s *Store) sqliteListAllKeys() ([]model.APIKey, error) {
	rows, err := s.query(
		`SELECT id,user_id,name,prefix,secret_hash,enabled,created_at,last_used_at FROM api_keys ORDER BY created_at`,
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

func (s *Store) sqliteUsageRecent(limit int) ([]model.UsageLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.query(
		`SELECT id,user_id,key_id,model,tokens,cost,status,account_id,attempts,stream_broken,compensated,created_at,tokens_input,tokens_output,tokens_cache_read,tokens_cache_write,rate_input,rate_output,rate_cache_read,rate_cache_write,rate_ratio FROM usage_logs ORDER BY created_at DESC LIMIT ?`,
		limit,
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

func (s *Store) sqliteCountUsageSince(sinceRFC3339 string) (int, error) {
	var n int
	err := s.queryRow(`SELECT COUNT(*) FROM usage_logs WHERE created_at >= ?`, sinceRFC3339).Scan(&n)
	return n, err
}

func (s *Store) sqliteKeyByIDOnly(keyID string) (model.APIKey, error) {
	k, err := scanSQLiteKey(s.queryRow(
		`SELECT id,user_id,name,prefix,secret_hash,enabled,created_at,last_used_at FROM api_keys WHERE id=?`,
		keyID,
	))
	if err == sql.ErrNoRows {
		return model.APIKey{}, ErrNotFound
	}
	if err != nil {
		return model.APIKey{}, err
	}
	return k, nil
}

func (s *Store) sqliteAdminSetKeyEnabled(keyID string, enabled bool) error {
	res, err := s.exec(`UPDATE api_keys SET enabled=? WHERE id=?`, s.boolArg(enabled), keyID)
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

func (s *Store) sqliteAdminDeleteKey(keyID string) error {
	res, err := s.exec(`DELETE FROM api_keys WHERE id=?`, keyID)
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

// UsageDayStat is one calendar day of aggregated usage (UTC date string).
type UsageDayStat struct {
	Day      string `json:"day"`
	Requests int    `json:"requests"`
	Tokens   int64  `json:"tokens"`
	Cost     int64  `json:"cost"`
}

// UsageModelStat is per-model usage totals for charts.
type UsageModelStat struct {
	Model    string `json:"model"`
	Tokens   int64  `json:"tokens"`
	Requests int    `json:"requests"`
}

func (s *Store) sqliteUsageByDay(days int) ([]UsageDayStat, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := s.query(`
		SELECT substr(created_at, 1, 10) AS day,
		       COUNT(*) AS requests,
		       COALESCE(SUM(tokens), 0) AS tokens,
		       COALESCE(SUM(cost), 0) AS cost
		FROM usage_logs
		WHERE created_at >= ?
		GROUP BY substr(created_at, 1, 10)
		ORDER BY day ASC
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[string]UsageDayStat{}
	for rows.Next() {
		var st UsageDayStat
		if err := rows.Scan(&st.Day, &st.Requests, &st.Tokens, &st.Cost); err != nil {
			return nil, err
		}
		byDay[st.Day] = st
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]UsageDayStat, 0, days)
	now := time.Now().UTC()
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		if st, ok := byDay[d]; ok {
			out = append(out, st)
		} else {
			out = append(out, UsageDayStat{Day: d})
		}
	}
	return out, nil
}

func (s *Store) sqliteUsageByModel(limit int) ([]UsageModelStat, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.query(`
		SELECT model,
		       COALESCE(SUM(tokens), 0) AS tokens,
		       COUNT(*) AS requests
		FROM usage_logs
		GROUP BY model
		ORDER BY tokens DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageModelStat{}
	for rows.Next() {
		var st UsageModelStat
		if err := rows.Scan(&st.Model, &st.Tokens, &st.Requests); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) sqliteCountUsersByRole(role string) (int, error) {
	var n int
	var err error
	if role == "" {
		err = s.queryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	} else {
		err = s.queryRow(`SELECT COUNT(*) FROM users WHERE role=?`, role).Scan(&n)
	}
	return n, err
}

func (s *Store) sqliteSetRole(userID, role string) error {
	res, err := s.exec(`UPDATE users SET role=? WHERE id=?`, role, userID)
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
