# Claude exchange runtime review

Observed 2026-09-11 (local time, UTC+12).

## Runtime

- Running PID 36592: `D:\workspace\subport\subport.exe`, started 16:23:04.
- Executable last written 16:22:52; relevant Go gateway and HTTP handler files last written 16:21:06.
- PID owns listening ports 8080 and 8085.
- Python helper last written 16:37:09. The gateway starts a fresh Python subprocess for each helper exchange (`claude_oauth.go:523`), so this script update does not require restarting the Go process.
- The 16:38:01 log includes the newly added response classification, proving that diagnostics ran for exchange `caf46844`.

## Verified failure

`_iso_boot.err.log:50-54` records organizations HTTP 200, followed by authorize HTTP 403. The authorize response parsed as JSON, with `permission_error` and the message `Claude Code requires a Pro or Max subscription.`

The unchanged UI message is explained by the helper still mapping authorize 401/403 to `session_stale_relogin`, and the HTTP handler translating that category to a request to copy the session cookie again. This diagnostic change did not fix that classification.

## Conclusion and limits

The latest diagnostics are active; this failure is not explained by an omitted restart for the Python update. The upstream response reports a subscription/permission requirement for this authorization attempt. This does not independently establish the user's paid-plan status or whether the correct organization was selected.

Next implementation step: map this recognized permission response to a distinct error category and accurate UI message, preserve HTTP status and exchange ID, and test it separately from authentication and challenge failures. If the user already has the required entitlement, inspect the selected organization and entitlement context rather than repeatedly copying the cookie.

Read-only verification used process/file timestamps, listening socket ownership, local source inspection, and the correlated log. No credentials were read or submitted, no process was restarted, and no application source was changed by this review.
