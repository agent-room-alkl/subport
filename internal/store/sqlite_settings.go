package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

const settingModelAliases = "model_aliases"

func ensureAppSettingsTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS app_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	return err
}

func ensureAppSettingsTablePG(db *sql.DB) error {
	return ensureAppSettingsTable(db)
}

func (s *Store) sqliteGetSetting(key string) (string, error) {
	var v string
	err := s.queryRow(`SELECT value FROM app_settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) sqliteSetSetting(key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.exec(`
INSERT INTO app_settings(key, value, updated_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at
`, key, value, now)
	return err
}

func (s *Store) GetSetting(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteGetSetting(key)
}

func (s *Store) SetSetting(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteSetSetting(key, value)
}

// GetModelAliasesJSON returns the stored model alias config JSON (may be empty).
func (s *Store) GetModelAliasesJSON() string {
	v, err := s.GetSetting(settingModelAliases)
	if err != nil {
		return ""
	}
	return v
}

// SetModelAliasesJSON persists model alias config JSON.
func (s *Store) SetModelAliasesJSON(raw string) error {
	return s.SetSetting(settingModelAliases, strings.TrimSpace(raw))
}

func (s *Store) sqliteAccountByNameProvider(name, provider string) (model.Account, error) {
	name = strings.TrimSpace(name)
	provider = strings.ToLower(strings.TrimSpace(provider))
	return scanSQLiteAccount(s.queryRow(`
SELECT id,name,provider,base_url,priority,healthy,load,cooldown_until,last_error,consecutive_timeouts,consecutive_403,COALESCE(consecutive_429,0)
FROM accounts WHERE name=? AND lower(provider)=? LIMIT 1`, name, provider))
}

// AccountByNameProvider finds an account by exact name + provider (provider case-insensitive).
func (s *Store) AccountByNameProvider(name, provider string) (model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.sqliteAccountByNameProvider(name, provider)
	if err == sql.ErrNoRows {
		return a, ErrNotFound
	}
	return a, err
}
