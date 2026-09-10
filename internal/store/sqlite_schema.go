package store

import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, password_salt TEXT NOT NULL, role TEXT NOT NULL, quota_total INTEGER NOT NULL, quota_used INTEGER NOT NULL, quota_reserved INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS api_keys (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL, secret_hash TEXT NOT NULL UNIQUE, prefix TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL, created_at TEXT NOT NULL, last_used_at TEXT NOT NULL DEFAULT '', FOREIGN KEY(user_id) REFERENCES users(id));
CREATE TABLE IF NOT EXISTS usage_logs (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, key_id TEXT NOT NULL, model TEXT NOT NULL, tokens INTEGER NOT NULL, cost INTEGER NOT NULL, status TEXT NOT NULL, account_id TEXT NOT NULL, attempts INTEGER NOT NULL, stream_broken INTEGER NOT NULL DEFAULT 0, compensated INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, FOREIGN KEY(user_id) REFERENCES users(id));
CREATE TABLE IF NOT EXISTS sessions (token TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at TEXT NOT NULL, FOREIGN KEY(user_id) REFERENCES users(id));
CREATE TABLE IF NOT EXISTS accounts (id TEXT PRIMARY KEY, name TEXT NOT NULL, provider TEXT NOT NULL, base_url TEXT NOT NULL, priority INTEGER NOT NULL, healthy INTEGER NOT NULL, load REAL NOT NULL, cooldown_until TEXT NOT NULL, last_error TEXT NOT NULL, consecutive_timeouts INTEGER NOT NULL, consecutive_403 INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS model_prices (model TEXT PRIMARY KEY, rate_input INTEGER NOT NULL, rate_output INTEGER NOT NULL, rate_cache_read INTEGER NOT NULL, rate_cache_write INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS billing_groups (name TEXT PRIMARY KEY, ratio INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS account_credentials (account_id TEXT PRIMARY KEY, access_token TEXT NOT NULL DEFAULT '', refresh_token TEXT NOT NULL DEFAULT '', extra_json TEXT NOT NULL DEFAULT '', expires_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS proxies (id TEXT PRIMARY KEY, name TEXT NOT NULL, type TEXT NOT NULL DEFAULT 'http', url TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS channels (id TEXT PRIMARY KEY, name TEXT NOT NULL, provider TEXT NOT NULL DEFAULT '', group_name TEXT NOT NULL DEFAULT 'default', priority INTEGER NOT NULL DEFAULT 1, enabled INTEGER NOT NULL DEFAULT 1, models_json TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS channel_accounts (channel_id TEXT NOT NULL, account_id TEXT NOT NULL, model_pattern TEXT NOT NULL DEFAULT '', priority INTEGER NOT NULL DEFAULT 1, PRIMARY KEY(channel_id, account_id));
CREATE TABLE IF NOT EXISTS model_routes (id TEXT PRIMARY KEY, pattern TEXT NOT NULL, provider TEXT NOT NULL, priority INTEGER NOT NULL DEFAULT 1, enabled INTEGER NOT NULL DEFAULT 1);
CREATE INDEX IF NOT EXISTS idx_keys_user ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_usage_user ON usage_logs(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_model_routes_priority ON model_routes(priority, provider);
CREATE TABLE IF NOT EXISTS payment_orders (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  package_id TEXT NOT NULL DEFAULT '',
  amount_fiat_cents INTEGER NOT NULL,
  currency TEXT NOT NULL,
  quota_credit INTEGER NOT NULL,
  status TEXT NOT NULL,
  provider_trade_no TEXT NOT NULL DEFAULT '',
  pay_url TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  paid_at TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_orders_idem ON payment_orders(user_id, idempotency_key) WHERE idempotency_key != '';
CREATE TABLE IF NOT EXISTS quota_topups (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  order_id TEXT NOT NULL DEFAULT '',
  credit INTEGER NOT NULL,
  source TEXT NOT NULL,
  operator_id TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_topups_order ON quota_topups(order_id) WHERE order_id != '';
`

func openSQLite(path string) (*sql.DB, error) {
	// WAL lets readers run while a writer holds the file, and busy_timeout
	// makes a writer wait its turn instead of failing. Without these, any
	// concurrent access that is not already serialised by Store.mu returns
	// SQLITE_BUSY "database is locked" - which surfaces to an API client as a
	// 500 under exactly the load the gateway is supposed to absorb. Relying on
	// the in-process mutex alone means the storage layer is only safe as long
	// as every future caller remembers to take it.
	//
	// Foreign-key enforcement is deliberately NOT switched on here. The schema
	// declares the references, but turning enforcement on is a behaviour change
	// for existing databases and belongs in its own task with its own evidence,
	// not smuggled in beside a concurrency fix.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Existing DBs created before prefix was added need a soft ALTER.
	if err = softAddColumn(db, `ALTER TABLE api_keys ADD COLUMN prefix TEXT NOT NULL DEFAULT ''`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = softAddColumn(db, `ALTER TABLE users ADD COLUMN quota_reserved INTEGER NOT NULL DEFAULT 0`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = softAddColumn(db, `ALTER TABLE accounts ADD COLUMN proxy_id TEXT NOT NULL DEFAULT ''`); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err = softAddColumn(db, `ALTER TABLE accounts ADD COLUMN consecutive_429 INTEGER NOT NULL DEFAULT 0`); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Token classes and the rate SNAPSHOT each row was billed at. The snapshot
	// is the point: a bill is a statement about the past, so editing the price
	// list today must not rewrite invoices issued last month.
	for _, stmt := range []string{
		`ALTER TABLE usage_logs ADD COLUMN tokens_input INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE usage_logs ADD COLUMN tokens_output INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE usage_logs ADD COLUMN tokens_cache_read INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE usage_logs ADD COLUMN tokens_cache_write INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE usage_logs ADD COLUMN rate_input INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE usage_logs ADD COLUMN rate_output INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE usage_logs ADD COLUMN rate_cache_read INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE usage_logs ADD COLUMN rate_cache_write INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE usage_logs ADD COLUMN rate_ratio INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN billing_group TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN ratio_override INTEGER NOT NULL DEFAULT 0`,
	} {
		if err = softAddColumn(db, stmt); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	if err = seedDefaultModelRoutes(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Reservations are in-flight state, and nothing is in flight while this
	// process is starting. A crash mid-request would otherwise leave quota
	// held by a request that no longer exists - the user would lose that
	// quota permanently, with nothing on the board to explain it.
	//
	// This is correct for one process against one file, which is what the
	// README says this is. It is NOT correct if a second instance is ever
	// pointed at the same database: this would free reservations belonging to
	// that instance's live requests. Moving to Postgres means moving this
	// reset behind an instance identity.
	if _, err = db.Exec(`UPDATE users SET quota_reserved = 0`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// softAddColumn runs an ALTER that may already have been applied. SQLite has
// no ADD COLUMN IF NOT EXISTS, so "duplicate column" is the success case on
// every start after the first.
func softAddColumn(db *sql.DB, stmt string) error {
	if _, err := db.Exec(stmt); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	return nil
}

// seedDefaultModelRoutes inserts the built-in claude/codex/antigravity patterns when the
// table is empty so a fresh install routes models before the wrong provider
// is tried.
func seedDefaultModelRoutes(db *sql.DB) error {
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
			`INSERT INTO model_routes(id, pattern, provider, priority, enabled) VALUES(?,?,?,?,1)`,
			d.id, d.pattern, d.provider, d.priority,
		); err != nil {
			return err
		}
	}
	return nil
}
