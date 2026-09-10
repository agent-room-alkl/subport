package store

import (
	"database/sql"
	"encoding/json"
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
				lk := strings.ToLower(k)
				if strings.Contains(lk, "token") || strings.Contains(lk, "secret") || strings.Contains(lk, "password") {
					continue
				}
				safe[k] = v
			}
			out["extra"] = safe
		}
	}
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

// ---------------------------------------------------------------- proxies

func (s *Store) ListProxies() []model.Proxy {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.sqliteListProxies()
	if err != nil || out == nil {
		return []model.Proxy{}
	}
	return out
}

func (s *Store) GetProxy(id string) (model.Proxy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteGetProxy(id)
}

func (s *Store) UpsertProxy(p model.Proxy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == "" {
		p.ID = NewID("proxy")
	}
	return s.sqliteUpsertProxy(p)
}

func (s *Store) DeleteProxy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sqliteDeleteProxy(id)
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

