package store

import (
	"database/sql"

	"github.com/agent-room-alkl/subport/internal/model"
)

func scanSQLiteAccount(rows interface{ Scan(...any) error }) (model.Account, error) {
	var a model.Account
	var healthy int
	err := rows.Scan(&a.ID, &a.Name, &a.Provider, &a.BaseURL, &a.Priority, &healthy, &a.Load, &a.CooldownUntil, &a.LastError, &a.ConsecutiveTimeouts, &a.Consecutive403, &a.Consecutive429, &a.ProxyID)
	a.Healthy = healthy != 0
	return a, err
}

func (s *Store) sqliteEnsureAccounts(accounts []model.Account) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	for _, a := range accounts {
		if _, err := s.db.Exec(`INSERT INTO accounts(id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403,consecutive_429,proxy_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.Name, a.Provider, a.BaseURL, a.Priority, boolInt(a.Healthy), a.Load, a.CooldownUntil, a.LastError, a.ConsecutiveTimeouts, a.Consecutive403, a.Consecutive429, a.ProxyID); err != nil {
			return err
		}
	}
	return nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Store) sqliteAccounts() ([]model.Account, error) {
	rows, err := s.db.Query(`SELECT id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403,COALESCE(consecutive_429,0),COALESCE(proxy_id,'') FROM accounts ORDER BY priority,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Account
	for rows.Next() {
		a, err := scanSQLiteAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) sqliteAccountByID(id string) (model.Account, error) {
	return scanSQLiteAccount(s.db.QueryRow(`SELECT id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403,COALESCE(consecutive_429,0),COALESCE(proxy_id,'') FROM accounts WHERE id=?`, id))
}

func (s *Store) sqliteSetAccountHealthy(id string, healthy bool) error {
	res, err := s.db.Exec(`UPDATE accounts SET healthy=? WHERE id=?`, boolInt(healthy), id)
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

var _ interface{ Scan(...any) error } = (*sql.Rows)(nil)

func (s *Store) sqliteUpsertAccount(a model.Account) error {
	_, err := s.db.Exec(`
INSERT INTO accounts(id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403,consecutive_429,proxy_id)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  provider=excluded.provider,
  base_url=excluded.base_url,
  priority=excluded.priority,
  healthy=excluded.healthy,
  proxy_id=excluded.proxy_id
`, a.ID, a.Name, a.Provider, a.BaseURL, a.Priority, boolInt(a.Healthy), a.Load, a.CooldownUntil, a.LastError, a.ConsecutiveTimeouts, a.Consecutive403, a.Consecutive429, a.ProxyID)
	return err
}

func (s *Store) sqliteApplyAccountHealth(id string, healthy bool, cooldownUntil, lastError string, timeouts, c403, c429 int) error {
	res, err := s.db.Exec(`UPDATE accounts SET healthy=?, cooldown_until=?, last_error=?, consecutive_timeouts=?, consecutive_403=?, consecutive_429=? WHERE id=?`,
		boolInt(healthy), cooldownUntil, lastError, timeouts, c403, c429, id)
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
