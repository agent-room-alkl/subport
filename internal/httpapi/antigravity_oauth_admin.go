package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/agent-room-alkl/subport/internal/gateway"
	"github.com/agent-room-alkl/subport/internal/store"
)

// wireAntigravityOAuthPersist registers the credential upsert hook used by the
// localhost:8085 callback listener and exchange API. Secrets are never logged.
func (s *Server) wireAntigravityOAuthPersist() {
	gateway.AntigravityOAuthPersist = func(accountID string, info *gateway.AntigravityOAuthTokenInfo) error {
		if info == nil {
			return nil
		}
		acct, err := s.Store.AccountByID(accountID)
		if err != nil {
			return err
		}
		if !strings.EqualFold(acct.Provider, "antigravity") {
			return errNotAntigravity
		}
		patch := store.CredentialPatch{}
		if info.AccessToken != "" {
			at := info.AccessToken
			patch.AccessToken = &at
		}
		if info.RefreshToken != "" {
			rt := info.RefreshToken
			patch.RefreshToken = &rt
		}
		if info.ExpiresAt != "" {
			exp := info.ExpiresAt
			patch.ExpiresAt = &exp
		}
		extra := map[string]any{}
		if cur, cerr := s.Store.GetCredential(accountID); cerr == nil && cur.ExtraJSON != "" {
			_ = json.Unmarshal([]byte(cur.ExtraJSON), &extra)
		}
		if info.ProjectID != "" {
			extra["project_id"] = info.ProjectID
		}
		if info.Email != "" {
			extra["email"] = info.Email
		}
		if len(extra) > 0 {
			b, _ := json.Marshal(extra)
			es := string(b)
			patch.ExtraJSON = &es
		}
		cred, err := s.Store.UpsertCredential(accountID, patch)
		if err != nil {
			return err
		}
		gateway.SetAccountCredential(accountID, cred.AccessToken, cred.RefreshToken, cred.ExtraJSON, cred.ExpiresAt)
		gateway.HydrateRuntimeFromAccount(accountID, acct.Provider)
		_ = s.Store.SetAccountHealthy(accountID, true)
		if s.Sched != nil {
			s.Sched.SetAccountHealth(accountID, true)
		}
		return nil
	}
}

var errNotAntigravity = &simpleError{msg: "account provider is not antigravity"}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

func parseAccountAntigravityOAuthPath(rest, suffix string) (accountID string, ok bool) {
	const prefix = "accounts/"
	const mid = "/antigravity/oauth/"
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

func (s *Server) requireAntigravityAccount(w http.ResponseWriter, id string) bool {
	acct, err := s.Store.AccountByID(id)
	if err != nil {
		fail(w, http.StatusNotFound, "account not found")
		return false
	}
	if !strings.EqualFold(acct.Provider, "antigravity") {
		fail(w, http.StatusBadRequest, "account provider must be antigravity")
		return false
	}
	return true
}

func (s *Server) handleAntigravityOAuthStart(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireAntigravityAccount(w, accountID) {
		return
	}
	result, err := gateway.StartAntigravityOAuth(accountID)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOut(w, result)
}

func (s *Server) handleAntigravityOAuthExchange(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireAntigravityAccount(w, accountID) {
		return
	}
	var in struct {
		SessionID   string `json:"session_id"`
		State       string `json:"state"`
		Code        string `json:"code"`
		CallbackURL string `json:"callback_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if sid := strings.TrimSpace(in.SessionID); sid != "" {
		if owner := gateway.AntigravityOAuthSessionAccount(sid); owner != "" && owner != accountID {
			fail(w, http.StatusBadRequest, "session does not belong to this account")
			return
		}
	}
	info, err := gateway.ExchangeAntigravityOAuth(in.SessionID, in.State, in.Code, in.CallbackURL)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	out := map[string]any{
		"ok":          true,
		"email":       info.Email,
		"project_id":  info.ProjectID,
		"expires_at":  info.ExpiresAt,
		"expires_in":  info.ExpiresIn,
		"credentials": s.Store.PublicCredentialStatus(accountID),
	}
	jsonOut(w, out)
}

func (s *Server) handleAntigravityOAuthStatus(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireAntigravityAccount(w, accountID) {
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID == "" {
		fail(w, http.StatusBadRequest, "session_id required")
		return
	}
	st := gateway.PollAntigravityOAuth(sessionID)
	out := map[string]any{
		"session_id": st.SessionID,
		"done":       st.Done,
		"ok":         st.OK,
		"error":      st.Error,
		"email":      st.Email,
		"project_id": st.ProjectID,
		"expires_at": st.ExpiresAt,
		"applied":    st.Applied,
	}
	if st.Done && st.OK {
		out["credentials"] = s.Store.PublicCredentialStatus(accountID)
	}
	jsonOut(w, out)
}