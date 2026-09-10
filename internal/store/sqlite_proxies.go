package store

import (
	"database/sql"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

func (s *Store) sqliteListProxies() ([]model.Proxy, error) {
	rows, err := s.db.Query(`SELECT id,name,type,url,enabled,created_at FROM proxies ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Proxy
	for rows.Next() {
		var p model.Proxy
		var en int
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.URL, &en, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Enabled = en != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) sqliteGetProxy(id string) (model.Proxy, error) {
	var p model.Proxy
	var en int
	err := s.db.QueryRow(`SELECT id,name,type,url,enabled,created_at FROM proxies WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Type, &p.URL, &en, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	p.Enabled = en != 0
	return p, err
}

func (s *Store) sqliteUpsertProxy(p model.Proxy) error {
	if p.CreatedAt == "" {
		p.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if p.Type == "" {
		p.Type = "http"
	}
	_, err := s.db.Exec(`
INSERT INTO proxies(id,name,type,url,enabled,created_at) VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name, type=excluded.type, url=excluded.url, enabled=excluded.enabled
`, p.ID, p.Name, p.Type, p.URL, boolInt(p.Enabled), p.CreatedAt)
	return err
}

func (s *Store) sqliteDeleteProxy(id string) error {
	res, err := s.db.Exec(`DELETE FROM proxies WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
