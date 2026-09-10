package httpapi

import "testing"

func TestParseAccountAntigravityOAuthPath(t *testing.T) {
	id, ok := parseAccountAntigravityOAuthPath("accounts/acct-antigravity-1/antigravity/oauth/start", "start")
	if !ok || id != "acct-antigravity-1" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
	if _, ok := parseAccountAntigravityOAuthPath("accounts/acct-antigravity-1/credentials", "start"); ok {
		t.Fatal("should not match credentials")
	}
	if _, ok := parseAccountAntigravityOAuthPath("accounts/a/b/antigravity/oauth/start", "start"); ok {
		t.Fatal("should reject nested id")
	}
}