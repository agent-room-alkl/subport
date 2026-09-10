package gateway

import (
	"net/url"
	"strings"
	"testing"
)

func TestParseAntigravityCallbackURL(t *testing.T) {
	code, state, err := ParseAntigravityCallbackURL("http://localhost:8085/callback?code=abc%2Fdef&state=xyz")
	if err != nil {
		t.Fatal(err)
	}
	if code != "abc/def" || state != "xyz" {
		t.Fatalf("got code=%q state=%q", code, state)
	}
	_, _, err = ParseAntigravityCallbackURL("http://localhost:8085/callback?error=access_denied")
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("expected oauth error, got %v", err)
	}
}

func TestStartAntigravityOAuthBuildsPKCEURL(t *testing.T) {
	AntigravityOAuthPersist = nil
	res, err := StartAntigravityOAuth("acct-antigravity-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID == "" || res.State == "" || res.AuthURL == "" {
		t.Fatalf("incomplete start result: %+v", res)
	}
	u, err := url.Parse(res.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "accounts.google.com" {
		t.Fatalf("host = %s", u.Host)
	}
	q := u.Query()
	wantID := antigravityOAuthClientID()
	if wantID == "" {
		t.Skip("set SUBPORT_ANTIGRAVITY_CLIENT_ID for this test")
	}
	if q.Get("client_id") != wantID {
		t.Fatalf("client_id mismatch")
	}
	if q.Get("redirect_uri") != antigravityOAuthRedirectURI {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("missing PKCE challenge")
	}
	if q.Get("state") != res.State {
		t.Fatalf("state mismatch")
	}
	if q.Get("access_type") != "offline" || q.Get("prompt") != "consent" {
		t.Fatalf("offline/consent missing")
	}
	if AntigravityOAuthSessionAccount(res.SessionID) != "acct-antigravity-1" {
		t.Fatalf("session account not bound")
	}
}

func TestExchangeAntigravityOAuthRejectsBadSession(t *testing.T) {
	_, err := ExchangeAntigravityOAuth("missing", "x", "y", "")
	if err == nil {
		t.Fatal("expected error")
	}
}