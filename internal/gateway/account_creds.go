package gateway

import (
	"encoding/json"
	"sync"
)

// Account-scoped credential cache. Load order for providers:
//   1. DB-backed cache (hydrated at bootstrap / admin upsert)
//   2. In-memory OAuth runtime (claudeRuntime / codexRuntime / antigravityRuntime)
//   3. Env vars / token files
//
// Tokens are never logged.

type accountTokenPair struct {
	AccessToken  string
	RefreshToken string
	ExtraJSON    string
	ExpiresAt    string
}

var (
	accountCredMu sync.RWMutex
	accountCreds  = map[string]accountTokenPair{}
)

// SetAccountCredential stores tokens for an account id in the process cache.
func SetAccountCredential(accountID, access, refresh, extraJSON, expiresAt string) {
	accountID = trimSpace(accountID)
	if accountID == "" {
		return
	}
	accountCredMu.Lock()
	defer accountCredMu.Unlock()
	accountCreds[accountID] = accountTokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExtraJSON:    extraJSON,
		ExpiresAt:    expiresAt,
	}
}

// ClearAccountCredential removes a cached account credential.
func ClearAccountCredential(accountID string) {
	accountCredMu.Lock()
	defer accountCredMu.Unlock()
	delete(accountCreds, accountID)
}

func accountAccessToken(accountID string) string {
	accountCredMu.RLock()
	defer accountCredMu.RUnlock()
	return accountCreds[accountID].AccessToken
}

func accountRefreshToken(accountID string) string {
	accountCredMu.RLock()
	defer accountCredMu.RUnlock()
	return accountCreds[accountID].RefreshToken
}

func accountExtraJSON(accountID string) string {
	accountCredMu.RLock()
	defer accountCredMu.RUnlock()
	return accountCreds[accountID].ExtraJSON
}

func accountExpiresAt(accountID string) string {
	accountCredMu.RLock()
	defer accountCredMu.RUnlock()
	return accountCreds[accountID].ExpiresAt
}

// AccountCredentialSnapshot is a non-secret view used by the refresh ticker
// to decide which accounts need work. Callers must not log tokens.
type AccountCredentialSnapshot struct {
	AccountID       string
	HasAccessToken  bool
	HasRefreshToken bool
	ExpiresAt       string
	ExtraJSON       string
}

// ListCachedCredentialSnapshots returns cache entries that have a refresh token.
func ListCachedCredentialSnapshots() []AccountCredentialSnapshot {
	accountCredMu.RLock()
	defer accountCredMu.RUnlock()
	out := make([]AccountCredentialSnapshot, 0, len(accountCreds))
	for id, p := range accountCreds {
		if trimSpace(p.RefreshToken) == "" {
			continue
		}
		out = append(out, AccountCredentialSnapshot{
			AccountID:       id,
			HasAccessToken:  trimSpace(p.AccessToken) != "",
			HasRefreshToken: true,
			ExpiresAt:       p.ExpiresAt,
			ExtraJSON:       p.ExtraJSON,
		})
	}
	return out
}

// CachedRefreshToken returns the refresh token for an account (for refresh
// workers only). Never log the return value.
func CachedRefreshToken(accountID string) string {
	return accountRefreshToken(accountID)
}

// CredentialPresence is a non-secret view of whether tokens are currently
// loaded for an account. Used by admin list APIs; never includes token values.
type CredentialPresence struct {
	HasAccessToken  bool
	HasRefreshToken bool
	ExpiresAt       string // RFC3339 when known; empty otherwise
}

// CredentialPresenceFor reports whether the gateway currently has access and/or
// refresh tokens for accountID: account credential cache, then (for claude/codex)
// the provider runtime / env / token files that live traffic uses (claude/codex/
// antigravity). Secrets are never returned.
func CredentialPresenceFor(accountID, provider string) CredentialPresence {
	var p CredentialPresence
	accountID = trimSpace(accountID)
	if accountID != "" {
		accountCredMu.RLock()
		pair, ok := accountCreds[accountID]
		accountCredMu.RUnlock()
		if ok {
			p.HasAccessToken = trimSpace(pair.AccessToken) != ""
			p.HasRefreshToken = trimSpace(pair.RefreshToken) != ""
			p.ExpiresAt = pair.ExpiresAt
		}
	}
	switch provider {
	case "claude":
		if !p.HasAccessToken && HasClaudeCredential() {
			p.HasAccessToken = true
		}
		if !p.HasRefreshToken && trimSpace(claudeRefreshToken()) != "" {
			p.HasRefreshToken = true
		}
		if trimSpace(p.ExpiresAt) == "" {
			p.ExpiresAt = claudeRuntimeExpiresRFC3339()
		}
	case "codex":
		if !p.HasAccessToken && HasCodexCredential() {
			p.HasAccessToken = true
		}
		if !p.HasRefreshToken && trimSpace(codexRefreshToken()) != "" {
			p.HasRefreshToken = true
		}
		if trimSpace(p.ExpiresAt) == "" {
			p.ExpiresAt = codexRuntimeExpiresRFC3339()
		}
	case "antigravity":
		if !p.HasAccessToken && HasAntigravityCredential() {
			p.HasAccessToken = true
		}
		if !p.HasRefreshToken && trimSpace(antigravityRefreshToken()) != "" {
			p.HasRefreshToken = true
		}
		if trimSpace(p.ExpiresAt) == "" {
			p.ExpiresAt = antigravityRuntimeExpiresRFC3339()
		}
	}
	return p
}

// HydrateRuntimeFromAccount copies a cached account credential into the
// provider runtime (claude or codex) so Ensure*/Call paths see DB tokens.
func HydrateRuntimeFromAccount(accountID, provider string) {
	accountCredMu.RLock()
	pair, ok := accountCreds[accountID]
	accountCredMu.RUnlock()
	if !ok || pair.AccessToken == "" && pair.RefreshToken == "" {
		return
	}
	switch provider {
	case "claude":
		claudeSetRuntime(pair.AccessToken, pair.RefreshToken, 0)
	case "codex":
		accountIDExtra := chatgptAccountIDFromExtra(pair.ExtraJSON)
		codexSetRuntime(pair.AccessToken, pair.RefreshToken, accountIDExtra, 0)
	case "antigravity":
		projectID := antigravityProjectIDFromExtra(pair.ExtraJSON)
		antigravitySetRuntime(pair.AccessToken, pair.RefreshToken, projectID, 0)
	}
}

func chatgptAccountIDFromExtra(extraJSON string) string {
	if extraJSON == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(extraJSON), &m) != nil {
		return ""
	}
	for _, k := range []string{"chatgpt_account_id", "account_id", "chatgptAccountId"} {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// RuntimeCredentialMaterial returns access/refresh/extra/expires currently
// available from provider runtime or env/files. Used only to sync bootstrap
// tokens into account_credentials. Caller must never log the return values.
func RuntimeCredentialMaterial(provider string) (access, refresh, extra, expiresAt string, ok bool) {
	switch provider {
	case "claude":
		access = claudeCredential()
		refresh = claudeRefreshToken()
		expiresAt = claudeRuntimeExpiresRFC3339()
	case "codex":
		access = codexCredential()
		refresh = codexRefreshToken()
		expiresAt = codexRuntimeExpiresRFC3339()
		if id := loadCodexAccountID(); id != "" {
			b, _ := json.Marshal(map[string]string{"chatgpt_account_id": id})
			extra = string(b)
		}
	case "antigravity":
		access = antigravityCredential()
		refresh = antigravityRefreshToken()
		expiresAt = antigravityRuntimeExpiresRFC3339()
		if id := loadAntigravityProjectID(); id != "" {
			b, _ := json.Marshal(map[string]string{"project_id": id})
			extra = string(b)
		}
	default:
		return "", "", "", "", false
	}
	if trimSpace(access) == "" && trimSpace(refresh) == "" {
		return "", "", "", "", false
	}
	return access, refresh, extra, expiresAt, true
}

