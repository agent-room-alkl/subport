package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func envWantsPostgres() bool {
	return strings.TrimSpace(os.Getenv("DATABASE_URL")) != "" || strings.TrimSpace(os.Getenv("PGHOST")) != ""
}

func postgresDSN() (string, error) {
	if dsn := strings.TrimSpace(os.Getenv("DATABASE_URL")); dsn != "" {
		return ensureSSLMode(dsn), nil
	}
	host := strings.TrimSpace(os.Getenv("PGHOST"))
	if host == "" {
		return "", fmt.Errorf("postgres: DATABASE_URL or PGHOST required")
	}
	user := strings.TrimSpace(os.Getenv("PGUSER"))
	pass := os.Getenv("PGPASSWORD")
	port := strings.TrimSpace(os.Getenv("PGPORT"))
	if port == "" {
		port = "5432"
	}
	dbName := strings.TrimSpace(os.Getenv("PGDATABASE"))
	if dbName == "" {
		dbName = "subport"
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, pass),
		Host:   host + ":" + port,
		Path:   "/" + dbName,
	}
	q := u.Query()
	q.Set("sslmode", "require")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func ensureSSLMode(dsn string) string {
	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil {
			if !strings.Contains(dsn, "sslmode=") {
				if strings.Contains(dsn, "?") {
					return dsn + "&sslmode=require"
				}
				return dsn + "?sslmode=require"
			}
			return dsn
		}
		q := u.Query()
		if q.Get("sslmode") == "" {
			q.Set("sslmode", "require")
			u.RawQuery = q.Encode()
		}
		return u.String()
	}
	if !strings.Contains(dsn, "sslmode=") {
		return strings.TrimSpace(dsn) + " sslmode=require"
	}
	return dsn
}

func openPostgres() (*sql.DB, error) {
	dsn, err := postgresDSN()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Schema is already applied on Azure. IF NOT EXISTS is a safety net only;
	// never DROP.
	if err := ensurePostgresSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := seedDefaultModelRoutesPG(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureAvailableModelsTablePG(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureAppSettingsTablePG(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := seedAvailableModels(db, true); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`UPDATE users SET quota_reserved = 0`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ensurePostgresSchema(db *sql.DB) error {
	const pgSchema = `
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  password_salt TEXT NOT NULL,
  role TEXT NOT NULL,
  quota_total BIGINT NOT NULL,
  quota_used BIGINT NOT NULL,
  quota_reserved BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  billing_group TEXT NOT NULL DEFAULT '',
  ratio_override BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  name TEXT NOT NULL,
  secret_hash TEXT NOT NULL UNIQUE,
  prefix TEXT NOT NULL DEFAULT '',
  enabled SMALLINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS usage_logs (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  key_id TEXT NOT NULL,
  model TEXT NOT NULL,
  tokens BIGINT NOT NULL,
  cost BIGINT NOT NULL,
  status TEXT NOT NULL,
  account_id TEXT NOT NULL,
  attempts INTEGER NOT NULL,
  stream_broken SMALLINT NOT NULL DEFAULT 0,
  compensated SMALLINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  tokens_input BIGINT NOT NULL DEFAULT 0,
  tokens_output BIGINT NOT NULL DEFAULT 0,
  tokens_cache_read BIGINT NOT NULL DEFAULT 0,
  tokens_cache_write BIGINT NOT NULL DEFAULT 0,
  rate_input BIGINT NOT NULL DEFAULT 0,
  rate_output BIGINT NOT NULL DEFAULT 0,
  rate_cache_read BIGINT NOT NULL DEFAULT 0,
  rate_cache_write BIGINT NOT NULL DEFAULT 0,
  rate_ratio BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS accounts (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL,
  base_url TEXT NOT NULL,
  priority INTEGER NOT NULL,
  healthy SMALLINT NOT NULL,
  load DOUBLE PRECISION NOT NULL,
  cooldown_until TEXT NOT NULL,
  last_error TEXT NOT NULL,
  consecutive_timeouts INTEGER NOT NULL,
  consecutive_403 INTEGER NOT NULL,
  consecutive_429 INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS model_prices (
  model TEXT PRIMARY KEY,
  rate_input BIGINT NOT NULL,
  rate_output BIGINT NOT NULL,
  rate_cache_read BIGINT NOT NULL,
  rate_cache_write BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS billing_groups (
  name TEXT PRIMARY KEY,
  ratio BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS account_credentials (
  account_id TEXT PRIMARY KEY,
  access_token TEXT NOT NULL DEFAULT '',
  refresh_token TEXT NOT NULL DEFAULT '',
  extra_json TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS channels (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL DEFAULT '',
  group_name TEXT NOT NULL DEFAULT 'default',
  priority INTEGER NOT NULL DEFAULT 1,
  enabled SMALLINT NOT NULL DEFAULT 1,
  models_json TEXT NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS channel_accounts (
  channel_id TEXT NOT NULL,
  account_id TEXT NOT NULL,
  model_pattern TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(channel_id, account_id)
);
CREATE TABLE IF NOT EXISTS model_routes (
  id TEXT PRIMARY KEY,
  pattern TEXT NOT NULL,
  provider TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 1,
  enabled SMALLINT NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS payment_orders (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  package_id TEXT NOT NULL DEFAULT '',
  amount_fiat_cents BIGINT NOT NULL,
  currency TEXT NOT NULL,
  quota_credit BIGINT NOT NULL,
  status TEXT NOT NULL,
  provider_trade_no TEXT NOT NULL DEFAULT '',
  pay_url TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  paid_at TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS quota_topups (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  order_id TEXT NOT NULL DEFAULT '',
  credit BIGINT NOT NULL,
  source TEXT NOT NULL,
  operator_id TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_keys_user ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_usage_user ON usage_logs(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_model_routes_priority ON model_routes(priority, provider);
`
	if _, err := db.Exec(pgSchema); err != nil {
		return err
	}
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_orders_idem ON payment_orders(user_id, idempotency_key) WHERE idempotency_key <> ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_topups_order ON quota_topups(order_id) WHERE order_id <> ''`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func seedDefaultModelRoutesPG(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM model_routes`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	defaults := []struct {
		id, pattern, provider string
		priority              int
	}{
		{"route-claude", "^claude", "claude", 1},
		{"route-codex", "^gpt-|^o[0-9]|^codex", "codex", 1},
		{"route-antigravity", "^gemini|^tab_flash|^gpt-oss", "antigravity", 1},
	}
	for _, d := range defaults {
		if _, err := db.Exec(
			`INSERT INTO model_routes(id, pattern, provider, priority, enabled) VALUES($1,$2,$3,$4,1)`,
			d.id, d.pattern, d.provider, d.priority,
		); err != nil {
			return err
		}
	}
	return nil
}
