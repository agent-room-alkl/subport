package store

import (
	"database/sql"

	"github.com/agent-room-alkl/subport/internal/model"
)

func scanSQLiteAccount(rows interface{ Scan(...any) error }) (model.Account, error) {
	var a model.Account
	var healthy sqlBool
	err := rows.Scan(&a.ID, &a.Name, &a.Provider, &a.BaseURL, &a.Priority, &healthy, &a.Load, &a.CooldownUntil, &a.LastError, &a.ConsecutiveTimeouts, &a.Consecutive403, &a.Consecutive429)
	a.Healthy = healthy.Bool()
	return a, err
}

func (s *Store) sqliteEnsureAccounts(accounts []model.Account) error {
	var count int
	if err := s.queryRow(`SELECT COUNT(*) FROM accounts`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	for _, a := range accounts {
		if _, err := s.exec(`INSERT INTO accounts(id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403,consecutive_429) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.Name, a.Provider, a.BaseURL, a.Priority, s.boolArg(a.Healthy), a.Load, a.CooldownUntil, a.LastError, a.ConsecutiveTimeouts, a.Consecutive403, a.Consecutive429); err != nil {
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
	rows, err := s.query(`SELECT id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403,COALESCE(consecutive_429,0) FROM accounts ORDER BY priority,id`)
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
	return scanSQLiteAccount(s.queryRow(`SELECT id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403,COALESCE(consecutive_429,0) FROM accounts WHERE id=?`, id))
}

func (s *Store) sqliteSetAccountHealthy(id string, healthy bool) error {
	res, err := s.exec(`UPDATE accounts SET healthy=? WHERE id=?`, s.boolArg(healthy), id)
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
	_, err := s.exec(`
INSERT INTO accounts(id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403,consecutive_429)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  provider=excluded.provider,
  base_url=excluded.base_url,
  priority=excluded.priority,
  healthy=excluded.healthy
`, a.ID, a.Name, a.Provider, a.BaseURL, a.Priority, s.boolArg(a.Healthy), a.Load, a.CooldownUntil, a.LastError, a.ConsecutiveTimeouts, a.Consecutive403, a.Consecutive429)
	return err
}

func (s *Store) sqliteApplyAccountHealth(id string, healthy bool, cooldownUntil, lastError string, timeouts, c403, c429 int) error {
	res, err := s.exec(`UPDATE accounts SET healthy=?, cooldown_until=?, last_error=?, consecutive_timeouts=?, consecutive_403=?, consecutive_429=? WHERE id=?`,
		s.boolArg(healthy), cooldownUntil, lastError, timeouts, c403, c429, id)
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

func (s *Store) sqliteDeleteAccount(id string) error {
	res, err := s.exec(`DELETE FROM accounts WHERE id=?`, id)
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
