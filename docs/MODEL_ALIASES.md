# Model aliases & fallbacks

Lightweight config that maps client-facing model names to canonical upstream
ids, and optionally retries one or two fallback models on **429 / 5xx** using
the same account-pool / cooldown rules.

## Format

JSON object (file `model_aliases.json` next to the binary, env
`SUBPORT_MODEL_ALIASES_FILE`, or admin API / `app_settings.model_aliases`):

```json
{
  "aliases": {
    "sonnet": "claude-sonnet-4-5-20250929",
    "haiku": "claude-haiku-4-5-20251001"
  },
  "fallbacks": {
    "claude-sonnet-4-5-20250929": ["claude-haiku-4-5-20251001"]
  },
  "max_fallback_tries": 2
}
```

- **aliases**: client name → canonical model id (resolved before routing).
- **fallbacks**: canonical id → ordered list of alternatives. On upstream
  `429` / `5xx` (and mapped `rate_limit` / `exceeded_limit`), Subport tries the
  next model once/twice with the same scheduler `Pick` rules.
- **max_fallback_tries**: cap on extra models after the canonical (default `2`).

Load order at boot: DB `app_settings` row `model_aliases`, then optional file
override.

## API (admin auth)

- `GET /api/model-aliases` — current effective config (+ raw `stored` string).
- `PUT /api/model-aliases` — replace config; persists to DB and process memory.

## Notes

- Fallbacks do **not** cross providers; model routes still decide which account
  pool is eligible.
- Account cooldown / rotate (NeoLab-style 429 soft cooldown) applies unchanged
  across fallback attempts.
