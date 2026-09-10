package gateway

// ChatGPT / Codex CLI subscription OAuth helpers.
//
// This is NOT the OpenAI Platform API-key path (sk-... → api.openai.com as a
// paid API customer). It uses Codex CLI / ChatGPT subscription OAuth tokens
// (Authorization: Bearer …) against chatgpt.com/backend-api/codex/responses,
// following the proven shape in _ref/sub2api.
//
// Credentials stay in env, token files, or the local Codex CLI auth.json.
// They are never stored in the accounts table, logged, or returned by any API.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	codexOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexOAuthTokenURL = "https://auth.openai.com/oauth/token"
	codexRefreshScopes = "openid profile email"
)

type codexTokenPair struct {
	AccessToken  string
	RefreshToken string
	AccountID    string
	ExpiresAt    time.Time
}

var (
	codexCredMu  sync.RWMutex
	codexRuntime codexTokenPair
)

func codexRuntimeAccessToken() string {
	codexCredMu.RLock()
	defer codexCredMu.RUnlock()
	return codexRuntime.AccessToken
}

func codexRuntimeExpiresRFC3339() string {
	codexCredMu.RLock()
	defer codexCredMu.RUnlock()
	if codexRuntime.ExpiresAt.IsZero() {
		return ""
	}
	return codexRuntime.ExpiresAt.UTC().Format(time.RFC3339)
}

func codexRuntimeAccountID() string {
	codexCredMu.RLock()
	defer codexCredMu.RUnlock()
	return codexRuntime.AccountID
}

func codexSetRuntime(access, refresh, accountID string, expiresIn int64) {
	codexCredMu.Lock()
	defer codexCredMu.Unlock()
	if access != "" {
		codexRuntime.AccessToken = access
	}
	if refresh != "" {
		codexRuntime.RefreshToken = refresh
	}
	if accountID != "" {
		codexRuntime.AccountID = accountID
	}
	if expiresIn > 0 {
		codexRuntime.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
}

func codexRefreshToken() string {
	codexCredMu.RLock()
	rt := codexRuntime.RefreshToken
	codexCredMu.RUnlock()
	if rt != "" {
		return rt
	}
	return loadCodexRefreshToken()
}

type codexAuthFile struct {
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
}

func codexAuthJSONPath() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_CODEX_AUTH_FILE")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".codex", "auth.json")
}

func loadCodexAuthFile() (access, refresh, accountID string) {
	path := codexAuthJSONPath()
	if path == "" {
		return "", "", ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", ""
	}
	var af codexAuthFile
	if err := json.Unmarshal(b, &af); err != nil {
		return "", "", ""
	}
	return strings.TrimSpace(af.Tokens.AccessToken),
		strings.TrimSpace(af.Tokens.RefreshToken),
		strings.TrimSpace(af.Tokens.AccountID)
}

func loadCodexAccessToken() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_CODEX_ACCESS_TOKEN")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("SUBPORT_PROVIDER_KEY_CODEX")); v != "" {
		return v
	}
	if b, err := os.ReadFile(".codex_access_token"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	access, _, _ := loadCodexAuthFile()
	return access
}

func loadCodexRefreshToken() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_CODEX_REFRESH_TOKEN")); v != "" {
		return v
	}
	if b, err := os.ReadFile(".codex_refresh_token"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	_, refresh, _ := loadCodexAuthFile()
	return refresh
}

func loadCodexAccountID() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_CODEX_ACCOUNT_ID")); v != "" {
		return v
	}
	if b, err := os.ReadFile(".codex_account_id"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	codexCredMu.RLock()
	id := codexRuntime.AccountID
	codexCredMu.RUnlock()
	if id != "" {
		return id
	}
	_, _, accountID := loadCodexAuthFile()
	return accountID
}

func codexCredential() string {
	// Global path (no account id): runtime -> env/files.
	if v := codexRuntimeAccessToken(); v != "" {
		return v
	}
	return loadCodexAccessToken()
}

func codexCredentialFor(accountID string) string {
	// DB/cache -> runtime -> env/files.
	if v := accountAccessToken(accountID); v != "" {
		return v
	}
	return codexCredential()
}

func codexAccountIDFor(accountID string) string {
	if extra := accountExtraJSON(accountID); extra != "" {
		if id := chatgptAccountIDFromExtra(extra); id != "" {
			return id
		}
	}
	return loadCodexAccountID()
}

// HasCodexCredential reports whether any Codex/ChatGPT access token is available.
func HasCodexCredential() bool {
	return codexCredential() != ""
}

// EnsureCodexTokensFromEnv loads Codex CLI auth.json / token files into the
// in-memory runtime (and refreshes when only a refresh token is present).
func EnsureCodexTokensFromEnv() error {
	access := loadCodexAccessToken()
	refresh := loadCodexRefreshToken()
	accountID := loadCodexAccountID()
	if access != "" {
		codexSetRuntime(access, refresh, accountID, 0)
		return nil
	}
	if codexRuntimeAccessToken() != "" {
		return nil
	}
	if refresh != "" {
		_, err := codexTryRefresh()
		return err
	}
	return nil
}

type codexTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
}

func codexTryRefresh() (bool, error) {
	rt := codexRefreshToken()
	if rt == "" {
		return false, fmt.Errorf("no refresh token")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", rt)
	form.Set("client_id", codexOAuthClientID)
	form.Set("scope", codexRefreshScopes)

	req, err := http.NewRequest(http.MethodPost, codexOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", codexCLIUserAgent)
	req.Header.Set("originator", codexOriginator)

	resp, err := upstreamClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return false, fmt.Errorf("codex token refresh status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var out codexTokenResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return false, fmt.Errorf("codex token refresh returned empty access_token")
	}
	codexSetRuntime(out.AccessToken, out.RefreshToken, loadCodexAccountID(), out.ExpiresIn)
	log.Printf("codex bootstrap: refreshed OAuth access token (expires_in=%ds)", out.ExpiresIn)
	return true, nil
}
