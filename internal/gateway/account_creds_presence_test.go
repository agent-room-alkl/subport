package gateway

import "testing"

func TestCredentialPresenceForAccountCacheOnly(t *testing.T) {
	ClearAccountCredential("acct-presence-test")

	codexCredMu.Lock()
	prev := codexRuntime
	codexRuntime = codexTokenPair{}
	codexCredMu.Unlock()
	defer func() {
		codexCredMu.Lock()
		codexRuntime = prev
		codexCredMu.Unlock()
		ClearAccountCredential("acct-presence-test")
	}()

	// Global/runtime credentials must NOT make an empty account row look authorized.
	codexSetRuntime("rt-access", "rt-refresh", "acc-xyz", 3600)

	p := CredentialPresenceFor("acct-presence-test", "codex")
	if p.HasAccessToken || p.HasRefreshToken {
		t.Fatalf("empty account must not inherit runtime: %+v", p)
	}
	if HasAccountAccessToken("acct-presence-test") {
		t.Fatal("HasAccountAccessToken should be false for empty cache")
	}

	p = CredentialPresenceFor("acct-mock-1", "mock")
	if p.HasAccessToken || p.HasRefreshToken {
		t.Fatalf("mock must not see codex runtime: %+v", p)
	}

	codexCredMu.Lock()
	codexRuntime = codexTokenPair{}
	codexCredMu.Unlock()

	SetAccountCredential("acct-presence-test", "db-access", "", "", "2099-01-01T00:00:00Z")
	p = CredentialPresenceFor("acct-presence-test", "codex")
	if !p.HasAccessToken {
		t.Fatalf("expected cache access token flag, got %+v", p)
	}
	if p.ExpiresAt != "2099-01-01T00:00:00Z" {
		t.Fatalf("expires_at=%q want DB value", p.ExpiresAt)
	}
	if !HasAccountAccessToken("acct-presence-test") {
		t.Fatal("HasAccountAccessToken should be true when cache has access")
	}
}

func TestClaudeCredentialForNoGlobalFallback(t *testing.T) {
	ClearAccountCredential("acct-iso-claude")
	defer ClearAccountCredential("acct-iso-claude")

	claudeCredMu.Lock()
	prev := claudeRuntime
	claudeRuntime = claudeTokenPair{AccessToken: "neighbor-access", RefreshToken: "neighbor-rt"}
	claudeCredMu.Unlock()
	defer func() {
		claudeCredMu.Lock()
		claudeRuntime = prev
		claudeCredMu.Unlock()
	}()

	if got := claudeCredentialFor("acct-iso-claude"); got != "" {
		t.Fatalf("non-empty accountID must not fall back to runtime, got %q", got)
	}
	SetAccountCredential("acct-iso-claude", "own-access", "own-rt", "", "")
	if got := claudeCredentialFor("acct-iso-claude"); got != "own-access" {
		t.Fatalf("want own-access, got %q", got)
	}
	// Legacy empty accountID may still use global.
	if got := claudeCredentialFor(""); got != "neighbor-access" {
		t.Fatalf("empty accountID legacy path want neighbor-access, got %q", got)
	}
}
