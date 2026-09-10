package gateway

// Claude subscription OAuth helpers (sessionKey → access_token) and in-memory
// credential hold. Patterns taken from _ref/sub2api's claude_oauth_service and
// pkg/oauth — smallest path only: org lookup, authorize, code exchange, refresh.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

)

const (
	claudeOAuthClientID    = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeOAuthTokenURL    = "https://platform.claude.com/v1/oauth/token"
	claudeOAuthRedirectURI = "https://platform.claude.com/oauth/code/callback"
	claudeOAuthScopeAPI    = "user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
	claudeAIBase           = "https://claude.ai"
)

type claudeTokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

var (
	claudeCredMu  sync.RWMutex
	claudeRuntime claudeTokenPair
)

func claudeRuntimeAccessToken() string {
	claudeCredMu.RLock()
	defer claudeCredMu.RUnlock()
	return claudeRuntime.AccessToken
}

func claudeRuntimeExpiresRFC3339() string {
	claudeCredMu.RLock()
	defer claudeCredMu.RUnlock()
	if claudeRuntime.ExpiresAt.IsZero() {
		return ""
	}
	return claudeRuntime.ExpiresAt.UTC().Format(time.RFC3339)
}

func claudeSetRuntime(access, refresh string, expiresIn int64) {
	claudeCredMu.Lock()
	defer claudeCredMu.Unlock()
	claudeRuntime.AccessToken = access
	if refresh != "" {
		claudeRuntime.RefreshToken = refresh
	}
	if expiresIn > 0 {
		claudeRuntime.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
}

func claudeRefreshToken() string {
	claudeCredMu.RLock()
	rt := claudeRuntime.RefreshToken
	claudeCredMu.RUnlock()
	if rt != "" {
		return rt
	}
	return loadClaudeRefreshToken()
}

type claudeTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// EnsureClaudeTokensFromEnv exchanges a Claude.ai sessionKey for OAuth tokens
// when needed, or refreshes via SUBPORT_CLAUDE_REFRESH_TOKEN. No-op when an
// access token is already available via env. Does not touch the account store.

// HasClaudeCredential reports whether any Claude access token is available
// (env or in-memory from session exchange / refresh).

// loadClaudeSession reads SUBPORT_CLAUDE_SESSION, or the contents of
// SUBPORT_CLAUDE_SESSION_FILE, or "./.claude_session" when present.
// File form lets a local operator inject a cookie without relying on an
// interactive shell environment the tooling can inherit.

// loadClaudeAccessToken reads SUBPORT_CLAUDE_ACCESS_TOKEN / SUBPORT_PROVIDER_KEY_CLAUDE,
// or the contents of ./.claude_access_token when present.
func loadClaudeAccessToken() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_CLAUDE_ACCESS_TOKEN")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("SUBPORT_PROVIDER_KEY_CLAUDE")); v != "" {
		return v
	}
	b, err := os.ReadFile(".claude_access_token")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// loadClaudeRefreshToken reads SUBPORT_CLAUDE_REFRESH_TOKEN or ./.claude_refresh_token.
func loadClaudeRefreshToken() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_CLAUDE_REFRESH_TOKEN")); v != "" {
		return v
	}
	b, err := os.ReadFile(".claude_refresh_token")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func loadClaudeSession() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_CLAUDE_SESSION")); v != "" {
		return v
	}
	path := strings.TrimSpace(os.Getenv("SUBPORT_CLAUDE_SESSION_FILE"))
	if path == "" {
		path = ".claude_session"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func HasClaudeCredential() bool {
	return claudeCredential() != ""
}
func EnsureClaudeTokensFromEnv() error {
	if loadClaudeAccessToken() != "" {
		return nil
	}
	if claudeRuntimeAccessToken() != "" {
		return nil
	}
	// Prefer refresh-token bootstrap over sessionKey exchange: the latter
	// hits claude.ai and is often blocked by Cloudflare from a Go client.
	if loadClaudeRefreshToken() != "" {
		_, err := claudeTryRefresh()
		return err
	}
	session := loadClaudeSession()
	if session == "" {
		return nil
	}
	org := strings.TrimSpace(os.Getenv("SUBPORT_CLAUDE_ORG"))
	pair, err := claudeExchangeSession(session, org)
	if err != nil {
		return fmt.Errorf("claude session exchange: %w", err)
	}
	claudeSetRuntime(pair.AccessToken, pair.RefreshToken, pair.ExpiresIn)
	log.Printf("claude bootstrap: obtained OAuth access token via sessionKey (expires_in=%ds)", pair.ExpiresIn)
	return nil
}


func claudeTryRefresh() (bool, error) {
	rt := claudeRefreshToken()
	if rt == "" {
		return false, fmt.Errorf("no refresh token")
	}
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": rt,
		"client_id":     claudeOAuthClientID,
	})
	req, err := http.NewRequest(http.MethodPost, claudeOAuthTokenURL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return false, fmt.Errorf("refresh failed: status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var tok claudeTokenResponse
	if err := json.Unmarshal(raw, &tok); err != nil {
		return false, err
	}
	if tok.AccessToken == "" {
		return false, fmt.Errorf("refresh returned empty access_token")
	}
	claudeSetRuntime(tok.AccessToken, tok.RefreshToken, tok.ExpiresIn)
	log.Printf("claude: refreshed OAuth access token (expires_in=%ds)", tok.ExpiresIn)
	return true, nil
}

func claudeExchangeSession(sessionKey, orgUUID string) (claudeTokenResponse, error) {
	var zero claudeTokenResponse
	if orgUUID == "" {
		var err error
		orgUUID, err = claudeFetchOrgUUID(sessionKey)
		if err != nil {
			return zero, err
		}
	}

	verifier, err := oauthCodeVerifier()
	if err != nil {
		return zero, err
	}
	challenge := oauthCodeChallengeS256(verifier)
	state, err := oauthRandomState()
	if err != nil {
		return zero, err
	}

	code, err := claudeAuthorize(sessionKey, orgUUID, challenge, state)
	if err != nil {
		return zero, err
	}
	return claudeExchangeCode(code, verifier)
}

func claudeFetchOrgUUID(sessionKey string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, claudeAIBase+"/api/organizations", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Origin", "https://claude.ai")
	req.Header.Set("Referer", "https://claude.ai/new")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.AddCookie(&http.Cookie{Name: "sessionKey", Value: sessionKey})
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("organizations: status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var orgs []struct {
		UUID      string  `json:"uuid"`
		Name      string  `json:"name"`
		RavenType *string `json:"raven_type"`
	}
	if err := json.Unmarshal(raw, &orgs); err != nil {
		return "", err
	}
	if len(orgs) == 0 {
		return "", fmt.Errorf("no organizations found for session")
	}
	for _, o := range orgs {
		if o.RavenType != nil && *o.RavenType == "team" {
			return o.UUID, nil
		}
	}
	return orgs[0].UUID, nil
}

func claudeAuthorize(sessionKey, orgUUID, challenge, state string) (string, error) {
	authURL := fmt.Sprintf("%s/v1/oauth/%s/authorize", claudeAIBase, orgUUID)
	body, _ := json.Marshal(map[string]any{
		"response_type":         "code",
		"client_id":             claudeOAuthClientID,
		"organization_uuid":     orgUUID,
		"redirect_uri":          claudeOAuthRedirectURI,
		"scope":                 claudeOAuthScopeAPI,
		"state":                 state,
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
	})
	req, err := http.NewRequest(http.MethodPost, authURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://claude.ai")
	req.Header.Set("Referer", "https://claude.ai/new")
	req.AddCookie(&http.Cookie{Name: "sessionKey", Value: sessionKey})
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("authorize: status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var result struct {
		RedirectURI string `json:"redirect_uri"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.RedirectURI == "" {
		return "", fmt.Errorf("authorize: empty redirect_uri")
	}
	u, err := url.Parse(result.RedirectURI)
	if err != nil {
		return "", err
	}
	code := u.Query().Get("code")
	respState := u.Query().Get("state")
	if code == "" {
		return "", fmt.Errorf("authorize: no code in redirect_uri")
	}
	if respState != "" {
		return code + "#" + respState, nil
	}
	return code, nil
}

func claudeExchangeCode(codeWithState, verifier string) (claudeTokenResponse, error) {
	var zero claudeTokenResponse
	authCode := codeWithState
	codeState := ""
	if i := strings.Index(codeWithState, "#"); i >= 0 {
		authCode = codeWithState[:i]
		codeState = codeWithState[i+1:]
	}
	payload := map[string]any{
		"code":          authCode,
		"grant_type":    "authorization_code",
		"client_id":     claudeOAuthClientID,
		"redirect_uri":  claudeOAuthRedirectURI,
		"code_verifier": verifier,
	}
	if codeState != "" {
		payload["state"] = codeState
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, claudeOAuthTokenURL, bytes.NewReader(body))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return zero, fmt.Errorf("token exchange: status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var tok claudeTokenResponse
	if err := json.Unmarshal(raw, &tok); err != nil {
		return zero, err
	}
	if tok.AccessToken == "" {
		return zero, fmt.Errorf("token exchange: empty access_token")
	}
	return tok, nil
}