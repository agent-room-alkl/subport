# ChatGPT / Codex subscription → OpenAI-compatible API

Subport can bind a **ChatGPT / Codex CLI subscription** (OAuth) and expose it as
an OpenAI-compatible `/v1/chat/completions` endpoint. This is **not** the OpenAI
Platform pay-per-use API key (`sk-…` → `api.openai.com`).

## Credentials

Prefer the local Codex CLI login (already present after `codex` auth):

- `%USERPROFILE%\.codex\auth.json` — `tokens.access_token`, `tokens.refresh_token`, `tokens.account_id`

Or drop token files next to the Subport working directory (gitignored):

- `.codex_access_token`
- `.codex_refresh_token` (optional; used on 401)
- `.codex_account_id` (optional if auth.json has it)

Env overrides (same names with `SUBPORT_CODEX_*`) also work when the process
actually inherits them.

Never paste tokens into chat or commit them.

## Bootstrap

On startup Subport:

1. Loads access/refresh/account id from env, token files, or `~/.codex/auth.json`
2. Upserts healthy account `acct-codex-1` with `provider=codex`
3. Pauses demo `mock` accounts when any real subscription is present

Upstream call: `POST https://chatgpt.com/backend-api/codex/responses` with
`Authorization: Bearer …` and `chatgpt-account-id`.

## Smoke test

```powershell
cd D:\workspace\subport
.\subport.exe
```

```powershell
$admin = Invoke-RestMethod http://127.0.0.1:8080/v1/admin/login -Method POST -ContentType application/json -Body '{"username":"admin","password":"subport-admin"}'
$key = (Invoke-RestMethod http://127.0.0.1:8080/v1/admin/api-keys -Headers @{Authorization="Bearer $($admin.token)"} -Method POST -ContentType application/json -Body '{"name":"codex-smoke"}').key
Invoke-RestMethod http://127.0.0.1:8080/v1/chat/completions -Method POST -Headers @{Authorization="Bearer $key"} -ContentType application/json -Body '{"model":"gpt-5.5","messages":[{"role":"user","content":"Say Hi!"}]}'
```

GPT / Codex models route to `acct-codex-1`; Claude models stay on `acct-claude-1`.
