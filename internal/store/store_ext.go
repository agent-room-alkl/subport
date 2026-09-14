package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/agent-room-alkl/subport/internal/model"
)

// ---------------------------------------------------------------- credentials

// GetCredential returns stored tokens for an account. ErrNotFound if none.
// Callers must never log the returned tokens.
func (s *Store) GetCredential(accountID string) (model.AccountCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteGetCredential(accountID)
}

// CredentialPatch is a partial credential update. Nil string pointers mean
// "leave unchanged". A non-nil empty string clears that field.
type CredentialPatch struct {
	AccessToken  *string
	RefreshToken *string
	ExtraJSON    *string
	ExpiresAt    *string
}

// UpsertCredential merges a patch into account_credentials.
func (s *Store) UpsertCredential(accountID string, patch CredentialPatch) (model.AccountCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var access, refresh, extra, expires string
	var setAccess, setRefresh, setExtra, setExpires bool
	if patch.AccessToken != nil {
		setAccess = true
		access = *patch.AccessToken
	}
	if patch.RefreshToken != nil {
		setRefresh = true
		refresh = *patch.RefreshToken
	}
	if patch.ExtraJSON != nil {
		setExtra = true
		extra = *patch.ExtraJSON
	}
	if patch.ExpiresAt != nil {
		setExpires = true
		expires = *patch.ExpiresAt
	}
	if err := s.sqliteUpsertCredential(accountID, access, refresh, extra, expires, setAccess, setRefresh, setExtra, setExpires); err != nil {
		return model.AccountCredential{}, err
	}
	return s.sqliteGetCredential(accountID)
}

// DeleteCredential removes stored tokens for an account.
func (s *Store) DeleteCredential(accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteDeleteCredential(accountID)
}

// SetAccountCookie stores a full browser Cookie header string for one account
// under extra_json (isolated per account_id). cookie must be non-empty after trim.
// Never log cookie contents.
func (s *Store) SetAccountCookie(accountID, cookie string) (model.AccountCredential, error) {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return model.AccountCredential{}, errors.New("cookie required")
	}
	cur, err := s.GetCredential(accountID)
	if err != nil && err != ErrNotFound {
		return model.AccountCredential{}, err
	}
	if err == ErrNotFound {
		cur = model.AccountCredential{AccountID: accountID}
	}
	extra := model.MergeCookieIntoExtraJSON(cur.ExtraJSON, cookie, false)
	return s.UpsertCredential(accountID, CredentialPatch{ExtraJSON: &extra})
}

// ClearAccountCookie removes the cookie key from extra_json for one account.
func (s *Store) ClearAccountCookie(accountID string) (model.AccountCredential, error) {
	cur, err := s.GetCredential(accountID)
	if err != nil {
		if err == ErrNotFound {
			return model.AccountCredential{AccountID: accountID}, nil
		}
		return model.AccountCredential{}, err
	}
	extra := model.MergeCookieIntoExtraJSON(cur.ExtraJSON, "", true)
	return s.UpsertCredential(accountID, CredentialPatch{ExtraJSON: &extra})
}

// GetAccountCookie returns the stored cookie for accountID. ErrNotFound if none.
// Never log the return value.
func (s *Store) GetAccountCookie(accountID string) (string, error) {
	c, err := s.GetCredential(accountID)
	if err != nil {
		return "", err
	}
	cookie := model.CookieFromExtraJSON(c.ExtraJSON)
	if cookie == "" {
		return "", ErrNotFound
	}
	return cookie, nil
}

// HasAccountCookie reports whether a non-empty cookie is stored for accountID.
func (s *Store) HasAccountCookie(accountID string) bool {
	c, err := s.GetCredential(accountID)
	if err != nil {
		return false
	}
	return model.CredentialHasCookie(c)
}

// PublicCredentialStatus returns the admin-safe credential view.
func (s *Store) PublicCredentialStatus(accountID string) map[string]any {
	c, err := s.GetCredential(accountID)
	if err != nil {
		return model.PublicCredentialStatus(model.AccountCredential{AccountID: accountID})
	}
	out := model.PublicCredentialStatus(c)
	if strings.TrimSpace(c.ExtraJSON) != "" {
		var obj map[string]any
		if json.Unmarshal([]byte(c.ExtraJSON), &obj) == nil {
			safe := map[string]any{}
			for k, v := range obj {
				if model.IsSecretExtraKey(k) {
					continue
				}
				safe[k] = v
			}
			out["extra"] = safe
		}
	}
	out["has_cookie"] = model.CredentialHasCookie(c)
	return out
}

// ListCredentialsForBootstrap returns all stored credentials so the gateway
// can hydrate its runtime cache at startup. Never log the result.
func (s *Store) ListCredentialsForBootstrap() ([]model.AccountCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteListCredentials()
}

// AccountByID looks up one account row.
func (s *Store) AccountByID(id string) (model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.sqliteAccountByID(id)
	if err == sql.ErrNoRows {
		return a, ErrNotFound
	}
	return a, err
}

// ---------------------------------------------------------------- channels

func (s *Store) ListChannels() []model.Channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.sqliteListChannels()
	if err != nil || out == nil {
		return []model.Channel{}
	}
	return out
}

func (s *Store) GetChannel(id string) (model.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteGetChannel(id)
}

func (s *Store) UpsertChannel(c model.Channel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		c.ID = NewID("chan")
	}
	return s.sqliteUpsertChannel(c)
}

func (s *Store) DeleteChannel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteDeleteChannel(id)
}

func (s *Store) ListChannelAccounts(channelID string) []model.ChannelAccount {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.sqliteListChannelAccounts(channelID)
	if err != nil || out == nil {
		return []model.ChannelAccount{}
	}
	return out
}

func (s *Store) UpsertChannelAccount(m model.ChannelAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteUpsertChannelAccount(m)
}

func (s *Store) DeleteChannelAccount(channelID, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteDeleteChannelAccount(channelID, accountID)
}

// ---------------------------------------------------------------- model routes

func (s *Store) ListModelRoutes() []model.ModelRoute {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.sqliteListModelRoutes()
	if err != nil || out == nil {
		return []model.ModelRoute{}
	}
	return out
}

func (s *Store) GetModelRoute(id string) (model.ModelRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteGetModelRoute(id)
}

func (s *Store) UpsertModelRoute(r model.ModelRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ID == "" {
		r.ID = NewID("route")
	}
	return s.sqliteUpsertModelRoute(r)
}

func (s *Store) DeleteModelRoute(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteDeleteModelRoute(id)
}

// ApplyAccountHealth persists auto-pause / cooldown counters for an account.
func (s *Store) ApplyAccountHealth(id string, healthy bool, cooldownUntil, lastError string, timeouts, c403, c429 int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteApplyAccountHealth(id, healthy, cooldownUntil, lastError, timeouts, c403, c429)
}

// GetAccount is an alias used by gateway.AccountHealthStore.
func (s *Store) GetAccount(id string) (model.Account, error) {
	return s.AccountByID(id)
}

// AdminSetKeyEnabled toggles a key by id only (admin path, no user scope).
func (s *Store) AdminSetKeyEnabled(keyID string, enabled bool) error {
	return s.sqliteAdminSetKeyEnabled(keyID, enabled)
}

// AdminDeleteKey deletes a key by id only (admin path).
func (s *Store) AdminDeleteKey(keyID string) error {
	return s.sqliteAdminDeleteKey(keyID)
}

// KeyByIDOnly looks up a key without user scoping (admin).
func (s *Store) KeyByIDOnly(keyID string) (model.APIKey, error) {
	k, err := s.sqliteKeyByIDOnly(keyID)
	if err != nil {
		return model.APIKey{}, ErrNotFound
	}
	return k, nil
}

// SetRole updates a user's role ("admin" | "user").
func (s *Store) SetRole(userID, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteSetRole(userID, role)
}

// CountUsers returns total user rows; CountAdmins returns role=admin count.
func (s *Store) CountUsers() int {
	n, err := s.sqliteCountUsersByRole("")
	if err != nil {
		return 0
	}
	return n
}

func (s *Store) CountAdmins() int {
	n, err := s.sqliteCountUsersByRole(model.RoleAdmin)
	if err != nil {
		return 0
	}
	return n
}

// UsageByDay returns last N days of usage aggregates (zeros filled).
func (s *Store) UsageByDay(days int) []UsageDayStat {
	out, err := s.sqliteUsageByDay(days)
	if err != nil || out == nil {
		return []UsageDayStat{}
	}
	return out
}

// UsageByModel returns top models by tokens.
func (s *Store) UsageByModel(limit int) []UsageModelStat {
	out, err := s.sqliteUsageByModel(limit)
	if err != nil || out == nil {
		return []UsageModelStat{}
	}
	return out
}

// ListAvailableModels returns the curated catalog (all rows for admin).
func (s *Store) ListAvailableModels() []model.AvailableModel {
	out, err := s.sqliteListAvailableModels(false)
	if err != nil || out == nil {
		return []model.AvailableModel{}
	}
	return out
}

// ListEnabledAvailableModels returns enabled catalog rows for the user console.
func (s *Store) ListEnabledAvailableModels() []model.AvailableModel {
	out, err := s.sqliteListAvailableModels(true)
	if err != nil || out == nil {
		return []model.AvailableModel{}
	}
	return out
}

// SetAvailableModelEnabled toggles a catalog row.
func (s *Store) SetAvailableModelEnabled(id string, enabled bool) error {
	return s.sqliteSetAvailableModelEnabled(id, enabled)
}

// UpsertAvailableModel inserts or updates a catalog row.
func (s *Store) UpsertAvailableModel(m model.AvailableModel) error {
	return s.sqliteUpsertAvailableModel(m)
}

// UsageSummary returns admin monitoring rollups.
func (s *Store) UsageSummary(topN int) UsageSummary {
	out, err := s.sqliteUsageSummary(topN)
	if err != nil {
		return UsageSummary{
			PerUser:      []UsageUserStat{},
			PerModel:     []UsageModelCostStat{},
			PerUserModel: []UsageUserModelStat{},
		}
	}
	return out
}
