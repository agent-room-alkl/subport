package gateway

// Antigravity (Google Cloud Code) subscription OAuth helpers.
//
// This is NOT a Google Cloud paid API-key path. It uses Antigravity / Cloud Code
// desktop OAuth tokens (Authorization: Bearer …) against
// cloudcode-pa.googleapis.com/v1internal:*, following the proven shape in
// _ref/sub2api.
//
// Credentials stay in env, token files, or an optional auth.json. They are
// never logged or returned by any API.

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
	antigravityOAuthTokenURL    = "https://oauth2.googleapis.com/token"
	antigravityDefaultUAVersion = "2.9.1"
)

type antigravityTokenPair struct {
	AccessToken  string
	RefreshToken string
	ProjectID    string
	ExpiresAt    time.Time
}

var (
	antigravityCredMu  sync.RWMutex
	antigravityRuntime antigravityTokenPair
)

func antigravityRuntimeAccessToken() string {
	antigravityCredMu.RLock()
	defer antigravityCredMu.RUnlock()
	return antigravityRuntime.AccessToken
}

func antigravityRuntimeExpiresRFC3339() string {
	antigravityCredMu.RLock()
	defer antigravityCredMu.RUnlock()
	if antigravityRuntime.ExpiresAt.IsZero() {
		return ""
	}
	return antigravityRuntime.ExpiresAt.UTC().Format(time.RFC3339)
}

func antigravityRuntimeProjectID() string {
	antigravityCredMu.RLock()
	defer antigravityCredMu.RUnlock()
	return antigravityRuntime.ProjectID
}

func antigravitySetRuntime(access, refresh, projectID string, expiresIn int64) {
	antigravityCredMu.Lock()
	defer antigravityCredMu.Unlock()
	if access != "" {
		antigravityRuntime.AccessToken = access
	}
	if refresh != "" {
		antigravityRuntime.RefreshToken = refresh
	}
	if projectID != "" {
		antigravityRuntime.ProjectID = projectID
	}
	if expiresIn > 0 {
		antigravityRuntime.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
}

func antigravityOAuthClientID() string {
	for _, k := range []string{"SUBPORT_ANTIGRAVITY_CLIENT_ID", "ANTIGRAVITY_OAUTH_CLIENT_ID"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func antigravityClientSecret() string {
	for _, k := range []string{"SUBPORT_ANTIGRAVITY_CLIENT_SECRET", "ANTIGRAVITY_OAUTH_CLIENT_SECRET"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
func antigravityUserAgent() string {
	ver := strings.TrimSpace(os.Getenv("SUBPORT_ANTIGRAVITY_UA_VERSION"))
	if ver == "" {
		ver = strings.TrimSpace(os.Getenv("ANTIGRAVITY_USER_AGENT_VERSION"))
	}
	if ver == "" {
		ver = antigravityDefaultUAVersion
	}
	return "antigravity/" + ver + " windows/amd64"
}

func antigravityRefreshToken() string {
	antigravityCredMu.RLock()
	rt := antigravityRuntime.RefreshToken
	antigravityCredMu.RUnlock()
	if rt != "" {
		return rt
	}
	return loadAntigravityRefreshToken()
}

type antigravityAuthFile struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ProjectID    string `json:"project_id"`
	Tokens       *struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ProjectID    string `json:"project_id"`
	} `json:"tokens"`
}

func antigravityAuthJSONPath() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_ANTIGRAVITY_AUTH_FILE")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".antigravity", "auth.json")
}

func loadAntigravityAuthFile() (access, refresh, projectID string) {
	path := antigravityAuthJSONPath()
	if path == "" {
		return "", "", ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", ""
	}
	var af antigravityAuthFile
	if err := json.Unmarshal(b, &af); err != nil {
		return "", "", ""
	}
	access = strings.TrimSpace(af.AccessToken)
	refresh = strings.TrimSpace(af.RefreshToken)
	projectID = strings.TrimSpace(af.ProjectID)
	if af.Tokens != nil {
		if access == "" {
			access = strings.TrimSpace(af.Tokens.AccessToken)
		}
		if refresh == "" {
			refresh = strings.TrimSpace(af.Tokens.RefreshToken)
		}
		if projectID == "" {
			projectID = strings.TrimSpace(af.Tokens.ProjectID)
		}
	}
	return access, refresh, projectID
}

func loadAntigravityAccessToken() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_ANTIGRAVITY_ACCESS_TOKEN")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("SUBPORT_PROVIDER_KEY_ANTIGRAVITY")); v != "" {
		return v
	}
	if b, err := os.ReadFile(".antigravity_access_token"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	access, _, _ := loadAntigravityAuthFile()
	return access
}

func loadAntigravityRefreshToken() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_ANTIGRAVITY_REFRESH_TOKEN")); v != "" {
		return v
	}
	if b, err := os.ReadFile(".antigravity_refresh_token"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	_, refresh, _ := loadAntigravityAuthFile()
	return refresh
}

func loadAntigravityProjectID() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_ANTIGRAVITY_PROJECT_ID")); v != "" {
		return v
	}
	if b, err := os.ReadFile(".antigravity_project_id"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	antigravityCredMu.RLock()
	id := antigravityRuntime.ProjectID
	antigravityCredMu.RUnlock()
	if id != "" {
		return id
	}
	_, _, projectID := loadAntigravityAuthFile()
	return projectID
}

func antigravityCredential() string {
	if v := antigravityRuntimeAccessToken(); v != "" {
		return v
	}
	return loadAntigravityAccessToken()
}

func antigravityCredentialFor(accountID string) string {
	if v := accountAccessToken(accountID); v != "" {
		return v
	}
	return antigravityCredential()
}

func antigravityProjectIDFor(accountID string) string {
	if extra := accountExtraJSON(accountID); extra != "" {
		if id := antigravityProjectIDFromExtra(extra); id != "" {
			return id
		}
	}
	return loadAntigravityProjectID()
}

func antigravityProjectIDFromExtra(extraJSON string) string {
	if extraJSON == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(extraJSON), &m) != nil {
		return ""
	}
	for _, k := range []string{"project_id", "antigravity_project_id", "projectId"} {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

// HasAntigravityCredential reports whether any Antigravity access token is available.
func HasAntigravityCredential() bool {
	return antigravityCredential() != ""
}

// EnsureAntigravityTokensFromEnv loads auth.json / token files into the
// in-memory runtime (and refreshes when only a refresh token is present).
func EnsureAntigravityTokensFromEnv() error {
	access := loadAntigravityAccessToken()
	refresh := loadAntigravityRefreshToken()
	projectID := loadAntigravityProjectID()
	if access != "" {
		antigravitySetRuntime(access, refresh, projectID, 0)
		return nil
	}
	if antigravityRuntimeAccessToken() != "" {
		return nil
	}
	if refresh != "" {
		_, err := antigravityTryRefresh()
		return err
	}
	return nil
}

type antigravityTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func antigravityTryRefresh() (bool, error) {
	rt := antigravityRefreshToken()
	if rt == "" {
		return false, fmt.Errorf("no refresh token")
	}
	access, refresh, expiresIn, err := antigravityRefreshWithToken(rt)
	if err != nil {
		return false, err
	}
	antigravitySetRuntime(access, refresh, loadAntigravityProjectID(), expiresIn)
	log.Printf("antigravity bootstrap: refreshed OAuth access token (expires_in=%ds)", expiresIn)
	return true, nil
}

func antigravityRefreshWithToken(rt string) (string, string, int64, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", rt)
	form.Set("client_id", antigravityOAuthClientID())
	form.Set("client_secret", antigravityClientSecret())

	req, err := http.NewRequest(http.MethodPost, antigravityOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := upstreamClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("antigravity token refresh status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var out antigravityTokenResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", 0, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return "", "", 0, fmt.Errorf("antigravity token refresh returned empty access_token")
	}
	newRefresh := out.RefreshToken
	if newRefresh == "" {
		newRefresh = rt
	}
	return out.AccessToken, newRefresh, out.ExpiresIn, nil
}
