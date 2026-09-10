package gateway

import (
	"bytes"
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

// RefreshBeforeExpiry is how soon before ExpiresAt a token is refreshed.
const RefreshBeforeExpiry = 30 * time.Minute

// DefaultRefreshInterval is the background ticker period.
const DefaultRefreshInterval = 3 * time.Minute

// CredentialPersister updates account_credentials after a successful refresh.
// Implementations must never log tokens.
type CredentialPersister interface {
	PersistRefreshedTokens(accountID, access, refresh, expiresAt string) error
	AccountProvider(accountID string) (provider string, ok bool)
}

// TokenRefreshResult is a non-secret summary of one refresh attempt.
type TokenRefreshResult struct {
	AccountID string
	Provider  string
	Refreshed bool
	Skipped   bool
	Err       string
}


// TokenRefreshStatus is a non-secret snapshot of the last background refresh pass.
type TokenRefreshStatus struct {
	LastRunAt *time.Time           `json:"last_run_at,omitempty"`
	Refreshed int                  `json:"refreshed"`
	Skipped   int                  `json:"skipped"`
	Errors    int                  `json:"errors"`
	Results   []TokenRefreshResult `json:"results"`
}

var (
	tokenRefreshStatusMu sync.RWMutex
	tokenRefreshStatus   = TokenRefreshStatus{Results: []TokenRefreshResult{}}
)

func recordTokenRefreshResults(results []TokenRefreshResult) {
	now := time.Now().UTC()
	st := TokenRefreshStatus{
		LastRunAt: &now,
		Results:   append([]TokenRefreshResult(nil), results...),
	}
	for _, r := range results {
		switch {
		case r.Err != "":
			st.Errors++
		case r.Refreshed:
			st.Refreshed++
		case r.Skipped:
			st.Skipped++
		}
	}
	tokenRefreshStatusMu.Lock()
	tokenRefreshStatus = st
	tokenRefreshStatusMu.Unlock()
}

// GetTokenRefreshStatus returns a copy of the last loop snapshot (no secrets).
func GetTokenRefreshStatus() TokenRefreshStatus {
	tokenRefreshStatusMu.RLock()
	defer tokenRefreshStatusMu.RUnlock()
	out := tokenRefreshStatus
	if out.LastRunAt != nil {
		t := *out.LastRunAt
		out.LastRunAt = &t
	}
	out.Results = append([]TokenRefreshResult(nil), tokenRefreshStatus.Results...)
	return out
}
// RefreshAccountTokens refreshes claude/codex/antigravity accounts that have a refresh_token
// in the credential cache when expiry is near (or unknown). Updates cache + DB.
func RefreshAccountTokens(persist CredentialPersister) []TokenRefreshResult {
	snaps := ListCachedCredentialSnapshots()
	var results []TokenRefreshResult
	now := time.Now()
	for _, snap := range snaps {
		provider, ok := "", false
		if persist != nil {
			provider, ok = persist.AccountProvider(snap.AccountID)
		}
		if !ok || (provider != "claude" && provider != "codex" && provider != "antigravity") {
			continue
		}
		res := TokenRefreshResult{AccountID: snap.AccountID, Provider: provider}
		if !needsRefresh(snap.ExpiresAt, now) {
			res.Skipped = true
			results = append(results, res)
			continue
		}
		rt := CachedRefreshToken(snap.AccountID)
		if rt == "" {
			res.Skipped = true
			results = append(results, res)
			continue
		}
		access, refresh, expiresIn, err := refreshProviderToken(provider, rt)
		if err != nil {
			res.Err = err.Error()
			log.Printf("token refresh: account=%s provider=%s failed: %v", snap.AccountID, provider, err)
			results = append(results, res)
			continue
		}
		expiresAt := ""
		if expiresIn > 0 {
			expiresAt = now.Add(time.Duration(expiresIn) * time.Second).UTC().Format(time.RFC3339)
		}
		extra := snap.ExtraJSON
		SetAccountCredential(snap.AccountID, access, refresh, extra, expiresAt)
		HydrateRuntimeFromAccount(snap.AccountID, provider)
		if persist != nil {
			if err := persist.PersistRefreshedTokens(snap.AccountID, access, refresh, expiresAt); err != nil {
				res.Err = err.Error()
				log.Printf("token refresh: account=%s provider=%s persist failed: %v", snap.AccountID, provider, err)
				results = append(results, res)
				continue
			}
		}
		res.Refreshed = true
		log.Printf("token refresh: account=%s provider=%s refreshed expires_in=%ds", snap.AccountID, provider, expiresIn)
		results = append(results, res)
	}
	return results
}

func needsRefresh(expiresAt string, now time.Time) bool {
	expiresAt = strings.TrimSpace(expiresAt)
	if expiresAt == "" {
		// Unknown expiry: refresh on schedule so tokens do not silently die.
		return true
	}
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, expiresAt)
	}
	if err != nil {
		return true
	}
	return !t.After(now.Add(RefreshBeforeExpiry))
}

func refreshProviderToken(provider, refreshToken string) (access, refresh string, expiresIn int64, err error) {
	switch provider {
	case "claude":
		return claudeRefreshWithToken(refreshToken)
	case "codex":
		return codexRefreshWithToken(refreshToken)
	case "antigravity":
		return antigravityRefreshWithToken(refreshToken)
	default:
		return "", "", 0, fmt.Errorf("unsupported provider %q", provider)
	}
}

func claudeRefreshWithToken(rt string) (string, string, int64, error) {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": rt,
		"client_id":     claudeOAuthClientID,
	})
	req, err := http.NewRequest(http.MethodPost, claudeOAuthTokenURL, bytes.NewReader(body))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("refresh failed: status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var tok claudeTokenResponse
	if err := json.Unmarshal(raw, &tok); err != nil {
		return "", "", 0, err
	}
	if tok.AccessToken == "" {
		return "", "", 0, fmt.Errorf("refresh returned empty access_token")
	}
	newRefresh := tok.RefreshToken
	if newRefresh == "" {
		newRefresh = rt
	}
	claudeSetRuntime(tok.AccessToken, newRefresh, tok.ExpiresIn)
	return tok.AccessToken, newRefresh, tok.ExpiresIn, nil
}

func codexRefreshWithToken(rt string) (string, string, int64, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", rt)
	form.Set("client_id", codexOAuthClientID)
	form.Set("scope", codexRefreshScopes)
	req, err := http.NewRequest(http.MethodPost, codexOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", codexCLIUserAgent)
	req.Header.Set("originator", codexOriginator)
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("codex token refresh status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var out codexTokenResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", 0, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return "", "", 0, fmt.Errorf("codex token refresh returned empty access_token")
	}
	newRefresh := out.RefreshToken
	if newRefresh == "" {
		newRefresh = rt
	}
	codexSetRuntime(out.AccessToken, newRefresh, loadCodexAccountID(), out.ExpiresIn)
	return out.AccessToken, newRefresh, out.ExpiresIn, nil
}


// ForceRefreshAccount refreshes one account immediately, ignoring the expiry window.
// Reuses the same provider refresh + persist path as the background loop.
func ForceRefreshAccount(accountID string, persist CredentialPersister) TokenRefreshResult {
	accountID = strings.TrimSpace(accountID)
	res := TokenRefreshResult{AccountID: accountID}
	provider, ok := "", false
	if persist != nil {
		provider, ok = persist.AccountProvider(accountID)
	}
	if !ok || (provider != "claude" && provider != "codex" && provider != "antigravity") {
		res.Err = "unsupported or unknown account provider for refresh"
		return res
	}
	res.Provider = provider
	rt := CachedRefreshToken(accountID)
	if rt == "" {
		res.Err = "no refresh token cached for account"
		return res
	}
	now := time.Now()
	access, refresh, expiresIn, err := refreshProviderToken(provider, rt)
	if err != nil {
		res.Err = err.Error()
		log.Printf("token refresh: force account=%s provider=%s failed: %v", accountID, provider, err)
		return res
	}
	expiresAt := ""
	if expiresIn > 0 {
		expiresAt = now.Add(time.Duration(expiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	extra := accountExtraJSON(accountID)
	SetAccountCredential(accountID, access, refresh, extra, expiresAt)
	HydrateRuntimeFromAccount(accountID, provider)
	if persist != nil {
		if err := persist.PersistRefreshedTokens(accountID, access, refresh, expiresAt); err != nil {
			res.Err = err.Error()
			log.Printf("token refresh: force account=%s provider=%s persist failed: %v", accountID, provider, err)
			return res
		}
	}
	res.Refreshed = true
	log.Printf("token refresh: force account=%s provider=%s refreshed expires_in=%ds", accountID, provider, expiresIn)
	return res
}
// StartTokenRefreshLoop runs RefreshAccountTokens on an interval until stop is closed.
func StartTokenRefreshLoop(stop <-chan struct{}, interval time.Duration, persist CredentialPersister) {
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	go func() {
		// First pass shortly after boot so near-expiry tokens are renewed early.
		t := time.NewTimer(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				recordTokenRefreshResults(RefreshAccountTokens(persist))
				t.Reset(interval)
			}
		}
	}()
	log.Printf("token refresh: background ticker started interval=%s before_expiry=%s", interval, RefreshBeforeExpiry)
}