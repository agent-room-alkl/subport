package gateway

// Claude subscription OAuth helpers (sessionKey → access_token) and in-memory
// credential hold. Patterns taken from _ref/sub2api's claude_oauth_service and
// pkg/oauth — smallest path only: org lookup, authorize, code exchange, refresh.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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

// ClaudeOAuthTokenInfo is the public result of a sessionKey exchange.
// Secrets must never be logged.
type ClaudeOAuthTokenInfo struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	ExpiresAt    string // RFC3339
}

// ClaudeExchangeError is a classified exchange failure for admin API mapping.
type ClaudeExchangeError struct {
	Code           string // session_stale_relogin | subscription_required | authorization_denied | cloudflare | rate_limited | helper_unavailable | exchange_failed
	Message        string
	Step           string // organizations | authorize | token | unknown
	UpstreamStatus int    // upstream HTTP status when known; 0 if unknown
}

func (e *ClaudeExchangeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func inferClaudeExchangeStep(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "organizations"):
		return "organizations"
	case strings.Contains(low, "authorize"):
		return "authorize"
	case strings.Contains(low, "token"):
		return "token"
	default:
		return "unknown"
	}
}

func inferClaudeUpstreamStatus(msg string) int {
	low := strings.ToLower(msg)
	for _, key := range []string{"status ", "status="} {
		if i := strings.Index(low, key); i >= 0 {
			rest := low[i+len(key):]
			n := 0
			for _, ch := range rest {
				if ch < '0' || ch > '9' {
					break
				}
				n = n*10 + int(ch-'0')
				if n > 999 {
					return 0
				}
			}
			if n >= 100 && n <= 599 {
				return n
			}
		}
	}
	return 0
}

func classifyClaudeExchangeErr(err error) *ClaudeExchangeError {
	if err == nil {
		return nil
	}
	if ce, ok := err.(*ClaudeExchangeError); ok {
		if ce.Step == "" {
			ce.Step = inferClaudeExchangeStep(ce.Message)
		}
		if ce.UpstreamStatus == 0 {
			ce.UpstreamStatus = inferClaudeUpstreamStatus(ce.Message)
		}
		return ce
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	code := "exchange_failed"
	switch {
	case strings.Contains(low, "claude_session_exchange.py not found") ||
		strings.Contains(low, "executable file not found") ||
		strings.Contains(low, "no module named 'curl_cffi'") ||
		strings.Contains(low, "no module named curl_cffi"):
		code = "helper_unavailable"
	case strings.Contains(low, "just a moment") || strings.Contains(low, "cloudflare") || strings.Contains(low, "cf-mitigated"):
		code = "cloudflare"
	case strings.Contains(low, "429") || strings.Contains(low, "rate limited") || strings.Contains(low, "rate_limited"):
		code = "rate_limited"
	case strings.Contains(low, "subscription_required") || strings.Contains(low, "pro or max") ||
		(strings.Contains(low, "requires a pro") && strings.Contains(low, "subscription")):
		code = "subscription_required"
	case strings.Contains(low, "authorization_denied") || strings.Contains(low, "permission_error"):
		code = "authorization_denied"
	case strings.Contains(low, "401") || strings.Contains(low, "unauthorized") ||
		strings.Contains(low, "no organizations") || strings.Contains(low, "sessionkey invalid") ||
		strings.Contains(low, "stale") || strings.Contains(low, "expired"):
		code = "session_stale_relogin"
	}
	return &ClaudeExchangeError{Code: code, Message: msg, Step: inferClaudeExchangeStep(msg), UpstreamStatus: inferClaudeUpstreamStatus(msg)}
}

func isClaudeCloudflareErr(err error) bool {
	ce := classifyClaudeExchangeErr(err)
	return ce != nil && ce.Code == "cloudflare"
}

// ExchangeClaudeSessionKey exchanges a Claude.ai sessionKey for OAuth tokens.
// Tries the native Go client first; on Cloudflare blocks, falls back once to the
// curl_cffi chrome131 helper at scripts/claude_session_exchange.py.
func ExchangeClaudeSessionKey(sessionKey, orgUUID string) (*ClaudeOAuthTokenInfo, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, &ClaudeExchangeError{Code: "exchange_failed", Message: "session_key required"}
	}
	pair, err := claudeExchangeSession(sessionKey, strings.TrimSpace(orgUUID))
	if err != nil {
		if isClaudeCloudflareErr(err) || forceClaudePythonExchange() {
			log.Printf("claude oauth: native exchange blocked or forced; trying curl_cffi helper")
			pair, err = claudeExchangeSessionViaPython(sessionKey, strings.TrimSpace(orgUUID))
		}
		if err != nil {
			return nil, classifyClaudeExchangeErr(err)
		}
	}
	info := &ClaudeOAuthTokenInfo{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
	}
	if pair.ExpiresIn > 0 {
		info.ExpiresAt = time.Now().UTC().Add(time.Duration(pair.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	claudeSetRuntime(pair.AccessToken, pair.RefreshToken, pair.ExpiresIn)
	return info, nil
}

func forceClaudePythonExchange() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("SUBPORT_CLAUDE_EXCHANGE_PYTHON")))
	return v == "1" || v == "true" || v == "yes"
}

func claudeExchangeSessionViaPython(sessionKey, orgUUID string) (claudeTokenResponse, error) {
	var zero claudeTokenResponse
	script, err := findClaudeExchangeScript()
	if err != nil {
		return zero, err
	}
	py := findPythonExecutable()
	payload := map[string]string{"session_key": sessionKey}
	if orgUUID != "" {
		payload["org_uuid"] = orgUUID
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, script)
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	// Log only non-secret stderr status lines (lengths / status codes).
	if se := strings.TrimSpace(stderr.String()); se != "" {
		for _, line := range strings.Split(se, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			low := strings.ToLower(line)
			if strings.Contains(low, "sk-ant") || strings.Contains(low, "sessionkey") || strings.Contains(low, "access_token") {
				continue
			}
			log.Printf("claude oauth helper: %s", truncate(line, 200))
		}
	}
	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		if runErr != nil {
			return zero, fmt.Errorf("python helper failed: %w", runErr)
		}
		return zero, fmt.Errorf("python helper returned empty output")
	}
	var out struct {
		OK             bool   `json:"ok"`
		AccessToken    string `json:"access_token"`
		RefreshToken   string `json:"refresh_token"`
		ExpiresIn      int64  `json:"expires_in"`
		Error          string `json:"error"`
		Code           string `json:"code"`
		Step           string `json:"step"`
		UpstreamStatus int    `json:"upstream_status"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return zero, fmt.Errorf("python helper bad JSON: %w", err)
	}
	if !out.OK || out.AccessToken == "" {
		code := out.Code
		if code == "" {
			code = "exchange_failed"
		}
		msg := out.Error
		if msg == "" {
			msg = "python helper exchange failed"
		}
		step := strings.TrimSpace(out.Step)
		if step == "" {
			step = inferClaudeExchangeStep(msg)
		}
		ust := out.UpstreamStatus
		if ust == 0 {
			ust = inferClaudeUpstreamStatus(msg)
		}
		return zero, &ClaudeExchangeError{Code: code, Message: msg, Step: step, UpstreamStatus: ust}
	}
	return claudeTokenResponse{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresIn:    out.ExpiresIn,
		TokenType:    "Bearer",
	}, nil
}

func findClaudeExchangeScript() (string, error) {
	candidates := []string{}
	if v := strings.TrimSpace(os.Getenv("SUBPORT_CLAUDE_EXCHANGE_SCRIPT")); v != "" {
		candidates = append(candidates, v)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "scripts", "claude_session_exchange.py"),
			filepath.Join(cwd, "claude_session_exchange.py"),
		)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "scripts", "claude_session_exchange.py"),
			filepath.Join(dir, "claude_session_exchange.py"),
		)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("claude_session_exchange.py not found (set SUBPORT_CLAUDE_EXCHANGE_SCRIPT)")
}

func findPythonExecutable() string {
	if v := strings.TrimSpace(os.Getenv("SUBPORT_PYTHON")); v != "" {
		return v
	}
	for _, name := range []string{"python", "python3", "py"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return "python"
}
