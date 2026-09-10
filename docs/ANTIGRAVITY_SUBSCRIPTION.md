# Antigravity subscription → OpenAI-compatible API

Subport can bind an **Antigravity / Google Cloud Code** subscription (OAuth) and
expose it as an OpenAI-compatible `/v1/chat/completions` endpoint. This is **not**
a Google Cloud paid API-key path.

Upstream (from `_ref/sub2api`):

- Token refresh: `POST https://oauth2.googleapis.com/token` (Google OAuth client used by Antigravity)
- Chat: `POST https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse`
- Auth header: `Authorization: Bearer <access_token>`
- User-Agent: `antigravity/<version> windows/amd64`
- Body: v1internal wrapper `{ project, requestId, userAgent, requestType, model, request }` where `request` is Gemini `contents` / `systemInstruction`

Subport public prefixes like `/antigravity/v1/messages` or `/antigravity/v1beta/` are optional stretch goals; core path is account-pool + chat completions.

## Credentials

Supply tokens via (first match wins for access):

1. Admin credential modal on `acct-antigravity-1` (stored in `account_credentials`)
2. Env: `SUBPORT_ANTIGRAVITY_ACCESS_TOKEN`, `SUBPORT_ANTIGRAVITY_REFRESH_TOKEN`, `SUBPORT_ANTIGRAVITY_PROJECT_ID`
   - aliases: `SUBPORT_PROVIDER_KEY_ANTIGRAVITY`
3. Working-dir token files (gitignored): `.antigravity_access_token`, `.antigravity_refresh_token`, `.antigravity_project_id`
4. Optional auth file: `%USERPROFILE%\.antigravity\auth.json` or `SUBPORT_ANTIGRAVITY_AUTH_FILE`

`project_id` is required for upstream calls (also stored in credential `extra_json`).

Never paste tokens into chat or commit them.

## Bootstrap

On startup Subport:

1. Loads access/refresh/project_id from env, token files, or auth.json
2. Upserts healthy account `acct-antigravity-1` with `provider=antigravity`, base `https://cloudcode-pa.googleapis.com`
3. Syncs tokens into SQLite `account_credentials`
4. Pauses demo `mock` accounts when any real subscription is present
5. Ensures model_route `route-antigravity` (`^gemini|^tab_flash|^gpt-oss`) → `antigravity`

## Model routing

Fresh installs seed:

| id | pattern | provider |
|---|---|---|
| route-claude | `^claude` | claude |
| route-codex | `^gpt-\|^o[0-9]\|^codex` | codex |
| route-antigravity | `^gemini\|^tab_flash\|^gpt-oss` | antigravity |

Existing DBs: bootstrap upserts `route-antigravity` if missing. Add Antigravity Claude aliases (e.g. `claude-.*-thinking`) as extra routes in admin if needed.

## Smoke test

```powershell
cd D:\workspace\subport
# set SUBPORT_ANTIGRAVITY_* in the shell, not in committed files
.\subport.exe
```

```powershell
$admin = Invoke-RestMethod http://127.0.0.1:8080/v1/admin/login -Method POST -ContentType application/json -Body '{"username":"admin","password":"subport-admin"}'
$key = (Invoke-RestMethod http://127.0.0.1:8080/v1/admin/api-keys -Headers @{Authorization="Bearer $($admin.token)"} -Method POST -ContentType application/json -Body '{"name":"ag-smoke"}').key
Invoke-RestMethod http://127.0.0.1:8080/v1/chat/completions -Method POST -Headers @{Authorization="Bearer $key"} -ContentType application/json -Body '{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"Say Hi!"}]}'
```

Gemini models route to `acct-antigravity-1`; Claude stays on `acct-claude-1`; GPT/Codex on `acct-codex-1`.

## One-click Google OAuth (admin)

1. Start Subport (`subport.exe` / `go run ./cmd/subport`) — it listens on `:8080` and starts an Antigravity OAuth callback on `http://127.0.0.1:8085/callback` (same RedirectURI / ClientID as sub2api).
2. Open admin UI → Accounts → row `acct-antigravity-1` → click **Google 授权**.
3. Complete Google login in the popup. Google redirects to `localhost:8085/callback`.
4. The callback page auto-exchanges the code (PKCE) and upserts `account_credentials` (access/refresh + `project_id` in extra_json), hydrates gateway runtime, and marks the account healthy.
5. If auto-exchange fails (popup blocked, etc.), paste the full callback URL into the modal **粘贴回调 URL** and click **用回调 URL 完成交换**.

Admin APIs (requireAdmin):

- `POST /api/accounts/{id}/antigravity/oauth/start` → `{auth_url, session_id, state}`
- `POST /api/accounts/{id}/antigravity/oauth/exchange` → body `{session_id, state?, code?, callback_url?}`
- `GET /api/accounts/{id}/antigravity/oauth/status?session_id=`

Do not commit tokens or OAuth client credentials. Set `SUBPORT_ANTIGRAVITY_CLIENT_ID` (or `ANTIGRAVITY_OAUTH_CLIENT_ID`) and `SUBPORT_ANTIGRAVITY_CLIENT_SECRET` (or `ANTIGRAVITY_OAUTH_CLIENT_SECRET`) in the environment; there are no hardcoded defaults.