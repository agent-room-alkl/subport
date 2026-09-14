package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agent-room-alkl/subport/internal/gateway"
	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/store"
)

// adminRoutesExtended handles credentials, channels, routes, and
// account connectivity tests. Called from adminRoutes after the legacy cases.
func (s *Server) adminExtRoutes(w http.ResponseWriter, r *http.Request, rest string) bool {
	switch {
	case rest == "accounts/import" && r.Method == http.MethodPost:
		s.handleAccountsImport(w, r)
		return true

	case rest == "model-aliases" && r.Method == http.MethodGet:
		cfg := gateway.GetModelAliasConfig()
		jsonOut(w, map[string]any{
			"aliases":            cfg.Aliases,
			"fallbacks":          cfg.Fallbacks,
			"max_fallback_tries": cfg.MaxFallbackTries,
			"stored":             s.Store.GetModelAliasesJSON(),
		})
		return true

	case rest == "model-aliases" && (r.Method == http.MethodPut || r.Method == http.MethodPost):
		var cfg gateway.ModelAliasConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			fail(w, http.StatusBadRequest, "bad request")
			return true
		}
		// Normalize a copy first, persist it, and only then publish it to the
		// live process. A failed database write must not return 200 or create a
		// configuration that disappears on restart.
		normalized := gateway.NormalizeModelAliasConfig(cfg)
		b, err := json.Marshal(normalized)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid model alias config")
			return true
		}
		if err := s.Store.SetModelAliasesJSON(string(b)); err != nil {
			fail(w, http.StatusInternalServerError, "could not save model alias config")
			return true
		}
		gateway.SetModelAliasConfig(normalized)
		jsonOut(w, normalized)
		return true

	case rest == "token-refresh/status" && r.Method == http.MethodGet:
		jsonOut(w, gateway.GetTokenRefreshStatus())
		return true

	case strings.HasPrefix(rest, "accounts/") && strings.Contains(rest, "/antigravity/oauth/start") && r.Method == http.MethodPost:
		if id, ok := parseAccountAntigravityOAuthPath(rest, "start"); ok {
			s.handleAntigravityOAuthStart(w, r, id)
			return true
		}

	case strings.HasPrefix(rest, "accounts/") && strings.Contains(rest, "/antigravity/oauth/exchange") && r.Method == http.MethodPost:
		if id, ok := parseAccountAntigravityOAuthPath(rest, "exchange"); ok {
			s.handleAntigravityOAuthExchange(w, r, id)
			return true
		}

	case strings.HasPrefix(rest, "accounts/") && strings.Contains(rest, "/antigravity/oauth/status") && r.Method == http.MethodGet:
		if id, ok := parseAccountAntigravityOAuthPath(rest, "status"); ok {
			s.handleAntigravityOAuthStatus(w, r, id)
			return true
		}

	case strings.HasPrefix(rest, "accounts/") && strings.Contains(rest, "/claude/oauth/exchange") && r.Method == http.MethodPost:
		if id, ok := parseAccountClaudeOAuthPath(rest, "exchange"); ok {
			s.handleClaudeOAuthExchange(w, r, id)
			return true
		}

	case strings.HasPrefix(rest, "accounts/") && strings.HasSuffix(rest, "/claude/cookie") && (r.Method == http.MethodPut || r.Method == http.MethodPost):
		if id, ok := parseAccountClaudeCookiePath(rest); ok {
			s.handleClaudeCookieSave(w, r, id)
			return true
		}

	case strings.HasPrefix(rest, "accounts/") && strings.HasSuffix(rest, "/claude/cookie") && r.Method == http.MethodDelete:
		if id, ok := parseAccountClaudeCookiePath(rest); ok {
			s.handleClaudeCookieClear(w, r, id)
			return true
		}

	case strings.HasPrefix(rest, "accounts/") && strings.HasSuffix(rest, "/claude/identity") && r.Method == http.MethodPost:
		if id, ok := parseAccountClaudeIdentityPath(rest); ok {
			s.handleClaudeCookieIdentity(w, r, id)
			return true
		}

	case strings.HasPrefix(rest, "accounts/") && strings.HasSuffix(rest, "/refresh") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(rest, "accounts/"), "/refresh")
		id = strings.TrimSuffix(id, "/")
		if _, err := s.Store.AccountByID(id); err != nil {
			fail(w, http.StatusNotFound, "account not found")
			return true
		}
		result := gateway.ForceRefreshAccount(id, store.TokenRefreshBridge{Store: s.Store})
		if result.Err != "" && !result.Refreshed {
			// Distinguish missing refresh path / unsupported vs transient failure.
			if strings.Contains(result.Err, "unsupported") || strings.Contains(result.Err, "no refresh token") {
				fail(w, http.StatusBadRequest, result.Err)
				return true
			}
			fail(w, http.StatusBadGateway, result.Err)
			return true
		}
		jsonOut(w, result)
		return true

	// --- account credentials ---
	case strings.HasPrefix(rest, "accounts/") && strings.HasSuffix(rest, "/credentials") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(strings.TrimPrefix(rest, "accounts/"), "/credentials")
		id = strings.TrimSuffix(id, "/")
		if _, err := s.Store.AccountByID(id); err != nil {
			fail(w, http.StatusNotFound, "account not found")
			return true
		}
		jsonOut(w, s.Store.PublicCredentialStatus(id))
		return true

	case strings.HasPrefix(rest, "accounts/") && strings.HasSuffix(rest, "/credentials") && r.Method == http.MethodPut:
		id := strings.TrimSuffix(strings.TrimPrefix(rest, "accounts/"), "/credentials")
		id = strings.TrimSuffix(id, "/")
		if _, err := s.Store.AccountByID(id); err != nil {
			fail(w, http.StatusNotFound, "account not found")
			return true
		}
		var in struct {
			AccessToken       *string `json:"access_token"`
			RefreshToken      *string `json:"refresh_token"`
			ChatGPTAccountID  *string `json:"chatgpt_account_id"`
			ProjectID         *string `json:"project_id"`
			ExpiresAt         *string `json:"expires_at"`
			ExtraJSON         *string `json:"extra_json"`
			ClearAccessToken  bool    `json:"clear_access_token"`
			ClearRefreshToken bool    `json:"clear_refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, http.StatusBadRequest, "bad request")
			return true
		}
		patch := store.CredentialPatch{}
		// Empty string means leave unchanged (per API contract). Explicit
		// clear_* flags wipe a field.
		if in.ClearAccessToken {
			empty := ""
			patch.AccessToken = &empty
		} else if in.AccessToken != nil && *in.AccessToken != "" {
			patch.AccessToken = in.AccessToken
		}
		if in.ClearRefreshToken {
			empty := ""
			patch.RefreshToken = &empty
		} else if in.RefreshToken != nil && *in.RefreshToken != "" {
			patch.RefreshToken = in.RefreshToken
		}
		if in.ExpiresAt != nil {
			patch.ExpiresAt = in.ExpiresAt
		}
		if in.ExtraJSON != nil && *in.ExtraJSON != "" {
			patch.ExtraJSON = in.ExtraJSON
		} else if in.ChatGPTAccountID != nil || in.ProjectID != nil {
			// Merge chatgpt_account_id / project_id into extra_json without wiping other keys.
			cur, _ := s.Store.GetCredential(id)
			extra := map[string]any{}
			if cur.ExtraJSON != "" {
				_ = json.Unmarshal([]byte(cur.ExtraJSON), &extra)
			}
			if in.ChatGPTAccountID != nil {
				if *in.ChatGPTAccountID == "" {
					delete(extra, "chatgpt_account_id")
				} else {
					extra["chatgpt_account_id"] = *in.ChatGPTAccountID
				}
			}
			if in.ProjectID != nil {
				if *in.ProjectID == "" {
					delete(extra, "project_id")
				} else {
					extra["project_id"] = *in.ProjectID
				}
			}
			b, _ := json.Marshal(extra)
			extraStr := string(b)
			patch.ExtraJSON = &extraStr
		}
		cred, err := s.Store.UpsertCredential(id, patch)
		if err != nil {
			fail(w, http.StatusInternalServerError, "could not save credentials")
			return true
		}
		// Sync process cache + provider runtime (never log tokens).
		gateway.SetAccountCredential(id, cred.AccessToken, cred.RefreshToken, cred.ExtraJSON, cred.ExpiresAt)
		if acct, aerr := s.Store.AccountByID(id); aerr == nil {
			gateway.HydrateRuntimeFromAccount(id, acct.Provider)
		}
		jsonOut(w, s.Store.PublicCredentialStatus(id))
		return true

	case strings.HasPrefix(rest, "accounts/") && strings.HasSuffix(rest, "/test") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(rest, "accounts/"), "/test")
		id = strings.TrimSuffix(id, "/")
		acct, err := s.Store.AccountByID(id)
		if err != nil {
			fail(w, http.StatusNotFound, "account not found")
			return true
		}
		// Prefer DB credentials for this account.
		if cred, cerr := s.Store.GetCredential(id); cerr == nil {
			gateway.SetAccountCredential(id, cred.AccessToken, cred.RefreshToken, cred.ExtraJSON, cred.ExpiresAt)
			gateway.HydrateRuntimeFromAccount(id, acct.Provider)
		}
		var input struct {
			Model    string `json:"model"`
			AuthMode string `json:"auth_mode"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil && err != io.EOF {
				fail(w, http.StatusBadRequest, "bad request")
				return true
			}
		}
		result := s.smokeTestAccount(acct, input.Model, input.AuthMode)
		jsonOut(w, result)
		return true

	// --- channels CRUD ---
	case rest == "channels" && r.Method == http.MethodPost:
		var c model.Channel
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			fail(w, http.StatusBadRequest, "bad request")
			return true
		}
		c.Enabled = true
		if err := s.Store.UpsertChannel(c); err != nil {
			fail(w, http.StatusInternalServerError, "could not save channel")
			return true
		}
		jsonOut(w, c)
		return true
	case strings.HasPrefix(rest, "channels/") && strings.Contains(rest, "/accounts") && r.Method == http.MethodGet:
		cid := strings.Split(strings.TrimPrefix(rest, "channels/"), "/")[0]
		jsonOut(w, s.Store.ListChannelAccounts(cid))
		return true
	case strings.HasPrefix(rest, "channels/") && strings.HasSuffix(rest, "/accounts") && r.Method == http.MethodPost:
		cid := strings.TrimSuffix(strings.TrimPrefix(rest, "channels/"), "/accounts")
		var m model.ChannelAccount
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			fail(w, http.StatusBadRequest, "bad request")
			return true
		}
		m.ChannelID = cid
		if err := s.Store.UpsertChannelAccount(m); err != nil {
			fail(w, http.StatusInternalServerError, "could not save mapping")
			return true
		}
		jsonOut(w, m)
		return true
	case strings.HasPrefix(rest, "channels/") && r.Method == http.MethodPut:
		id := strings.TrimPrefix(rest, "channels/")
		if strings.Contains(id, "/") {
			return false
		}
		var c model.Channel
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			fail(w, http.StatusBadRequest, "bad request")
			return true
		}
		c.ID = id
		if err := s.Store.UpsertChannel(c); err != nil {
			fail(w, http.StatusInternalServerError, "could not save channel")
			return true
		}
		jsonOut(w, c)
		return true
	case strings.HasPrefix(rest, "channels/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(rest, "channels/")
		if strings.Contains(id, "/") {
			parts := strings.Split(id, "/")
			if len(parts) == 3 && parts[1] == "accounts" {
				if err := s.Store.DeleteChannelAccount(parts[0], parts[2]); err != nil {
					fail(w, http.StatusNotFound, "mapping not found")
					return true
				}
				w.WriteHeader(http.StatusNoContent)
				return true
			}
			return false
		}
		if err := s.Store.DeleteChannel(id); err != nil {
			fail(w, http.StatusNotFound, "channel not found")
			return true
		}
		w.WriteHeader(http.StatusNoContent)
		return true

	// --- model routes ---
	case rest == "model-routes" && r.Method == http.MethodGet:
		jsonOut(w, s.Store.ListModelRoutes())
		return true
	case rest == "model-routes" && r.Method == http.MethodPost:
		var route model.ModelRoute
		if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
			fail(w, http.StatusBadRequest, "bad request")
			return true
		}
		route.Enabled = true
		if err := s.Store.UpsertModelRoute(route); err != nil {
			fail(w, http.StatusInternalServerError, "could not save route")
			return true
		}
		s.Sched.SetModelRoutes(s.Store.ListModelRoutes())
		jsonOut(w, route)
		return true
	case strings.HasPrefix(rest, "model-routes/") && r.Method == http.MethodPut:
		id := strings.TrimPrefix(rest, "model-routes/")
		var route model.ModelRoute
		if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
			fail(w, http.StatusBadRequest, "bad request")
			return true
		}
		route.ID = id
		if err := s.Store.UpsertModelRoute(route); err != nil {
			fail(w, http.StatusInternalServerError, "could not save route")
			return true
		}
		s.Sched.SetModelRoutes(s.Store.ListModelRoutes())
		jsonOut(w, route)
		return true
	case strings.HasPrefix(rest, "model-routes/") && r.Method == http.MethodDelete:
		if err := s.Store.DeleteModelRoute(strings.TrimPrefix(rest, "model-routes/")); err != nil {
			fail(w, http.StatusNotFound, "route not found")
			return true
		}
		s.Sched.SetModelRoutes(s.Store.ListModelRoutes())
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func (s *Server) smokeTestAccount(acct model.Account, requestedModel, requestedAuthMode string) map[string]any {
	start := time.Now()
	out := map[string]any{
		"account_id": acct.ID,
		"provider":   acct.Provider,
		"ok":         false,
	}
	prov := strings.ToLower(strings.TrimSpace(acct.Provider))
	modelName := strings.TrimSpace(requestedModel)
	if modelName == "" {
		modelName = smokeModelFor(acct.Provider)
	}
	authMode := strings.ToLower(strings.TrimSpace(requestedAuthMode))
	if authMode == "" {
		authMode = "auto"
	}
	out["model"] = modelName

	// Claude: prefer cookie-based claude.ai path when this account has a cookie.
	// Isolation: only this account's cookie / token — never another account's.
	if prov == "claude" {
		hasCookie := s.Store.HasAccountCookie(acct.ID) || gateway.HasAccountCookie(acct.ID)
		hasOAuth := gateway.HasAccountAccessToken(acct.ID)
		if authMode != "auto" && authMode != "cookie" && authMode != "oauth" {
			out["path"] = "none"
			out["error"] = "invalid auth_mode; expected auto, cookie, or oauth"
			return out
		}
		if authMode == "cookie" && !hasCookie {
			out["path"] = "cookie"
			out["error"] = "account has no cookie / 账号未配置 Cookie"
			return out
		}
		if authMode == "oauth" && !hasOAuth {
			out["path"] = "oauth"
			out["error"] = "account has no access_token / 账号未配置 OAuth access_token"
			return out
		}
		if hasCookie && authMode != "oauth" {
			cookie := gateway.AccountCookie(acct.ID)
			if cookie == "" {
				if c, err := s.Store.GetAccountCookie(acct.ID); err == nil {
					cookie = c
					// Hydrate cache ExtraJSON for subsequent calls without logging secrets.
					if cred, gerr := s.Store.GetCredential(acct.ID); gerr == nil {
						gateway.SetAccountCredential(acct.ID, cred.AccessToken, cred.RefreshToken, cred.ExtraJSON, cred.ExpiresAt)
					}
				}
			}
			res := gateway.ClaudeCookieSmokeTest(acct, cookie, modelName)
			out["path"] = "cookie"
			out["latency_ms"] = res.LatencyMS
			out["ok"] = res.OK
			out["http_status"] = res.HTTPStatus
			if res.OrgUUID != "" {
				out["org_uuid"] = res.OrgUUID
			}
			if res.MessageZH != "" {
				out["message_zh"] = res.MessageZH
			}
			if res.MessageEN != "" {
				out["message_en"] = res.MessageEN
			}
			if res.ResetsAt != "" {
				out["resets_at"] = res.ResetsAt
			}
			if !res.OK {
				out["error"] = res.Error
			}
			return out
		}
		if !hasOAuth {
			out["latency_ms"] = time.Since(start).Milliseconds()
			out["path"] = "none"
			out["error"] = "account has no cookie or access_token / 账号未配置 Cookie 或 access_token"
			out["message_zh"] = "账号未配置 Cookie 或 access_token"
			out["message_en"] = "account has no cookie or access_token"
			return out
		}
		out["path"] = "oauth"
	} else if prov == "codex" || prov == "antigravity" {
		if !gateway.HasAccountAccessToken(acct.ID) {
			out["latency_ms"] = time.Since(start).Milliseconds()
			out["error"] = "account has no credentials / 账号未配置凭证"
			return out
		}
	}

	req := gateway.ChatRequest{
		Model:  modelName,
		Stream: false,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{{Role: "user", Content: "ping"}},
	}
	_, err := gateway.CallUpstream(acct, req)
	out["latency_ms"] = time.Since(start).Milliseconds()
	if err != nil {
		info := gateway.MapUpstreamError(err)
		out["error"] = info.Message
		out["message_zh"] = info.MessageZH
		out["message_en"] = info.MessageEN
		if info.ResetsAt != "" {
			out["resets_at"] = info.ResetsAt
		}
		if info.HTTPStatus != 0 {
			out["http_status"] = info.HTTPStatus
		}
		if info.Kind != "" {
			out["kind"] = string(info.Kind)
		}
		return out
	}
	out["ok"] = true
	return out
}

func smokeModelFor(provider string) string {
	switch strings.ToLower(provider) {
	case "claude":
		return "claude-haiku-4-5-20251001"
	case "codex":
		return "gpt-5.5"
	case "antigravity":
		return "gemini-2.5-flash"
	case "openai":
		return "gpt-4o-mini"
	default:
		return "gpt-4o-mini"
	}
}

func (s *Server) adminListChannels(w http.ResponseWriter, r *http.Request) {
	stored := s.Store.ListChannels()
	if len(stored) == 0 {
		// Fallback stub so the legacy admin UI keeps working before channels are configured.
		jsonOut(w, []map[string]any{
			{"id": "openai-main", "name": "OpenAI primary", "provider": "openai", "priority": 1, "group": "default", "models": []string{"gpt-4o", "gpt-4o-mini"}, "accounts": 2, "health": 1.0, "enabled": true, "status": "healthy"},
			{"id": "anthropic-backup", "name": "Anthropic backup", "provider": "anthropic", "priority": 2, "group": "default", "models": []string{"claude-sonnet-4"}, "accounts": 1, "health": 1.0, "enabled": true, "status": "healthy"},
			{"id": "claude-subscription", "name": "Claude.ai subscription", "provider": "claude", "priority": 1, "group": "default", "models": []string{"claude-sonnet-4-20250514", "claude-opus-4-20250514"}, "accounts": 1, "health": 1.0, "enabled": true, "status": "healthy"},
		})
		return
	}
	out := make([]map[string]any, 0, len(stored))
	for _, c := range stored {
		models := []string{}
		_ = json.Unmarshal([]byte(c.ModelsJSON), &models)
		maps := s.Store.ListChannelAccounts(c.ID)
		status := "healthy"
		if !c.Enabled {
			status = "paused"
		}
		out = append(out, map[string]any{
			"id": c.ID, "name": c.Name, "provider": c.Provider, "priority": c.Priority,
			"group": c.GroupName, "models": models, "accounts": len(maps),
			"health": 1.0, "enabled": c.Enabled, "status": status,
		})
	}
	jsonOut(w, out)
}

// applyRuntimeCredentialPresence ORs DB-derived PublicAccount token flags with
// gateway runtime / env / cache knowledge so bootstrap-loaded tokens show up in
// admin list responses. Never copies secret values.
func applyRuntimeCredentialPresence(pub map[string]any, a model.Account) {
	rt := gateway.CredentialPresenceFor(a.ID, a.Provider)
	if rt.HasAccessToken {
		pub["has_access_token"] = true
	}
	if rt.HasRefreshToken {
		pub["has_refresh_token"] = true
	}
	if pub["expires_at"] == nil && strings.TrimSpace(rt.ExpiresAt) != "" {
		pub["expires_at"] = rt.ExpiresAt
	}
}
