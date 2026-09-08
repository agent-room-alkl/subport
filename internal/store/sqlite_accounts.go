package store

import (
	"database/sql"

	"github.com/agent-room-alkl/subport/internal/model"
)

func scanSQLiteAccount(rows interface{ Scan(...any) error }) (model.Account, error) {
	var a model.Account
	var healthy int
	err := rows.Scan(&a.ID, &a.Name, &a.Provider, &a.BaseURL, &a.Priority, &healthy, &a.Load, &a.CooldownUntil, &a.LastError, &a.ConsecutiveTimeouts, &a.Consecutive403)
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
		if _, err := s.db.Exec(`INSERT INTO accounts(id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.Name, a.Provider, a.BaseURL, a.Priority, boolInt(a.Healthy), a.Load, a.CooldownUntil, a.LastError, a.ConsecutiveTimeouts, a.Consecutive403); err != nil {
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
	rows, err := s.db.Query(`SELECT id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403 FROM accounts ORDER BY priority,id`)
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
