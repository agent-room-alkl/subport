"""
Smoke-test Subport OpenAI-compatible chat completions.

Usage:
  set SUBPORT_API_KEY=sk-sp-...
  python scripts/test_chat.py

Or: python scripts/test_chat.py sk-sp-...
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.request

BASE_URL = os.environ.get("SUBPORT_BASE_URL", "http://127.0.0.1:8080").rstrip("/")

# ---- pick one model (default: GPT / Codex path) ----
MODEL = "gpt-5.5"

# Claude (Haiku recommended for subscription smoke)
# MODEL = "claude-haiku-4-5-20251001"
# MODEL = "claude-sonnet-4-5"

# Codex / OpenAI-family (also matched by ^gpt-|^o[0-9]|^codex)
# MODEL = "gpt-4.1"
# MODEL = "o3"
# MODEL = "codex-mini"

# Antigravity / Gemini
# MODEL = "gemini-2.5-flash"
# MODEL = "gemini-2.5-pro"

PROMPT = "Say hi in one short sentence."


def main() -> int:
    key = (sys.argv[1] if len(sys.argv) > 1 else "") or os.environ.get("SUBPORT_API_KEY", "")
    if not key.startswith("sk-sp-"):
        print("Usage: set SUBPORT_API_KEY=sk-sp-... && python scripts/test_chat.py", file=sys.stderr)
        print("   or: python scripts/test_chat.py sk-sp-...", file=sys.stderr)
        return 2

    url = f"{BASE_URL}/v1/chat/completions"
    body = {
        "model": MODEL,
        "messages": [{"role": "user", "content": PROMPT}],
        "stream": False,
    }
    data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        method="POST",
        headers={
            "Authorization": f"Bearer {key}",
            "Content-Type": "application/json",
        },
    )
    print(f"POST {url}")
    print(f"model={MODEL}")
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            raw = resp.read().decode("utf-8")
            print(f"status={resp.status}")
            print(raw)
    except urllib.error.HTTPError as e:
        err = e.read().decode("utf-8", errors="replace")
        print(f"status={e.code}", file=sys.stderr)
        print(err, file=sys.stderr)
        return 1
    except Exception as e:
        print(f"request failed: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
