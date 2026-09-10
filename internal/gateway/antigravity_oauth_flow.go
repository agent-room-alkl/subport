package gateway

// Antigravity Google OAuth (PKCE) one-click authorize flow for admin.
// Matches sub2api ClientID / RedirectURI / scopes so Google accepts the
// desktop client. Tokens are never logged.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	antigravityAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	antigravityUserInfoURL  = "https://www.googleapis.com/oauth2/v2/userinfo"
	// Same RedirectURI as sub2api pkg/antigravity — Google accepts this with ClientID.
	antigravityOAuthRedirectURI = "http://localhost:8085/callback"
	antigravityOAuthScopes      = "https://www.googleapis.com/auth/cloud-platform " +
		"https://www.googleapis.com/auth/userinfo.email " +
		"https://www.googleapis.com/auth/userinfo.profile " +
		"https://www.googleapis.com/auth/cclog " +
		"https://www.googleapis.com/auth/experimentsandconfigs"
	antigravityOAuthSessionTTL = 30 * time.Minute
	antigravityProdBaseURL     = "https://cloudcode-pa.googleapis.com"
	antigravityDailyBaseURL    = "https://daily-cloudcode-pa.googleapis.com"
)

type antigravityOAuthSession struct {
	AccountID    string
	State        string
	CodeVerifier string
	CreatedAt    time.Time
}

// AntigravityOAuthTokenInfo is the non-logging result of a code exchange.
type AntigravityOAuthTokenInfo struct {
	AccessToken  string `json:"-"`
	RefreshToken string `json:"-"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    string `json:"expires_at"` // RFC3339
	Email        string `json:"email,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
}

// AntigravityOAuthStartResult is returned by StartAntigravityOAuth.
type AntigravityOAuthStartResult struct {
	AuthURL   string `json:"auth_url"`
	SessionID string `json:"session_id"`
	State     string `json:"state"`
}

// AntigravityOAuthStatus is a non-secret poll view for the admin UI.
type AntigravityOAuthStatus struct {
	SessionID string `json:"session_id"`
	Done      bool   `json:"done"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Email     string `json:"email,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Applied   bool   `json:"applied"`
}

type antigravityOAuthCompletion struct {
	AccountID string
	Info      *AntigravityOAuthTokenInfo
	Err       string
	Done      bool
	Applied   bool
}

var (
	antigravityOAuthMu       sync.Mutex
	antigravityOAuthSessions = map[string]*antigravityOAuthSession{}
	antigravityOAuthByState  = map[string]string{} // state -> sessionID
	antigravityOAuthDone     = map[string]*antigravityOAuthCompletion{}
	antigravityCallbackOnce  sync.Once
	antigravityCallbackErr   error
)

// AntigravityOAuthPersist is set by httpapi to upsert credentials after a
// successful exchange (callback or explicit exchange). Never log tokens.
var AntigravityOAuthPersist func(accountID string, info *AntigravityOAuthTokenInfo) error

// StartAntigravityOAuth creates a PKCE session and returns the Google auth URL.
func StartAntigravityOAuth(accountID string) (*AntigravityOAuthStartResult, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, fmt.Errorf("account_id required")
	}
	EnsureAntigravityCallbackListener()

	state, err := oauthRandomState()
	if err != nil {
		return nil, err
	}
	verifier, err := oauthCodeVerifier()
	if err != nil {
		return nil, err
	}
	sidBytes, err := antigravityRandomBytes(16)
	if err != nil {
		return nil, err
	}
	sessionID := hex.EncodeToString(sidBytes)
	challenge := oauthCodeChallengeS256(verifier)

	sess := &antigravityOAuthSession{
		AccountID:    accountID,
		State:        state,
		CodeVerifier: verifier,
		CreatedAt:    time.Now(),
	}
	antigravityOAuthMu.Lock()
	antigravityOAuthSessions[sessionID] = sess
	antigravityOAuthByState[state] = sessionID
	delete(antigravityOAuthDone, sessionID)
	antigravityOAuthMu.Unlock()

	params := url.Values{}
	params.Set("client_id", antigravityOAuthClientID())
	params.Set("redirect_uri", antigravityOAuthRedirectURI)
	params.Set("response_type", "code")
	params.Set("scope", antigravityOAuthScopes)
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("include_granted_scopes", "true")

	return &AntigravityOAuthStartResult{
		AuthURL:   antigravityAuthorizeURL + "?" + params.Encode(),
		SessionID: sessionID,
		State:     state,
	}, nil
}

// antigravityRandomBytes returns n cryptographically random bytes.
func antigravityRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

// ExchangeAntigravityOAuth exchanges code (+ optional callback_url) for tokens,
// loads project_id, persists via AntigravityOAuthPersist when set, and marks
// the session complete for polling.
func ExchangeAntigravityOAuth(sessionID, state, code, callbackURL string) (*AntigravityOAuthTokenInfo, error) {
	sessionID = strings.TrimSpace(sessionID)
	state = strings.TrimSpace(state)
	code = strings.TrimSpace(code)
	callbackURL = strings.TrimSpace(callbackURL)

	if code == "" && callbackURL != "" {
		c, st, err := ParseAntigravityCallbackURL(callbackURL)
		if err != nil {
			return nil, err
		}
		code = c
		if state == "" {
			state = st
		}
	}
	if sessionID == "" || state == "" || code == "" {
		return nil, fmt.Errorf("session_id, state, and code (or callback_url) are required")
	}

	antigravityOAuthMu.Lock()
	sess, ok := antigravityOAuthSessions[sessionID]
	if ok && time.Since(sess.CreatedAt) > antigravityOAuthSessionTTL {
		delete(antigravityOAuthSessions, sessionID)
		delete(antigravityOAuthByState, sess.State)
		ok = false
	}
	antigravityOAuthMu.Unlock()
	if !ok || sess == nil {
		return nil, fmt.Errorf("oauth session not found or expired")
	}
	if sess.State != state {
		return nil, fmt.Errorf("state mismatch")
	}

	info, err := antigravityExchangeAndLoad(context.Background(), code, sess.CodeVerifier)
	comp := &antigravityOAuthCompletion{AccountID: sess.AccountID, Done: true}
	if err != nil {
		comp.Err = err.Error()
		antigravityOAuthMu.Lock()
		antigravityOAuthDone[sessionID] = comp
		antigravityOAuthMu.Unlock()
		return nil, err
	}
	comp.Info = info

	if AntigravityOAuthPersist != nil {
		if perr := AntigravityOAuthPersist(sess.AccountID, info); perr != nil {
			comp.Err = perr.Error()
			antigravityOAuthMu.Lock()
			antigravityOAuthDone[sessionID] = comp
			antigravityOAuthMu.Unlock()
			return nil, fmt.Errorf("persist credentials: %w", perr)
		}
		comp.Applied = true
	}

	antigravityOAuthMu.Lock()
	antigravityOAuthDone[sessionID] = comp
	delete(antigravityOAuthSessions, sessionID)
	delete(antigravityOAuthByState, sess.State)
	antigravityOAuthMu.Unlock()
	return info, nil
}

// PollAntigravityOAuth returns completion status for a session (non-secret).
func PollAntigravityOAuth(sessionID string) AntigravityOAuthStatus {
	sessionID = strings.TrimSpace(sessionID)
	st := AntigravityOAuthStatus{SessionID: sessionID}
	antigravityOAuthMu.Lock()
	defer antigravityOAuthMu.Unlock()
	comp, ok := antigravityOAuthDone[sessionID]
	if !ok || comp == nil || !comp.Done {
		return st
	}
	st.Done = true
	st.Applied = comp.Applied
	if comp.Err != "" {
		st.Error = comp.Err
		return st
	}
	st.OK = true
	if comp.Info != nil {
		st.Email = comp.Info.Email
		st.ProjectID = comp.Info.ProjectID
		st.ExpiresAt = comp.Info.ExpiresAt
	}
	return st
}

// AntigravityOAuthSessionAccount returns the account id bound to a live or
// completed OAuth session, if known.
func AntigravityOAuthSessionAccount(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	antigravityOAuthMu.Lock()
	defer antigravityOAuthMu.Unlock()
	if sess, ok := antigravityOAuthSessions[sessionID]; ok && sess != nil {
		return sess.AccountID
	}
	if comp, ok := antigravityOAuthDone[sessionID]; ok && comp != nil {
		return comp.AccountID
	}
	return ""
}

// ParseAntigravityCallbackURL extracts code and state from a full redirect URL.
func ParseAntigravityCallbackURL(raw string) (code, state string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("empty callback url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid callback url: %w", err)
	}
	q := u.Query()
	code = strings.TrimSpace(q.Get("code"))
	state = strings.TrimSpace(q.Get("state"))
	if code == "" {
		if e := strings.TrimSpace(q.Get("error")); e != "" {
			return "", state, fmt.Errorf("oauth error: %s", e)
		}
		return "", "", fmt.Errorf("callback url missing code")
	}
	return code, state, nil
}

func antigravityExchangeAndLoad(ctx context.Context, code, verifier string) (*AntigravityOAuthTokenInfo, error) {
	form := url.Values{}
	form.Set("client_id", antigravityOAuthClientID())
	form.Set("client_secret", antigravityClientSecret())
	form.Set("code", code)
	form.Set("redirect_uri", antigravityOAuthRedirectURI)
	form.Set("grant_type", "authorization_code")
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := upstreamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token exchange status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var tok antigravityTokenResponse
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, fmt.Errorf("token response decode: %w", err)
	}
	if strings.TrimSpace(tok.AccessToken) == "" {
		return nil, fmt.Errorf("token exchange returned empty access_token")
	}

	info := &AntigravityOAuthTokenInfo{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresIn:    tok.ExpiresIn,
	}
	if tok.ExpiresIn > 0 {
		// Skew 5 minutes like sub2api.
		exp := time.Now().Add(time.Duration(tok.ExpiresIn-300) * time.Second)
		info.ExpiresAt = exp.UTC().Format(time.RFC3339)
	}

	if email, err := antigravityFetchUserEmail(ctx, tok.AccessToken); err == nil {
		info.Email = email
	}
	if projectID, err := antigravityLoadProjectID(ctx, tok.AccessToken); err == nil {
		info.ProjectID = projectID
	} else {
		log.Printf("antigravity oauth: project_id lookup warning: %v", err)
	}
	return info, nil
}

func antigravityFetchUserEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, antigravityUserInfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("userinfo status %d", resp.StatusCode)
	}
	var ui struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(raw, &ui); err != nil {
		return "", err
	}
	return strings.TrimSpace(ui.Email), nil
}

func antigravityLoadProjectID(ctx context.Context, accessToken string) (string, error) {
	body := map[string]any{
		"metadata": map[string]string{
			"ideType":    "ANTIGRAVITY",
			"ideVersion": antigravityDefaultUAVersion,
			"ideName":    "antigravity",
		},
	}
	payload, _ := json.Marshal(body)
	var lastErr error
	for _, base := range []string{antigravityProdBaseURL, antigravityDailyBaseURL} {
		apiURL := base + "/v1internal:loadCodeAssist"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(payload)))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", antigravityUserAgent())
		resp, err := upstreamClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout ||
			resp.StatusCode == http.StatusNotFound || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("loadCodeAssist HTTP %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode >= 300 {
			return "", fmt.Errorf("loadCodeAssist HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
		}
		var parsed struct {
			CloudAICompanionProject string `json:"cloudaicompanionProject"`
		}
		var rawMap map[string]any
		_ = json.Unmarshal(raw, &parsed)
		_ = json.Unmarshal(raw, &rawMap)
		if id := strings.TrimSpace(parsed.CloudAICompanionProject); id != "" {
			return id, nil
		}
		if id, err := antigravityTryOnboard(ctx, accessToken, base, rawMap); err == nil && id != "" {
			return id, nil
		} else if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("loadCodeAssist missing project_id")
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("project_id not found")
	}
	return "", lastErr
}

func antigravityTryOnboard(ctx context.Context, accessToken, baseURL string, loadRaw map[string]any) (string, error) {
	tierID := antigravityDefaultTierID(loadRaw)
	if tierID == "" {
		return "", fmt.Errorf("no default tier for onboard")
	}
	body := map[string]any{
		"tierId": tierID,
		"metadata": map[string]string{
			"ideType":    "ANTIGRAVITY",
			"platform":   "PLATFORM_UNSPECIFIED",
			"pluginType": "GEMINI",
		},
	}
	payload, _ := json.Marshal(body)
	apiURL := strings.TrimRight(baseURL, "/") + "/v1internal:onboardUser"
	for attempt := 0; attempt < 5; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(payload)))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", antigravityUserAgent())
		resp, err := upstreamClient.Do(req)
		if err != nil {
			return "", err
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			return "", fmt.Errorf("onboardUser HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
		}
		var out struct {
			Done     bool           `json:"done"`
			Response map[string]any `json:"response"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", err
		}
		if out.Done {
			if id := antigravityProjectFromOnboard(out.Response); id != "" {
				return id, nil
			}
			return "", fmt.Errorf("onboardUser done without project_id")
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("onboardUser timed out")
}

func antigravityDefaultTierID(loadRaw map[string]any) string {
	if loadRaw == nil {
		return ""
	}
	rawTiers, ok := loadRaw["allowedTiers"].([]any)
	if !ok {
		return ""
	}
	for _, raw := range rawTiers {
		tier, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if isDefault, _ := tier["isDefault"].(bool); !isDefault {
			continue
		}
		if id, ok := tier["id"].(string); ok {
			if t := strings.TrimSpace(id); t != "" {
				return t
			}
		}
	}
	return ""
}

func antigravityProjectFromOnboard(resp map[string]any) string {
	if resp == nil {
		return ""
	}
	v, ok := resp["cloudaicompanionProject"]
	if !ok {
		return ""
	}
	switch project := v.(type) {
	case string:
		return strings.TrimSpace(project)
	case map[string]any:
		if id, ok := project["id"].(string); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

// EnsureAntigravityCallbackListener starts the localhost:8085 OAuth redirect
// catcher once. Safe to call repeatedly.
func EnsureAntigravityCallbackListener() {
	antigravityCallbackOnce.Do(func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/callback", antigravityOAuthCallbackHTTP)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" || r.URL.Path == "" {
				http.Redirect(w, r, "/callback", http.StatusFound)
				return
			}
			http.NotFound(w, r)
		})
		srv := &http.Server{Addr: "127.0.0.1:8085", Handler: mux}
		go func() {
			log.Printf("antigravity oauth: callback listener on http://127.0.0.1:8085/callback")
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				antigravityCallbackErr = err
				log.Printf("antigravity oauth: callback listener error: %v", err)
			}
		}()
	})
}

func antigravityOAuthCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := strings.TrimSpace(q.Get("code"))
	state := strings.TrimSpace(q.Get("state"))
	oauthErr := strings.TrimSpace(q.Get("error"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if oauthErr != "" {
		_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>Antigravity OAuth</title></head>
<body style="font-family:system-ui;padding:2rem;max-width:40rem">
<h1>授权失败</h1><p>%s</p>
<p>请回到 Subport 重试，或粘贴本页完整 URL。</p>
<p style="word-break:break-all;font-size:12px;color:#666">%s</p>
</body></html>`, htmlEscape(oauthErr), htmlEscape(r.URL.String()))
		return
	}
	if code == "" || state == "" {
		_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><title>Antigravity OAuth</title></head>
<body style="font-family:system-ui;padding:2rem"><h1>等待 Google 回调</h1>
<p>此页应带有 code 与 state 参数。请从 Subport 点击「Google 授权」开始。</p></body></html>`)
		return
	}

	antigravityOAuthMu.Lock()
	sessionID := antigravityOAuthByState[state]
	antigravityOAuthMu.Unlock()

	var exchangeErr string
	var email, projectID string
	ok := false
	if sessionID != "" {
		info, err := ExchangeAntigravityOAuth(sessionID, state, code, "")
		if err != nil {
			exchangeErr = err.Error()
		} else if info != nil {
			ok = true
			email = info.Email
			projectID = info.ProjectID
		}
	} else {
		exchangeErr = "session not found for state (paste this URL into Subport)"
	}

	fullURL := "http://localhost:8085/callback?" + r.URL.RawQuery
	status := "授权成功，请回到 Subport"
	if !ok {
		status = "已收到 Google 回调，请回到 Subport 粘贴下方 URL 完成交换"
		if exchangeErr != "" {
			status = "自动交换未完成：" + exchangeErr + "。请回到 Subport 粘贴下方 URL。"
		}
	}

	_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>Antigravity OAuth</title></head>
<body style="font-family:system-ui;padding:2rem;max-width:48rem">
<h1>%s</h1>
<p>可关闭本窗口，返回 Subport 管理页。</p>
%s
<label style="display:block;margin-top:1rem;font-size:12px;color:#555">回调 URL（粘贴备用）</label>
<textarea id="cb" readonly style="width:100%%;min-height:5rem;font-family:monospace;font-size:12px">%s</textarea>
<script>
(function(){
  var payload = {type:'subport-antigravity-oauth', ok:%t, session_id:%q, state:%q, code:%q, callback_url: document.getElementById('cb').value};
  try { if (window.opener) window.opener.postMessage(payload, '*'); } catch (e) {}
})();
</script>
</body></html>`,
		htmlEscape(status),
		func() string {
			if ok {
				extra := ""
				if email != "" {
					extra += "<p>账号：" + htmlEscape(email) + "</p>"
				}
				if projectID != "" {
					extra += "<p>project_id：" + htmlEscape(projectID) + "</p>"
				}
				return extra
			}
			return ""
		}(),
		htmlEscape(fullURL),
		ok,
		sessionID,
		state,
		code,
	)
}

func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return r.Replace(s)
}

