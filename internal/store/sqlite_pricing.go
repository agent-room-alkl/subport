package store

// Price list and ratios.
//
// Two things live here and they are kept apart on purpose: what a model costs
// (rates, per token class) and who gets a discount (ratios, per group or per
// user). Mixing them is how a price change for one model quietly becomes a
// price change for one customer.

import (
	"database/sql"
	"errors"

	"github.com/agent-room-alkl/subport/internal/model"
)

// DefaultRatio is what a user with no group and no override is charged at:
// exactly list price. Zero is NOT a valid ratio - a missing configuration must
// never mean "free".
const DefaultRatio = model.RatioScale

func (s *Store) sqliteSetModelPrice(p model.ModelPrice) error {
	_, err := s.db.Exec(
		`INSERT INTO model_prices(model,rate_input,rate_output,rate_cache_read,rate_cache_write)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(model) DO UPDATE SET rate_input=excluded.rate_input, rate_output=excluded.rate_output,
		   rate_cache_read=excluded.rate_cache_read, rate_cache_write=excluded.rate_cache_write`,
		p.Model, p.Input, p.Output, p.CacheRead, p.CacheWrite,
	)
	return err
}

func (s *Store) sqliteModelPrice(name string) (model.ModelPrice, error) {
	var p model.ModelPrice
	err := s.db.QueryRow(
		`SELECT model,rate_input,rate_output,rate_cache_read,rate_cache_write FROM model_prices WHERE model=?`,
		name,
	).Scan(&p.Model, &p.Input, &p.Output, &p.CacheRead, &p.CacheWrite)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ModelPrice{}, ErrNotFound
	}
	return p, err
}

func (s *Store) sqliteModelPrices() ([]model.ModelPrice, error) {
	rows, err := s.db.Query(`SELECT model,rate_input,rate_output,rate_cache_read,rate_cache_write FROM model_prices ORDER BY model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ModelPrice{}
	for rows.Next() {
		var p model.ModelPrice
		if err := rows.Scan(&p.Model, &p.Input, &p.Output, &p.CacheRead, &p.CacheWrite); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) sqliteSetGroupRatio(name string, ratio int64) error {
	_, err := s.db.Exec(
		`INSERT INTO billing_groups(name,ratio) VALUES(?,?)
		 ON CONFLICT(name) DO UPDATE SET ratio=excluded.ratio`,
		name, ratio,
	)
	return err
}

func (s *Store) sqliteGroupRatio(name string) (int64, error) {
	var ratio int64
	err := s.db.QueryRow(`SELECT ratio FROM billing_groups WHERE name=?`, name).Scan(&ratio)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return ratio, err
}

func (s *Store) sqliteSetUserBilling(userID, group string, override int64) error {
	res, err := s.db.Exec(
		`UPDATE users SET billing_group=?, ratio_override=? WHERE id=?`,
		group, override, userID,
	)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) sqliteUserBilling(userID string) (group string, override int64, err error) {
	err = s.db.QueryRow(`SELECT billing_group, ratio_override FROM users WHERE id=?`, userID).Scan(&group, &override)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	return group, override, err
}
