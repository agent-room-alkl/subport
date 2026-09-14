package model

import (
	"encoding/json"
	"strings"
)

// AccountCredential holds OAuth/API tokens for one upstream account.
// Secrets must never be logged or returned by public APIs.
type AccountCredential struct {
	AccountID    string `json:"account_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExtraJSON    string `json:"extra_json"`
	ExpiresAt    string `json:"expires_at"`
	UpdatedAt    string `json:"updated_at"`
}

// ExtraCookieKey is the account_credentials.extra_json key for a full browser
// Cookie header string (claude.ai DevTools paste). Treat as a secret.
const ExtraCookieKey = "cookie"

// ClaudeCookieIdentity is safe account metadata resolved from claude.ai with
// the account's own Cookie. It is stored next to (but never inside) public
// credential secrets so admins can detect a Cookie pasted into the wrong row.
type ClaudeCookieIdentity struct {
	Email      string `json:"email,omitempty"`
	Name       string `json:"name,omitempty"`
	OrgUUID    string `json:"org_uuid,omitempty"`
	OrgName    string `json:"org_name,omitempty"`
	Plan       string `json:"plan,omitempty"`
	VerifiedAt string `json:"verified_at,omitempty"`
}

var claudeIdentityExtraKeys = []string{
	"claude_identity_email", "claude_identity_name", "claude_identity_org_uuid",
	"claude_identity_org_name", "claude_identity_plan", "claude_identity_verified_at",
}

// MergeClaudeCookieIdentity replaces previously verified Claude identity
// metadata while preserving credentials and unrelated provider metadata.
func MergeClaudeCookieIdentity(extraJSON string, identity ClaudeCookieIdentity) string {
	extra := map[string]any{}
	if strings.TrimSpace(extraJSON) != "" {
		_ = json.Unmarshal([]byte(extraJSON), &extra)
	}
	for _, k := range claudeIdentityExtraKeys {
		delete(extra, k)
	}
	if identity.Email != "" {
		extra["claude_identity_email"] = strings.TrimSpace(identity.Email)
	}
	if identity.Name != "" {
		extra["claude_identity_name"] = strings.TrimSpace(identity.Name)
	}
	if identity.OrgUUID != "" {
		extra["claude_identity_org_uuid"] = strings.TrimSpace(identity.OrgUUID)
	}
	if identity.OrgName != "" {
		extra["claude_identity_org_name"] = strings.TrimSpace(identity.OrgName)
	}
	if identity.Plan != "" {
		extra["claude_identity_plan"] = strings.TrimSpace(identity.Plan)
	}
	if identity.VerifiedAt != "" {
		extra["claude_identity_verified_at"] = strings.TrimSpace(identity.VerifiedAt)
	}
	b, err := json.Marshal(extra)
	if err != nil {
		return extraJSON
	}
	return string(b)
}

// ClaudeCookieIdentityFromExtraJSON returns only safe persisted identity data.
func ClaudeCookieIdentityFromExtraJSON(extraJSON string) ClaudeCookieIdentity {
	var extra map[string]any
	if json.Unmarshal([]byte(extraJSON), &extra) != nil {
		return ClaudeCookieIdentity{}
	}
	read := func(key string) string {
		v, _ := extra[key].(string)
		return strings.TrimSpace(v)
	}
	return ClaudeCookieIdentity{
		Email: read("claude_identity_email"), Name: read("claude_identity_name"),
		OrgUUID: read("claude_identity_org_uuid"), OrgName: read("claude_identity_org_name"),
		Plan: read("claude_identity_plan"), VerifiedAt: read("claude_identity_verified_at"),
	}
}

// CookieFromExtraJSON returns the trimmed cookie string stored under ExtraCookieKey.
// Empty when missing or unparseable. Never log the return value.
func CookieFromExtraJSON(extraJSON string) string {
	extraJSON = strings.TrimSpace(extraJSON)
	if extraJSON == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(extraJSON), &m) != nil {
		return ""
	}
	for _, k := range []string{ExtraCookieKey, "Cookie", "cookies"} {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// SessionKeyFromCookieHeader returns the sessionKey value from a pasted Cookie
// request header. It is used only for account-isolation checks and must never
// be logged or returned by an API.
func SessionKeyFromCookieHeader(cookieHeader string) string {
	for _, part := range strings.Split(cookieHeader, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(strings.TrimSpace(name), "sessionKey") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// CredentialHasCookie reports whether a non-empty cookie is stored for this credential.
func CredentialHasCookie(c AccountCredential) bool {
	return CookieFromExtraJSON(c.ExtraJSON) != ""
}

// MergeCookieIntoExtraJSON sets or clears the cookie key while preserving other extra fields.
// clear=true removes the cookie key. Returns the new extra_json string.
func MergeCookieIntoExtraJSON(extraJSON, cookie string, clear bool) string {
	extra := map[string]any{}
	if strings.TrimSpace(extraJSON) != "" {
		_ = json.Unmarshal([]byte(extraJSON), &extra)
	}
	if clear || strings.TrimSpace(cookie) == "" {
		delete(extra, ExtraCookieKey)
		delete(extra, "Cookie")
		delete(extra, "cookies")
	} else {
		extra[ExtraCookieKey] = strings.TrimSpace(cookie)
	}
	if len(extra) == 0 {
		return ""
	}
	b, err := json.Marshal(extra)
	if err != nil {
		return extraJSON
	}
	return string(b)
}

// IsSecretExtraKey reports whether an extra_json key must be stripped from admin APIs.
func IsSecretExtraKey(k string) bool {
	lk := strings.ToLower(strings.TrimSpace(k))
	if lk == "" {
		return true
	}
	if strings.Contains(lk, "token") || strings.Contains(lk, "secret") || strings.Contains(lk, "password") {
		return true
	}
	if strings.Contains(lk, "cookie") || lk == "session_key" || lk == "sessionkey" {
		return true
	}
	return false
}

// PublicCredentialStatus is the admin-safe view of stored credentials.
// Tokens and cookies are never included — only presence flags and non-secret extra JSON.
func PublicCredentialStatus(c AccountCredential) map[string]any {
	// Never embed raw ExtraJSON here — it may contain cookie/session secrets.
	// store.PublicCredentialStatus overlays a filtered "extra" map.
	safeExtra := map[string]any{}
	if strings.TrimSpace(c.ExtraJSON) != "" {
		var obj map[string]any
		if json.Unmarshal([]byte(c.ExtraJSON), &obj) == nil {
			for k, v := range obj {
				if IsSecretExtraKey(k) {
					continue
				}
				safeExtra[k] = v
			}
		}
	}
	return map[string]any{
		"has_access_token":  strings.TrimSpace(c.AccessToken) != "",
		"has_refresh_token": strings.TrimSpace(c.RefreshToken) != "",
		"has_cookie":        CredentialHasCookie(c),
		"extra":             safeExtra,
		"updated_at":        c.UpdatedAt,
		"expires_at":        c.ExpiresAt,
	}
}

// Channel groups accounts under a named routing bucket.
type Channel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	GroupName  string `json:"group_name"`
	Priority   int    `json:"priority"`
	Enabled    bool   `json:"enabled"`
	ModelsJSON string `json:"models_json"`
	CreatedAt  string `json:"created_at"`
}

// ChannelAccount maps an account into a channel with optional model pattern.
type ChannelAccount struct {
	ChannelID    string `json:"channel_id"`
	AccountID    string `json:"account_id"`
	ModelPattern string `json:"model_pattern"`
	Priority     int    `json:"priority"`
}

// ModelRoute maps a model-name regex to an upstream provider.
type ModelRoute struct {
	ID       string `json:"id"`
	Pattern  string `json:"pattern"`
	Provider string `json:"provider"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

// AvailableModel is a curated, admin-managed model shown in the user console.
type AvailableModel struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Label     string `json:"label"`
	Enabled   bool   `json:"enabled"`
	SortOrder int    `json:"sort_order"`
	Notes     string `json:"notes"`
}
