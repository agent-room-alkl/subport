# Claude.ai subscription provider

Subport can relay OpenAI-compatible `POST /v1/chat/completions` through **your own** Claude.ai / Claude Code subscription via OAuth access tokens (not the paid Anthropic API `x-api-key` product).

This follows the same subscription OAuth pattern used by the local `_ref/sub2api` reference: session cookie → org UUID → PKCE authorize → token at `platform.claude.com`, then `POST https://api.anthropic.com/v1/messages` with `Authorization: Bearer <access_token>` and Claude Code `anthropic-beta` headers.

Secrets are **never** stored in the accounts table or committed. Prefer environment variables.

## Env vars

| Variable | Required | Purpose |
|---|---|---|
| `SUBPORT_CLAUDE_ACCESS_TOKEN` | one of these | OAuth access token (preferred if you already have one) |
| `SUBPORT_PROVIDER_KEY_CLAUDE` | one of these | Same as access token (matches Subport’s `SUBPORT_PROVIDER_KEY_<PROVIDER>` convention) |
| `SUBPORT_CLAUDE_REFRESH_TOKEN` | recommended | Refresh token; used on 401 and for refresh-only bootstrap |
| `SUBPORT_CLAUDE_SESSION` | alternative | Claude.ai `sessionKey` cookie; exchanged for tokens at startup |
| `SUBPORT_CLAUDE_ORG` | optional | Organization UUID; discovered via `/api/organizations` when omitted |

**Credential resolution order for requests:** in-memory token (from session exchange / refresh) → `SUBPORT_CLAUDE_ACCESS_TOKEN` → `SUBPORT_PROVIDER_KEY_CLAUDE` → `SUBPORT_PROVIDER_KEY`.

On startup, if any Claude credential is available, Subport upserts account `acct-claude-1` (`provider=claude`, `base_url=https://api.anthropic.com`, priority 1, healthy) and pauses seeded `mock` accounts in priority tier 1 so traffic prefers Claude.

## How to get a token

1. **Access + refresh tokens** (best for long-running Subport): use Claude Code / Anthropic’s OAuth setup-token flow on your own machine, or exchange a browser `sessionKey` once via Subport’s bootstrap (`SUBPORT_CLAUDE_SESSION`).
2. **sessionKey**: while logged into [claude.ai](https://claude.ai) in your browser, copy the `sessionKey` cookie value into `SUBPORT_CLAUDE_SESSION` for local use only. Do not paste it into chat or commit it.

Do **not** share subscription sessions across tenants. This binding is for the operator’s own subscription.

## Smoke test (PowerShell)

```powershell
# From D:\workspace\subport — set your real secrets in the shell, not in files you commit
$env:SUBPORT_CLAUDE_ACCESS_TOKEN = "<your-oauth-access-token>"
# optional:
# $env:SUBPORT_CLAUDE_REFRESH_TOKEN = "<refresh>"
# or instead of access token:
# $env:SUBPORT_CLAUDE_SESSION = "<sessionKey>"
# $env:SUBPORT_CLAUDE_ORG = "<org-uuid>"

go run ./cmd/subport
```

In another shell:

```powershell
# 1. Register (invite default: subport-invite)
$reg = Invoke-RestMethod -Method POST http://127.0.0.1:8080/api/auth/register `
  -ContentType 'application/json' `
  -Body '{"username":"claude-demo","password":"demo-password","invite_code":"subport-invite"}'
$token = $reg.token

# 2. Create API key
$keyResp = Invoke-RestMethod -Method POST http://127.0.0.1:8080/api/console/keys `
  -Headers @{ Authorization = "Bearer $token" } `
  -ContentType 'application/json' `
  -Body '{"name":"claude-smoke"}'
$apiKey = $keyResp.secret

# 3. Chat completions (non-stream)
Invoke-RestMethod -Method POST http://127.0.0.1:8080/v1/chat/completions `
  -Headers @{ Authorization = "Bearer $apiKey" } `
  -ContentType 'application/json' `
  -Body '{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Say hi in one word"}]}'
```

curl equivalent:

```bash
TOKEN=$(curl -s -X POST localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"claude-demo","password":"demo-password","invite_code":"subport-invite"}' \
  | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

KEY=$(curl -s -X POST localhost:8080/api/console/keys \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"claude-smoke"}' \
  | grep -o '"secret":"[^"]*"' | cut -d'"' -f4)

curl -s -X POST localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Say hi in one word"}]}'
```

Confirm the account is bound:

```bash
# admin session → GET /api/accounts should list provider=claude, id=acct-claude-1
```

## Not implemented (v1)

- True Anthropic SSE streaming passthrough (Stream currently completes non-stream then emits one OpenAI-shaped SSE chunk + `[DONE]`).
- Persisting refreshed tokens back to disk/env (in-memory only for the process lifetime).
- Official Anthropic API-key provider (`x-api-key`) — separate from subscription; can be added later as `provider=anthropic`.
- Multi-tenant session sharing / credential pooling.