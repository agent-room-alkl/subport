package store_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
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

func TestChannelsCRUD(t *testing.T) {
	dir := t.TempDir()
	s, err := store.OpenStore(filepath.Join(dir, "crud.json"), "http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()

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

func TestAccountCookieRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s, err := store.OpenStore(filepath.Join(dir, "cookie.json"), "http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()

	acct := model.Account{
		ID: "acct-cookie-1", Name: "Cookie Claude", Provider: "claude",
		BaseURL: "https://api.anthropic.com", Priority: 1, Healthy: true,
	}
	if err := s.UpsertAccount(acct); err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}

	// Preserve unrelated extra fields when setting cookie.
	extra := `{"chatgpt_account_id":"acct_keep","project_id":"proj_keep"}`
	if _, err := s.UpsertCredential(acct.ID, store.CredentialPatch{ExtraJSON: &extra}); err != nil {
		t.Fatalf("seed extra: %v", err)
	}

	fake := "sessionKey=sk-ant-sid01-FAKE; other=1"
	if _, err := s.SetAccountCookie(acct.ID, "  "+fake+"  "); err != nil {
		t.Fatalf("SetAccountCookie: %v", err)
	}
	if !s.HasAccountCookie(acct.ID) {
		t.Fatal("expected HasAccountCookie true")
	}
	got, err := s.GetAccountCookie(acct.ID)
	if err != nil || got != fake {
		t.Fatalf("GetAccountCookie: got %q err=%v", got, err)
	}

	pub := s.PublicCredentialStatus(acct.ID)
	if pub["has_cookie"] != true {
		t.Fatalf("has_cookie: %#v", pub)
	}
	blob, _ := json.Marshal(pub)
	if strings.Contains(string(blob), "sk-ant-sid01-FAKE") || strings.Contains(string(blob), "sessionKey=") {
		t.Fatalf("public status leaked cookie: %s", blob)
	}
	extraOut, _ := pub["extra"].(map[string]any)
	if _, ok := extraOut["cookie"]; ok {
		t.Fatal("extra must not include cookie key")
	}
	if extraOut["chatgpt_account_id"] != "acct_keep" {
		t.Fatalf("chatgpt_account_id lost: %#v", extraOut)
	}

	pa := model.PublicAccount(acct, mustCred(t, s, acct.ID))
	if pa["has_cookie"] != true {
		t.Fatalf("PublicAccount has_cookie: %#v", pa)
	}
	pblob, _ := json.Marshal(pa)
	if strings.Contains(string(pblob), "sk-ant-sid01-FAKE") {
		t.Fatalf("PublicAccount leaked cookie: %s", pblob)
	}

	if _, err := s.ClearAccountCookie(acct.ID); err != nil {
		t.Fatalf("ClearAccountCookie: %v", err)
	}
	if s.HasAccountCookie(acct.ID) {
		t.Fatal("expected HasAccountCookie false after clear")
	}
	pub2 := s.PublicCredentialStatus(acct.ID)
	if pub2["has_cookie"] != false {
		t.Fatalf("has_cookie after clear: %#v", pub2)
	}
	extra2, _ := pub2["extra"].(map[string]any)
	if extra2["chatgpt_account_id"] != "acct_keep" {
		t.Fatalf("chatgpt_account_id lost after clear: %#v", extra2)
	}
}

func mustCred(t *testing.T, s *store.Store, id string) model.AccountCredential {
	t.Helper()
	c, err := s.GetCredential(id)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	return c
}
