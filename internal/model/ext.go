package model

import "strings"

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

// PublicCredentialStatus is the admin-safe view of stored credentials.
// Tokens are never included — only presence flags and non-secret extra JSON.
func PublicCredentialStatus(c AccountCredential) map[string]any {
	return map[string]any{
		"has_access_token":  strings.TrimSpace(c.AccessToken) != "",
		"has_refresh_token": strings.TrimSpace(c.RefreshToken) != "",
		"extra":             c.ExtraJSON,
		"updated_at":        c.UpdatedAt,
		"expires_at":        c.ExpiresAt,
	}
}

// Proxy is an optional egress proxy for upstream calls.
type Proxy struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"` // http | socks5 | ...
	URL       string `json:"url"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
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
