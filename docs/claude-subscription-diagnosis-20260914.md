# Claude subscription diagnosis — 2026-09-14

Account tested: Subport `acct-claude-1`. No credentials or personal identifiers are included here.

## Observed evidence

- `/api/oauth/profile`, using the saved account access token, reports an active Claude Pro subscription, `billing_type=stripe_subscription`, `rate_limit_tier=default_claude_ai`, and application `slug=claude-code`.
- `/api/oauth/usage`, using that same token, initially reported five-hour and seven-day utilization of 0%. Extra usage was disabled with `disabled_reason=out_of_credits`.
- The models endpoint listed Sonnet, Opus, Fable and Haiku models. Listing alone does not establish inference access.
- Subport `/v1/chat/completions` successfully called Haiku 4.5. Its usage log identifies `acct-claude-1` as the account used.
- Direct `/v1/messages` calls with that same token returned 429 `rate_limit_error` / `Error` for Sonnet 5, Sonnet 4.6, Opus 5/4.8/4.7/4.6 and Fable 5.1/5. No Retry-After header was supplied.
- A subsequent comparison returned 429 for dated Sonnet 4.5 (`claude-sonnet-4-5-20250929`) immediately followed by 200 and text `OK` for dated Haiku 4.5.
- The Sonnet response had `x-should-retry=true` but no detailed rate-limit headers. The Haiku response reported five-hour and seven-day status `allowed`, five-hour utilization `0.02`, overall status `allowed`, and overage status `rejected` / `out_of_credits`.
- Subport's main Claude account became unhealthy after 429s. Later local 503 responses therefore do not prove rejection by each requested upstream model. Another configured Claude account lacks credentials.
- Neither the standard local `~/.claude/.credentials.json` nor project `.claude_access_token` exists. Local Claude settings contain no environment entries or API key helper. Native Claude Code identity cannot be compared from those files.

## Conclusion and unresolved questions

The saved token is a valid Claude Code OAuth token for an active subscription, and the same token can perform Haiku inference. A global lack of subscription quota, invalid token, or Subport-only routing failure does not fully explain the model-dependent results.

The upstream does not supply enough detail to identify why the other models return 429. Extra usage being exhausted is an observed fact, not proof that it causes these model-specific failures. Do not label the other models unsupported or claim the cause is established yet.

Next independent check: compare a native Claude Code request and its authenticated account/organization with this saved OAuth account, without revealing personal identifiers or credentials. Also compare the native request format with Subport's minimal Messages request.

## Follow-up: room task T-02

Robin reports a successful Sonnet call in the client using the subscription bound to `acct-claude-1`. This is user-reported evidence; the exact client model ID and request have not been inspected.

A controlled direct Messages comparison used `claude-sonnet-4-6`, the same saved token, identical headers, prompt and `max_tokens=16`, changing only `stream` from false to true. Both requests returned HTTP 429 with `rate_limit_error` / `Error`. Changing streaming alone did not fix this observed failure. No production code, authentication identity, or rate-limit policy was changed.

## Follow-up: room task T-03

- Robin supplied client session information showing Claude Code 2.1.266, model `claude-sonnet-5`, Pro. A fresh profile lookup compared the account email privately and confirmed equality; no email is retained here. Robin also reports successful Sonnet use on claude.ai.
- The DB credential was updated at `2026-09-13T23:05:46.2054496Z` and has an expiry of `2026-09-14T07:05:45Z`. Both access and refresh tokens are present. At the time of this investigation, the recorded access expiry is in the future. This rules out simply treating September 11 refresh logs as current credential status; it does not independently prove refresh validity.
- The authorization code explicitly requests `user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload` in `internal/gateway/claude_oauth.go`. There is no missing `user:inference` in that requested scope string.
- `claudeTokenResponse` parses only access token, refresh token, expiry and token type; it does not retain granted scopes or organization/account metadata. The exchange handler in `internal/httpapi/claude_oauth_admin.go` saves token/expiry fields but no corresponding authorization metadata. The DB `extra_json` for this account is empty.
- Consequently, actual granted scopes and original grant provenance cannot be reconstructed from the current DB row. This is a diagnostic metadata gap, not evidence that missing scopes caused the 429. Successful Haiku inference further argues against total absence of inference permission.

Remaining useful checks: obtain the provider's explanation using failing request IDs; compare the native client's supported authorization/session context with the saved grant. Reauthorization is a possible diagnostic step, not an established fix, and should use the supported interactive authorization flow rather than fabricate client identity. No secrets were printed and no credential was refreshed or replaced during this audit.

## Follow-up: room task T-04

The user asked to check the existing website session. The configured `SUBPORT_CLAUDE_SESSION` / `SUBPORT_CLAUDE_SESSION_FILE` fallback to project `.claude_session` yielded no saved session credential in this process/workspace (`saved_session_present False`). Therefore no Cookie-authenticated organizations request was made. This is a missing local credential, not an upstream refusal or proof that website-session requests cannot work. The user should not paste secrets into the shared room; a supported local login is required for further website-session inspection.
