package store_test

import (
	"path/filepath"
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/store"
)

func TestCredentialRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cred-roundtrip.json")
	s, err := store.OpenStore(path, "http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()

	acct := model.Account{
		ID: "acct-test-1", Name: "Test Claude", Provider: "claude",
		BaseURL: "https://api.anthropic.com", Priority: 1, Healthy: true,
	}
	if err := s.UpsertAccount(acct); err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}

	access := "fake-access-token-aaa"
	refresh := "fake-refresh-token-bbb"
	extra := `{"chatgpt_account_id":"acct_fake_123"}`
	c, err := s.UpsertCredential(acct.ID, store.CredentialPatch{
		AccessToken:  &access,
		RefreshToken: &refresh,
		ExtraJSON:    &extra,
	})
	if err != nil {
		t.Fatalf("UpsertCredential: %v", err)
	}
	if c.AccessToken != access || c.RefreshToken != refresh {
		t.Fatalf("stored tokens mismatch")
	}
	if c.UpdatedAt == "" {
		t.Fatal("expected updated_at")
	}

	got, err := s.GetCredential(acct.ID)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if got.AccessToken != access || got.RefreshToken != refresh || got.ExtraJSON != extra {
		t.Fatalf("roundtrip mismatch")
	}

	pub := s.PublicCredentialStatus(acct.ID)
	if pub["has_access_token"] != true || pub["has_refresh_token"] != true {
		t.Fatalf("public status flags: %#v", pub)
	}
	if _, ok := pub["access_token"]; ok {
		t.Fatal("public status must not include access_token")
	}
	if _, ok := pub["refresh_token"]; ok {
		t.Fatal("public status must not include refresh_token")
	}
	extraOut, _ := pub["extra"].(map[string]any)
	if extraOut["chatgpt_account_id"] != "acct_fake_123" {
		t.Fatalf("extra chatgpt_account_id: %#v", pub["extra"])
	}

	// Empty string leave unchanged
	empty := ""
	newAccess := "fake-access-token-ccc"
	c2, err := s.UpsertCredential(acct.ID, store.CredentialPatch{
		AccessToken:  &newAccess,
		RefreshToken: nil, // leave
	})
	if err != nil {
		t.Fatalf("UpsertCredential patch: %v", err)
	}
	if c2.AccessToken != newAccess {
		t.Fatalf("access not updated")
	}
	if c2.RefreshToken != refresh {
		t.Fatalf("refresh should be unchanged, got %q", c2.RefreshToken)
	}

	// Explicit clear
	c3, err := s.UpsertCredential(acct.ID, store.CredentialPatch{RefreshToken: &empty})
	if err != nil {
		t.Fatalf("clear refresh: %v", err)
	}
	if c3.RefreshToken != "" {
		t.Fatal("refresh should be cleared")
	}

	routes := s.ListModelRoutes()
	if len(routes) < 2 {
		t.Fatalf("expected seeded model routes, got %d", len(routes))
	}
	foundClaude, foundCodex := false, false
	for _, r := range routes {
		if r.ID == "route-claude" && r.Provider == "claude" {
			foundClaude = true
		}
		if r.ID == "route-codex" && r.Provider == "codex" {
			foundCodex = true
		}
	}
	if !foundClaude || !foundCodex {
		t.Fatalf("missing default routes: %#v", routes)
	}

	if err := s.DeleteCredential(acct.ID); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if _, err := s.GetCredential(acct.ID); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestProxiesChannelsCRUD(t *testing.T) {
	dir := t.TempDir()
	s, err := store.OpenStore(filepath.Join(dir, "crud.json"), "http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()

	p := model.Proxy{ID: "proxy-1", Name: "US", Type: "http", URL: "http://127.0.0.1:8888", Enabled: true}
	if err := s.UpsertProxy(p); err != nil {
		t.Fatalf("UpsertProxy: %v", err)
	}
	if len(s.ListProxies()) != 1 {
		t.Fatal("expected 1 proxy")
	}

	ch := model.Channel{ID: "chan-1", Name: "Claude pool", Provider: "claude", ModelsJSON: `["claude-sonnet-4-5"]`, Enabled: true, Priority: 1}
	if err := s.UpsertChannel(ch); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := s.UpsertChannelAccount(model.ChannelAccount{ChannelID: "chan-1", AccountID: "acct-claude-1", Priority: 1}); err != nil {
		t.Fatalf("UpsertChannelAccount: %v", err)
	}
	if len(s.ListChannelAccounts("chan-1")) != 1 {
		t.Fatal("expected 1 mapping")
	}
}
