package store

import (
	"database/sql"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

func (s *Store) sqliteListChannels() ([]model.Channel, error) {
	rows, err := s.db.Query(`SELECT id,name,provider,group_name,priority,enabled,models_json,created_at FROM channels ORDER BY priority,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Channel
	for rows.Next() {
		var c model.Channel
		var en int
		if err := rows.Scan(&c.ID, &c.Name, &c.Provider, &c.GroupName, &c.Priority, &en, &c.ModelsJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Enabled = en != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) sqliteGetChannel(id string) (model.Channel, error) {
	var c model.Channel
	var en int
	err := s.db.QueryRow(`SELECT id,name,provider,group_name,priority,enabled,models_json,created_at FROM channels WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &c.Provider, &c.GroupName, &c.Priority, &en, &c.ModelsJSON, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return c, ErrNotFound
	}
	c.Enabled = en != 0
	return c, err
}

func (s *Store) sqliteUpsertChannel(c model.Channel) error {
	if c.CreatedAt == "" {
		c.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if c.GroupName == "" {
		c.GroupName = "default"
	}
	if c.ModelsJSON == "" {
		c.ModelsJSON = "[]"
	}
	_, err := s.db.Exec(`
INSERT INTO channels(id,name,provider,group_name,priority,enabled,models_json,created_at)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name, provider=excluded.provider, group_name=excluded.group_name,
  priority=excluded.priority, enabled=excluded.enabled, models_json=excluded.models_json
`, c.ID, c.Name, c.Provider, c.GroupName, c.Priority, boolInt(c.Enabled), c.ModelsJSON, c.CreatedAt)
	return err
}

func (s *Store) sqliteDeleteChannel(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM channel_accounts WHERE channel_id=?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM channels WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) sqliteListChannelAccounts(channelID string) ([]model.ChannelAccount, error) {
	rows, err := s.db.Query(`SELECT channel_id,account_id,model_pattern,priority FROM channel_accounts WHERE channel_id=? ORDER BY priority,account_id`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ChannelAccount
	for rows.Next() {
		var m model.ChannelAccount
		if err := rows.Scan(&m.ChannelID, &m.AccountID, &m.ModelPattern, &m.Priority); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) sqliteUpsertChannelAccount(m model.ChannelAccount) error {
	_, err := s.db.Exec(`
INSERT INTO channel_accounts(channel_id,account_id,model_pattern,priority) VALUES(?,?,?,?)
ON CONFLICT(channel_id, account_id) DO UPDATE SET
  model_pattern=excluded.model_pattern, priority=excluded.priority
`, m.ChannelID, m.AccountID, m.ModelPattern, m.Priority)
	return err
}

func (s *Store) sqliteDeleteChannelAccount(channelID, accountID string) error {
	res, err := s.db.Exec(`DELETE FROM channel_accounts WHERE channel_id=? AND account_id=?`, channelID, accountID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
