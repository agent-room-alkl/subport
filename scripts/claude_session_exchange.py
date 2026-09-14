#!/usr/bin/env python3
"""One-shot Claude sessionKey -> OAuth tokens via curl_cffi (chrome131).

stdin JSON:  {"session_key": "...", "org_uuid": "optional"}
stdout JSON: {"ok": true, "access_token": "...", "refresh_token": "...", "expires_in": N}
          or {"ok": false, "error": "...", "code": "session_stale_relogin|subscription_required|authorization_denied|cloudflare|rate_limited|exchange_failed"}

Never prints secrets. Status lines (lengths only) go to stderr.
"""
from __future__ import annotations

import base64
import hashlib
import json
import secrets
import sys
import time
from urllib.parse import parse_qs, urlparse

from curl_cffi import requests as cffi_requests

CLIENT_ID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
TOKEN_URL = "https://platform.claude.com/v1/oauth/token"
REDIRECT_URI = "https://platform.claude.com/oauth/code/callback"
SCOPE = "user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
CLAUDE_AI = "https://claude.ai"
IMPERSONATE = "chrome131"


def b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


def pkce() -> tuple[str, str, str]:
    verifier = b64url(secrets.token_bytes(32))
    challenge = b64url(hashlib.sha256(verifier.encode("ascii")).digest())
    state = b64url(secrets.token_bytes(32))
    return verifier, challenge, state


def browser_headers(cookie_header: str = "") -> dict:
    headers = {
        "Accept": "application/json, text/plain, */*",
        "Accept-Language": "en-US,en;q=0.9",
        "Cache-Control": "no-cache",
        "Origin": "https://claude.ai",
        "Referer": "https://claude.ai/new",
    }
    if cookie_header:
        headers["Cookie"] = cookie_header
    return headers



def classify_body(status: int, text: str, content_type: str = "") -> dict:
    """Desensitized response class — never returns raw body or secrets."""
    ct = (content_type or "").split(";")[0].strip().lower()
    low = (text or "").lower()
    kind = "other"
    if "application/json" in ct or (text or "").lstrip().startswith(("{", "[")):
        kind = "json"
    elif "text/html" in ct or "<html" in low or "just a moment" in low:
        kind = "html"
    err_type = ""
    err_code = ""
    err_msg = ""
    if kind == "json":
        try:
            data = json.loads(text or "")
            if isinstance(data, dict):
                err = data.get("error")
                if isinstance(err, dict):
                    err_type = str(err.get("type") or "")[:80]
                    err_code = str(err.get("code") or "")[:80]
                    err_msg = str(err.get("message") or "")[:120]
                elif isinstance(err, str):
                    err_msg = err[:120]
                else:
                    err_type = str(data.get("type") or "")[:80]
                    err_code = str(data.get("code") or "")[:80]
                    err_msg = str(data.get("message") or data.get("detail") or "")[:120]
        except Exception:
            kind = "json_unparseable"
    # scrub accidental secrets in message
    for bad in ("sk-ant", "sessionkey", "access_token", "refresh_token"):
        if bad in err_msg.lower():
            err_msg = "[redacted]"
            break
    return {
        "content_type": ct or "unknown",
        "body_kind": kind,
        "body_len": len(text or ""),
        "error_type": err_type,
        "error_code": err_code,
        "error_message": err_msg,
        "cf": is_cf_block(status, text or ""),
    }


def log_class(step: str, status: int, text: str, content_type: str = "") -> dict:
    info = classify_body(status, text, content_type)
    print(
        f"{step} class status={status} ct={info['content_type']} kind={info['body_kind']} "
        f"len={info['body_len']} cf={info['cf']} err_type={info['error_type']!r} "
        f"err_code={info['error_code']!r} err_msg={info['error_message']!r}",
        file=sys.stderr,
    )
    return info


def is_cf_block(status: int, text: str) -> bool:
    low = (text or "").lower()
    if status == 403 and ("just a moment" in low or "cf-mitigated" in low or "cloudflare" in low):
        return True
    return status == 403 and "Just a moment" in (text or "")


def fail(code: str, msg: str, exit_code: int = 2, *, step: str = "", upstream_status: int = 0) -> None:
    payload = {"ok": False, "error": msg, "code": code}
    if step:
        payload["step"] = step
    if upstream_status:
        payload["upstream_status"] = int(upstream_status)
    sys.stdout.write(json.dumps(payload, ensure_ascii=False))
    sys.stdout.flush()
    raise SystemExit(exit_code)


def fetch_org(session: cffi_requests.Session, session_key: str, cookie_header: str = "") -> str:
    r = session.get(
        f"{CLAUDE_AI}/api/organizations",
        headers=browser_headers(cookie_header),
        cookies=None if cookie_header else {"sessionKey": session_key},
        timeout=60,
    )
    text = r.text or ""
    print(
        f"organizations status={r.status_code} body_len={len(text)} cf={is_cf_block(r.status_code, text)}",
        file=sys.stderr,
    )
    ct = r.headers.get("content-type") or r.headers.get("Content-Type") or ""
    log_class("organizations", r.status_code, text, ct)
    if is_cf_block(r.status_code, text):
        fail("cloudflare", "organizations blocked by Cloudflare", step="organizations", upstream_status=r.status_code)
    if r.status_code in (401, 403):
        fail("session_stale_relogin", f"organizations status {r.status_code}: sessionKey rejected by API", step="organizations", upstream_status=r.status_code)
    if r.status_code == 429:
        fail("rate_limited", "organizations rate limited (429)", step="organizations", upstream_status=429)
    if r.status_code >= 300:
        fail("exchange_failed", f"organizations status {r.status_code}", step="organizations", upstream_status=r.status_code)
    orgs = r.json()
    if not isinstance(orgs, list) or not orgs:
        fail("session_stale_relogin", "no organizations found for session", step="organizations", upstream_status=200)
    for o in orgs:
        if o.get("raven_type") == "team":
            return o["uuid"]
    return orgs[0]["uuid"]


def authorize(session: cffi_requests.Session, session_key: str, org: str, challenge: str, state: str, cookie_header: str = "") -> str:
    url = f"{CLAUDE_AI}/v1/oauth/{org}/authorize"
    payload = {
        "response_type": "code",
        "client_id": CLIENT_ID,
        "organization_uuid": org,
        "redirect_uri": REDIRECT_URI,
        "scope": SCOPE,
        "state": state,
        "code_challenge": challenge,
        "code_challenge_method": "S256",
    }
    headers = {**browser_headers(cookie_header), "Content-Type": "application/json", "Accept": "application/json"}
    r = session.post(
        url,
        json=payload,
        headers=headers,
        cookies=None if cookie_header else {"sessionKey": session_key},
        timeout=60,
    )
    text = r.text or ""
    ct = r.headers.get("content-type") or r.headers.get("Content-Type") or ""
    print(
        f"authorize status={r.status_code} body_len={len(text)} cf={is_cf_block(r.status_code, text)}",
        file=sys.stderr,
    )
    info = log_class("authorize", r.status_code, text, ct)
    if is_cf_block(r.status_code, text) or info.get("cf"):
        fail("cloudflare", "authorize blocked by Cloudflare", step="authorize", upstream_status=r.status_code)
    if r.status_code in (401, 403):
        detail = info.get("error_type") or info.get("error_code") or info.get("body_kind") or "unknown"
        err_msg = (info.get("error_message") or "").lower()
        err_type = (info.get("error_type") or "").lower()
        # Narrow: only explicit Pro/Max subscription wording → subscription_required.
        # Generic permission_error (e.g. org access disabled) must NOT be labeled as missing Pro/Max.
        if ("pro or max" in err_msg) or ("requires a pro" in err_msg and "subscription" in err_msg):
            fail(
                "subscription_required",
                f"authorize status {r.status_code} class=subscription_required ct={info.get('content_type')}",
                step="authorize",
                upstream_status=r.status_code,
            )
        if err_type == "permission_error":
            fail(
                "authorization_denied",
                f"authorize status {r.status_code} class=permission_error ct={info.get('content_type')}",
                step="authorize",
                upstream_status=r.status_code,
            )
        fail(
            "session_stale_relogin",
            f"authorize status {r.status_code} class={detail} ct={info.get('content_type')}",
            step="authorize",
            upstream_status=r.status_code,
        )
    if r.status_code == 429:
        fail("rate_limited", "authorize rate limited (429)", step="authorize", upstream_status=429)
    if r.status_code >= 300:
        fail("exchange_failed", f"authorize status {r.status_code}", step="authorize", upstream_status=r.status_code)
    data = r.json()
    redirect = data.get("redirect_uri") or ""
    if not redirect:
        fail("exchange_failed", "authorize: empty redirect_uri")
    u = urlparse(redirect)
    qs = parse_qs(u.query)
    code = (qs.get("code") or [""])[0]
    resp_state = (qs.get("state") or [""])[0]
    if not code:
        fail("exchange_failed", "authorize: no code in redirect_uri")
    if resp_state:
        return f"{code}#{resp_state}"
    return code


def exchange_code(session: cffi_requests.Session, code_with_state: str, verifier: str) -> dict:
    auth_code = code_with_state
    code_state = ""
    if "#" in code_with_state:
        auth_code, code_state = code_with_state.split("#", 1)
    payload = {
        "code": auth_code,
        "grant_type": "authorization_code",
        "client_id": CLIENT_ID,
        "redirect_uri": REDIRECT_URI,
        "code_verifier": verifier,
    }
    if code_state:
        payload["state"] = code_state
    headers = {
        "Accept": "application/json, text/plain, */*",
        "Content-Type": "application/json",
        "User-Agent": "axios/1.13.6",
    }

    def do_post():
        return session.post(TOKEN_URL, json=payload, headers=headers, timeout=60)

    r = do_post()
    text = r.text or ""
    print(f"token status={r.status_code} body_len={len(text)}", file=sys.stderr)
    if r.status_code == 429:
        ra = r.headers.get("Retry-After") or r.headers.get("retry-after")
        wait_s = 60
        if ra:
            try:
                wait_s = int(float(ra))
            except ValueError:
                wait_s = 60
        wait_s = min(max(wait_s, 1), 180)
        print(f"token 429 wait_s={wait_s}", file=sys.stderr)
        time.sleep(wait_s)
        r = do_post()
        text = r.text or ""
        print(f"token retry status={r.status_code} body_len={len(text)}", file=sys.stderr)
        if r.status_code == 429:
            fail("rate_limited", "token endpoint still 429 after Retry-After")
    if r.status_code >= 300:
        fail("exchange_failed", f"token exchange status {r.status_code}")
    tok = r.json()
    if not tok.get("access_token"):
        fail("exchange_failed", "token exchange empty access_token")
    print(
        f"oauth_ok access_len={len(tok.get('access_token') or '')} "
        f"refresh_len={len(tok.get('refresh_token') or '')} expires_in={tok.get('expires_in')}",
        file=sys.stderr,
    )
    return tok


def main() -> None:
    raw = sys.stdin.read()
    try:
        inp = json.loads(raw) if raw.strip() else {}
    except json.JSONDecodeError:
        fail("exchange_failed", "invalid stdin JSON", 1)
    session_key = (inp.get("session_key") or "").strip()
    org_uuid = (inp.get("org_uuid") or "").strip()
    cookie_header = (inp.get("cookie") or "").strip()
    if not session_key:
        fail("exchange_failed", "session_key required", 1)

    # Safe diagnostic only; never emit the Cookie or sessionKey values.
    print(f"cookie_mode={'full' if cookie_header else 'session_only'}", file=sys.stderr)

    session = cffi_requests.Session(impersonate=IMPERSONATE)
    try:
        if not org_uuid:
            org_uuid = fetch_org(session, session_key, cookie_header)
        verifier, challenge, state = pkce()
        code = authorize(session, session_key, org_uuid, challenge, state, cookie_header)
        tok = exchange_code(session, code, verifier)
    except SystemExit:
        raise
    except Exception as e:
        msg = str(e)
        low = msg.lower()
        if "cloudflare" in low or "just a moment" in low:
            fail("cloudflare", "Cloudflare blocked exchange")
        if "429" in low:
            fail("rate_limited", msg)
        fail("exchange_failed", "exchange failed")

    out = {
        "ok": True,
        "access_token": tok.get("access_token") or "",
        "refresh_token": tok.get("refresh_token") or "",
        "expires_in": int(tok.get("expires_in") or 0),
        "token_type": tok.get("token_type") or "Bearer",
    }
    sys.stdout.write(json.dumps(out, ensure_ascii=False))
    sys.stdout.flush()


if __name__ == "__main__":
    main()
