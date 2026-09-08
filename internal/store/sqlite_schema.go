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
CREATE INDEX IF NOT EXISTS idx_keys_user ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_usage_user ON usage_logs(user_id, created_at);
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
