package model

import (
	"strings"
	"testing"
)

func TestMergeClaudeCookieIdentityPreservesCookieAndReplacesIdentity(t *testing.T) {
	extra := `{"cookie":"sessionKey=secret","other":"keep","claude_identity_email":"old@example.com"}`
	merged := MergeClaudeCookieIdentity(extra, ClaudeCookieIdentity{
		Email: "new@example.com", OrgUUID: "org-1", VerifiedAt: "2026-09-14T10:00:00Z",
	})
	if CookieFromExtraJSON(merged) != "sessionKey=secret" {
		t.Fatal("cookie was not preserved")
	}
	identity := ClaudeCookieIdentityFromExtraJSON(merged)
	if identity.Email != "new@example.com" || identity.OrgUUID != "org-1" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
	if strings.Contains(merged, "old@example.com") || !strings.Contains(merged, `"other":"keep"`) {
		t.Fatalf("identity replacement damaged extra json: %s", merged)
	}
}

func TestMergeClaudeCookieIdentityClearDoesNotExposeCookie(t *testing.T) {
	extra := `{"cookie":"sessionKey=secret","claude_identity_email":"old@example.com","claude_identity_verified_at":"now"}`
	cleared := MergeClaudeCookieIdentity(extra, ClaudeCookieIdentity{})
	if identity := ClaudeCookieIdentityFromExtraJSON(cleared); identity.VerifiedAt != "" || identity.Email != "" {
		t.Fatalf("identity not cleared: %#v", identity)
	}
	public := PublicCredentialStatus(AccountCredential{ExtraJSON: cleared})
	if blob := public["extra"]; strings.Contains(strings.TrimSpace(toTestString(blob)), "sessionKey") {
		t.Fatalf("public status leaked cookie: %#v", public)
	}
}

func toTestString(v any) string {
	if m, ok := v.(map[string]any); ok {
		out := ""
		for k, value := range m {
			out += k
			if s, ok := value.(string); ok {
				out += s
			}
		}
		return out
	}
	return ""
}
