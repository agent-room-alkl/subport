# Postgres (Azure)

Database: `subport` on `kiwi-pg.postgres.database.azure.com`.

Credentials live in project `.env` (gitignored) and Azure Key Vault secret `kiwi-pg-admin-password`.

## What was migrated

- Full Subport table schema (14 tables), clean data
- One admin user: `admin` / `subport-admin` (quota 1_000_000)
- Default model_routes only — no accounts, keys, OAuth tokens, usage, or smoke users

## How the app selects a dialect

At startup, `store.OpenStore` prefers **Postgres** when either is set:

- `DATABASE_URL` (recommended; include `sslmode=require`), or
- `PGHOST` (+ `PGUSER` / `PGPASSWORD` / `PGPORT` / `PGDATABASE`)

Otherwise it falls back to **SQLite** beside `SUBPORT_DB` (default `subport-data.json` → `subport-data.json.sqlite`).

`cmd/subport/main.go` calls `loadDotEnv(".env")` so launching `subport.exe` from the repo root picks up `.env` automatically. You can also export the same variables in a restart script.

## Run with Postgres

```bash
# from repo root (loads .env)
set SUBPORT_ALIPAY_MOCK=1
set SUBPORT_INVITE_CODE=
.\subport.exe
```

Logs include `store=postgres` when the Postgres driver is active.

SQLite remains available only when no Postgres env is configured.

Note: Azure `subport` stores flag columns (`healthy`, `enabled`, `stream_broken`, `compensated`) as SMALLINT 0/1 (not Postgres BOOLEAN). The store binds and scans both INTEGER/SMALLINT and BOOLEAN-compatible values.

## Connect with psql

```bash
# from .env values
psql "$DATABASE_URL"
```

Never commit `.env` or passwords.
