package gateway

import "testing"

func TestCredentialPresenceForORRuntime(t *testing.T) {
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

	codexSetRuntime("rt-access", "rt-refresh", "acc-xyz", 3600)

	p := CredentialPresenceFor("acct-presence-test", "codex")
	if !p.HasAccessToken || !p.HasRefreshToken {
		t.Fatalf("runtime presence: %+v", p)
	}
	if p.ExpiresAt == "" {
		t.Fatal("expected runtime expires_at")
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
}
