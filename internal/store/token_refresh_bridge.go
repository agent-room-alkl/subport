package store

import (
	"strings"

	"github.com/agent-room-alkl/subport/internal/model"
)

// TokenRefreshBridge adapts Store to gateway.CredentialPersister.
type TokenRefreshBridge struct {
	Store *Store
}

func (b TokenRefreshBridge) PersistRefreshedTokens(accountID, access, refresh, expiresAt string) error {
	patch := CredentialPatch{
		AccessToken:  &access,
		RefreshToken: &refresh,
		ExpiresAt:    &expiresAt,
	}
	_, err := b.Store.UpsertCredential(accountID, patch)
	return err
}

func (b TokenRefreshBridge) AccountProvider(accountID string) (string, bool) {
	a, err := b.Store.AccountByID(accountID)
	if err != nil {
		return "", false
	}
	p := strings.ToLower(strings.TrimSpace(a.Provider))
	return p, p != ""
}

// Ensure model import retained if needed for docs.
var _ = model.Account{}