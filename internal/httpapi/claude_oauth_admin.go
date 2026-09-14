package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"crypto/rand"
	"encoding/hex"

	"github.com/agent-room-alkl/subport/internal/gateway"
	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/store"
)

func parseAccountClaudeOAuthPath(rest, suffix string) (accountID string, ok bool) {
	const prefix = "accounts/"
	const mid = "/claude/oauth/"
	if !strings.HasPrefix(rest, prefix) || !strings.Contains(rest, mid) {
		return "", false
	}
	if !strings.HasSuffix(rest, mid+suffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(rest, prefix), mid+suffix)
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func (s *Server) requireClaudeAccount(w http.ResponseWriter, id string) bool {
	acct, err := s.Store.AccountByID(id)
	if err != nil {
		fail(w, http.StatusNotFound, "account not found")
		return false
	}
	if !strings.EqualFold(acct.Provider, "claude") {
		fail(w, http.StatusBadRequest, "account provider must be claude")
		return false
	}
	return true
}

func claudeExchangeFailMessage(code string, upstreamStatus int) string {
	switch code {
	case "subscription_required":
		return "本次组织/授权上下文缺少 Claude Code 所需的 Pro/Max 订阅权限（不是 sessionKey 过期）。请换有 Pro/Max 的组织或账号，或升级后再授权"
	case "authorization_denied":
		return "Claude 拒绝了此次授权（组织/权限访问问题），不是 sessionKey 过期。请检查组织权限设置后重试"
	case "session_stale_relogin":
		if upstreamStatus > 0 {
			return "Claude API 拒绝了这份 sessionKey（HTTP " + itoa(upstreamStatus) + "）。网页在线不代表粘贴串可用，请刷新后重新复制完整 sessionKey"
		}
		return "Claude API 拒绝了这份 sessionKey。网页在线不代表粘贴串可用，请刷新后重新复制完整 sessionKey"
	case "cloudflare":
		return "Cloudflare 拦截了交换请求，请稍后重试"
	case "rate_limited":
		return "上游限流 (429)，请稍后重试"
	case "helper_unavailable":
		return "服务器缺少 Claude OAuth 交换组件（Python/curl_cffi/交换脚本），请重新发布完整镜像"
	default:
		return "Claude 授权交换失败"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func shortExchangeID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Server) handleClaudeOAuthExchange(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireClaudeAccount(w, accountID) {
		return
	}
	var in struct {
		SessionKey string `json:"session_key"`
		OrgUUID    string `json:"org_uuid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	sessionKey := strings.TrimSpace(in.SessionKey)
	if sessionKey == "" {
		fail(w, http.StatusBadRequest, "session_key required")
		return
	}

	exchangeID := shortExchangeID()
	fullCookie := ""
	if cred, cerr := s.Store.GetCredential(accountID); cerr == nil {
		candidate := model.CookieFromExtraJSON(cred.ExtraJSON)
		// Never combine the entered sessionKey with a different row/account's
		// browser Cookie. Exact value equality is the isolation boundary.
		if model.SessionKeyFromCookieHeader(candidate) == sessionKey {
			fullCookie = candidate
		}
	}
	info, err := gateway.ExchangeClaudeSessionKeyWithCookie(sessionKey, strings.TrimSpace(in.OrgUUID), fullCookie)
	if err != nil {
		code := "exchange_failed"
		step := "unknown"
		upstreamStatus := 0
		if ce, ok := err.(*gateway.ClaudeExchangeError); ok && ce != nil {
			code = ce.Code
			if ce.Step != "" {
				step = ce.Step
			}
			upstreamStatus = ce.UpstreamStatus
		}
		status := http.StatusBadRequest
		switch code {
		case "cloudflare":
			status = http.StatusBadGateway
		case "rate_limited":
			status = http.StatusTooManyRequests
		case "subscription_required":
			status = http.StatusForbidden
		case "authorization_denied":
			status = http.StatusForbidden
		case "session_stale_relogin":
			status = http.StatusUnauthorized
		}
		log.Printf("claude oauth exchange failed id=%s account=%s code=%s step=%s upstream_status=%d key_len=%d",
			exchangeID, accountID, code, step, upstreamStatus, len(sessionKey))
		// Desensitized diagnostics only — never echo secrets or upstream bodies.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":           claudeExchangeFailMessage(code, upstreamStatus),
			"exchange_id":     exchangeID,
			"code":            code,
			"step":            step,
			"upstream_status": upstreamStatus,
		})
		return
	}
	log.Printf("claude oauth exchange ok id=%s account=%s", exchangeID, accountID)

	patch := store.CredentialPatch{}
	at := info.AccessToken
	patch.AccessToken = &at
	if info.RefreshToken != "" {
		rt := info.RefreshToken
		patch.RefreshToken = &rt
	}
	if info.ExpiresAt != "" {
		exp := info.ExpiresAt
		patch.ExpiresAt = &exp
	}
	cred, err := s.Store.UpsertCredential(accountID, patch)
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not save credentials")
		return
	}
	gateway.SetAccountCredential(accountID, cred.AccessToken, cred.RefreshToken, cred.ExtraJSON, cred.ExpiresAt)
	gateway.HydrateRuntimeFromAccount(accountID, "claude")

	// Mark healthy and clear cooldown / 429 counters in DB when possible.
	_ = s.Store.ApplyAccountHealth(accountID, true, "", "", 0, 0, 0)
	_ = s.Store.SetAccountHealthy(accountID, true)
	if s.Sched != nil {
		s.Sched.SetAccountHealth(accountID, true)
	}

	jsonOut(w, map[string]any{
		"ok":          true,
		"message":     "Claude 授权成功",
		"expires_at":  info.ExpiresAt,
		"expires_in":  info.ExpiresIn,
		"credentials": s.Store.PublicCredentialStatus(accountID),
	})
}

func parseAccountClaudeCookiePath(rest string) (accountID string, ok bool) {
	const prefix = "accounts/"
	const suffix = "/claude/cookie"
	if !strings.HasPrefix(rest, prefix) || !strings.HasSuffix(rest, suffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(rest, prefix), suffix)
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func parseAccountClaudeIdentityPath(rest string) (accountID string, ok bool) {
	const prefix = "accounts/"
	const suffix = "/claude/identity"
	if !strings.HasPrefix(rest, prefix) || !strings.HasSuffix(rest, suffix) {
		return "", false
	}
	id := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(rest, prefix), suffix), "/")
	return id, id != "" && !strings.Contains(id, "/")
}

func (s *Server) resolveAndStoreClaudeCookieIdentity(accountID string, acct model.Account, cred model.AccountCredential) (model.ClaudeCookieIdentity, error) {
	// Always clear the previous identity before resolving a newly pasted Cookie;
	// stale email metadata is more dangerous than showing "unverified".
	extra := model.MergeClaudeCookieIdentity(cred.ExtraJSON, model.ClaudeCookieIdentity{})
	identity, identityErr := gateway.FetchClaudeCookieIdentity(acct, model.CookieFromExtraJSON(cred.ExtraJSON))
	if identityErr == nil {
		extra = model.MergeClaudeCookieIdentity(extra, identity)
	}
	updated, err := s.Store.UpsertCredential(accountID, store.CredentialPatch{ExtraJSON: &extra})
	if err != nil {
		return model.ClaudeCookieIdentity{}, err
	}
	gateway.SetAccountCredential(accountID, updated.AccessToken, updated.RefreshToken, updated.ExtraJSON, updated.ExpiresAt)
	return identity, identityErr
}

func (s *Server) handleClaudeCookieIdentity(w http.ResponseWriter, _ *http.Request, accountID string) {
	if !s.requireClaudeAccount(w, accountID) {
		return
	}
	acct, _ := s.Store.AccountByID(accountID)
	cred, err := s.Store.GetCredential(accountID)
	if err != nil || !model.CredentialHasCookie(cred) {
		fail(w, http.StatusBadRequest, "account has no cookie")
		return
	}
	identity, err := s.resolveAndStoreClaudeCookieIdentity(accountID, acct, cred)
	if err != nil {
		jsonOut(w, map[string]any{
			"ok": false, "identity_verified": false,
			"message": "无法核对 Cookie 身份；Cookie 已保留，请检查 Cloudflare 或重新粘贴",
		})
		return
	}
	jsonOut(w, map[string]any{
		"ok": true, "identity_verified": true, "identity": identity,
		"message": "Cookie 身份核对成功",
	})
}

func (s *Server) handleClaudeCookieSave(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireClaudeAccount(w, accountID) {
		return
	}
	var in struct {
		Cookie string `json:"cookie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	cookie := strings.TrimSpace(in.Cookie)
	if cookie == "" {
		fail(w, http.StatusBadRequest, "cookie required")
		return
	}
	cred, err := s.Store.SetAccountCookie(accountID, cookie)
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not save cookie")
		return
	}
	acct, _ := s.Store.AccountByID(accountID)
	identity, identityErr := s.resolveAndStoreClaudeCookieIdentity(accountID, acct, cred)
	log.Printf("claude cookie saved account=%s cookie_len=%d", accountID, len(cookie))
	out := map[string]any{
		"ok":                true,
		"message":           "Cookie 已保存，身份已核对",
		"has_cookie":        true,
		"credentials":       s.Store.PublicCredentialStatus(accountID),
		"identity_verified": identityErr == nil,
	}
	if identityErr == nil {
		out["identity"] = identity
	} else {
		out["message"] = "Cookie 已保存，但暂时无法核对身份"
	}
	jsonOut(w, out)
}

func (s *Server) handleClaudeCookieClear(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireClaudeAccount(w, accountID) {
		return
	}
	cred, err := s.Store.ClearAccountCookie(accountID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not clear cookie")
		return
	}
	extra := model.MergeClaudeCookieIdentity(cred.ExtraJSON, model.ClaudeCookieIdentity{})
	cred, err = s.Store.UpsertCredential(accountID, store.CredentialPatch{ExtraJSON: &extra})
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not clear cookie identity")
		return
	}
	gateway.SetAccountCredential(accountID, cred.AccessToken, cred.RefreshToken, cred.ExtraJSON, cred.ExpiresAt)
	log.Printf("claude cookie cleared account=%s", accountID)
	jsonOut(w, map[string]any{
		"ok":          true,
		"message":     "Cookie 已清除",
		"has_cookie":  false,
		"credentials": s.Store.PublicCredentialStatus(accountID),
	})
}
