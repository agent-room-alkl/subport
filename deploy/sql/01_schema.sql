-- Subport PostgreSQL schema. Idempotent (IF NOT EXISTS); safe to re-run.
-- This mirrors internal/store/postgres.go (ensurePostgresSchema) plus the
-- available_models and app_settings tables. The Go process also applies this
-- on startup as a safety net; this file lets you provision a fresh database
-- (or review the schema) without running the app.
--
--   psql "host=<host> user=<admin> dbname=subport sslmode=require" -f 01_schema.sql
--
-- Boolean-like columns are SMALLINT (0/1) to match the Go store layer.

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

CREATE TABLE IF NOT EXISTS available_models (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  enabled SMALLINT NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_keys_user ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_usage_user ON usage_logs(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_model_routes_priority ON model_routes(priority, provider);
CREATE INDEX IF NOT EXISTS idx_available_models_sort ON available_models(enabled, sort_order, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_orders_idem ON payment_orders(user_id, idempotency_key) WHERE idempotency_key <> '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_topups_order ON quota_topups(order_id) WHERE order_id <> '';
