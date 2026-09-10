package store

import (
	"database/sql"

	"github.com/agent-room-alkl/subport/internal/model"
)

func (s *Store) sqliteListModelRoutes() ([]model.ModelRoute, error) {
	rows, err := s.db.Query(`SELECT id,pattern,provider,priority,enabled FROM model_routes ORDER BY priority,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ModelRoute
	for rows.Next() {
		var r model.ModelRoute
		var en int
		if err := rows.Scan(&r.ID, &r.Pattern, &r.Provider, &r.Priority, &en); err != nil {
			return nil, err
		}
		r.Enabled = en != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) sqliteGetModelRoute(id string) (model.ModelRoute, error) {
	var r model.ModelRoute
	var en int
	err := s.db.QueryRow(`SELECT id,pattern,provider,priority,enabled FROM model_routes WHERE id=?`, id).
		Scan(&r.ID, &r.Pattern, &r.Provider, &r.Priority, &en)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	r.Enabled = en != 0
	return r, err
}

func (s *Store) sqliteUpsertModelRoute(r model.ModelRoute) error {
	_, err := s.db.Exec(`
INSERT INTO model_routes(id,pattern,provider,priority,enabled) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  pattern=excluded.pattern, provider=excluded.provider, priority=excluded.priority, enabled=excluded.enabled
`, r.ID, r.Pattern, r.Provider, r.Priority, boolInt(r.Enabled))
	return err
}

func (s *Store) sqliteDeleteModelRoute(id string) error {
	res, err := s.db.Exec(`DELETE FROM model_routes WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
