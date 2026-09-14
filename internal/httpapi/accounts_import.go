package httpapi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/agent-room-alkl/subport/internal/gateway"
	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/store"
)

type accountImportRow struct {
	Provider     string `json:"provider"`
	Name         string `json:"name"`
	Label        string `json:"label"`
	Cookie       string `json:"cookie"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	BaseURL      string `json:"base_url"`
	Priority     *int   `json:"priority"`
}

// handleAccountsImport accepts a JSON array of credential rows and creates or
// updates accounts by name+provider. Secrets are never logged.
//
// POST /api/accounts/import
// Body: [{ "provider":"claude", "name":"email@x", "cookie":"...", "access_token":"...", "refresh_token":"..." }]
func (s *Server) handleAccountsImport(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	var rows []accountImportRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		var wrap struct {
			Accounts []accountImportRow `json:"accounts"`
		}
		if err2 := json.Unmarshal(raw, &wrap); err2 != nil || len(wrap.Accounts) == 0 {
			fail(w, http.StatusBadRequest, "expected JSON array of account credential objects")
			return
		}
		rows = wrap.Accounts
	}
	if len(rows) == 0 {
		fail(w, http.StatusBadRequest, "empty import list")
		return
	}
	if len(rows) > 500 {
		fail(w, http.StatusBadRequest, "too many rows (max 500)")
		return
	}

	results := make([]map[string]any, 0, len(rows))
	okN, errN := 0, 0
	for i, row := range rows {
		res := map[string]any{"index": i}
		provider := strings.ToLower(strings.TrimSpace(row.Provider))
		name := strings.TrimSpace(row.Name)
		if name == "" {
			name = strings.TrimSpace(row.Label)
		}
		res["name"] = name
		res["provider"] = provider
		if provider == "" || name == "" {
			res["ok"] = false
			res["error"] = "provider and name are required"
			errN++
			results = append(results, res)
			continue
		}
		switch provider {
		case "claude", "codex", "antigravity", "openai", "mock":
		default:
			res["ok"] = false
			res["error"] = "unsupported provider"
			errN++
			results = append(results, res)
			continue
		}

		acct, err := s.Store.AccountByNameProvider(name, provider)
		created := false
		if err == store.ErrNotFound {
			priority := 1
			if row.Priority != nil {
				priority = *row.Priority
			}
			acct = model.Account{
				ID:       genAccountID(provider),
				Name:     name,
				Provider: provider,
				BaseURL:  importBaseURL(provider, row.BaseURL),
				Priority: priority,
				Healthy:  true,
			}
			if err := s.Store.UpsertAccount(acct); err != nil {
				res["ok"] = false
				res["error"] = "could not create account"
				errN++
				results = append(results, res)
				continue
			}
			created = true
		} else if err != nil {
			res["ok"] = false
			res["error"] = "lookup failed"
			errN++
			results = append(results, res)
			continue
		} else {
			if strings.TrimSpace(row.BaseURL) != "" {
				acct.BaseURL = strings.TrimSpace(row.BaseURL)
			}
			if row.Priority != nil {
				acct.Priority = *row.Priority
			}
			if err := s.Store.UpsertAccount(acct); err != nil {
				res["ok"] = false
				res["error"] = "could not update account"
				errN++
				results = append(results, res)
				continue
			}
		}
		res["account_id"] = acct.ID
		res["created"] = created

		patch := store.CredentialPatch{}
		hasCred := false
		if at := strings.TrimSpace(row.AccessToken); at != "" {
			patch.AccessToken = &at
			hasCred = true
		}
		if rt := strings.TrimSpace(row.RefreshToken); rt != "" {
			patch.RefreshToken = &rt
			hasCred = true
		}
		if hasCred {
			cred, uerr := s.Store.UpsertCredential(acct.ID, patch)
			if uerr != nil {
				res["ok"] = false
				res["error"] = "could not save tokens"
				errN++
				results = append(results, res)
				continue
			}
			gateway.SetAccountCredential(acct.ID, cred.AccessToken, cred.RefreshToken, cred.ExtraJSON, cred.ExpiresAt)
			gateway.HydrateRuntimeFromAccount(acct.ID, acct.Provider)
		}
		if cookie := strings.TrimSpace(row.Cookie); cookie != "" {
			cred, cerr := s.Store.SetAccountCookie(acct.ID, cookie)
			if cerr != nil {
				res["ok"] = false
				res["error"] = "could not save cookie"
				errN++
				results = append(results, res)
				continue
			}
			gateway.SetAccountCredential(acct.ID, cred.AccessToken, cred.RefreshToken, cred.ExtraJSON, cred.ExpiresAt)
			res["has_cookie"] = true
		} else {
			res["has_cookie"] = s.Store.HasAccountCookie(acct.ID)
		}
		pub := s.Store.PublicCredentialStatus(acct.ID)
		res["has_access_token"] = pub["has_access_token"]
		res["has_refresh_token"] = pub["has_refresh_token"]
		res["ok"] = true
		okN++
		results = append(results, res)
		log.Printf("accounts import row=%d account=%s provider=%s created=%v cookie_len=%d at_len=%d rt_len=%d",
			i, acct.ID, provider, created, len(strings.TrimSpace(row.Cookie)), len(strings.TrimSpace(row.AccessToken)), len(strings.TrimSpace(row.RefreshToken)))
	}

	if s.Sched != nil {
		s.Sched.ReplaceAccounts(s.Store.Accounts())
	}
	jsonOut(w, map[string]any{
		"ok":       errN == 0,
		"imported": okN,
		"failed":   errN,
		"results":  results,
	})
}

func importBaseURL(provider, supplied string) string {
	if v := strings.TrimSpace(supplied); v != "" {
		return v
	}
	switch provider {
	case "claude":
		return "https://api.anthropic.com"
	case "codex":
		return "https://chatgpt.com"
	case "antigravity":
		return "https://cloudcode-pa.googleapis.com"
	case "openai":
		return "https://api.openai.com"
	default:
		return ""
	}
}
