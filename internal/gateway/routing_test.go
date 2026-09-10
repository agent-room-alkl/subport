package gateway

import (
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
)

func TestProvidersForModel(t *testing.T) {
	routes := []model.ModelRoute{
		{ID: "route-claude", Pattern: "^claude", Provider: "claude", Priority: 1, Enabled: true},
		{ID: "route-codex", Pattern: "^gpt-|^o[0-9]|^codex", Provider: "codex", Priority: 1, Enabled: true},
	}

	allowed, matched := ProvidersForModel(routes, "claude-sonnet-4-5")
	if !matched || !allowed["claude"] || allowed["codex"] {
		t.Fatalf("claude model: matched=%v allowed=%v", matched, allowed)
	}

	allowed, matched = ProvidersForModel(routes, "gpt-4o")
	if !matched || !allowed["codex"] || allowed["claude"] {
		t.Fatalf("gpt model: matched=%v allowed=%v", matched, allowed)
	}

	allowed, matched = ProvidersForModel(routes, "o3-mini")
	if !matched || !allowed["codex"] {
		t.Fatalf("o3 model: matched=%v allowed=%v", matched, allowed)
	}

	_, matched = ProvidersForModel(routes, "some-local-model")
	if matched {
		t.Fatal("unknown model should not match")
	}
}

func TestPickFiltersByModelRoutes(t *testing.T) {
	s := NewScheduler([]model.Account{
		{ID: "c1", Provider: "claude", Priority: 1, Healthy: true},
		{ID: "x1", Provider: "codex", Priority: 1, Healthy: true},
		{ID: "m1", Provider: "mock", Priority: 2, Healthy: true},
	})
	s.SetModelRoutes([]model.ModelRoute{
		{ID: "route-claude", Pattern: "^claude", Provider: "claude", Priority: 1, Enabled: true},
		{ID: "route-codex", Pattern: "^gpt-|^o[0-9]|^codex", Provider: "codex", Priority: 1, Enabled: true},
	})

	a, ok := s.Pick(0, "claude-opus-4")
	if !ok || a.Provider != "claude" {
		t.Fatalf("expected claude first, got %#v ok=%v", a, ok)
	}
	// attempt 1 should be mock (always eligible) not codex
	a2, ok2 := s.Pick(1, "claude-opus-4")
	if !ok2 || a2.Provider != "mock" {
		t.Fatalf("expected mock second for claude model, got %#v ok=%v", a2, ok2)
	}

	b, ok := s.Pick(0, "gpt-4o-mini")
	if !ok || b.Provider != "codex" {
		t.Fatalf("expected codex for gpt model, got %#v ok=%v", b, ok)
	}
}

func TestAccountCredentialCacheOrder(t *testing.T) {
	ClearAccountCredential("acct-x")
	SetAccountCredential("acct-x", "db-token", "db-refresh", `{"chatgpt_account_id":"acc-1"}`, "")
	if got := accountAccessToken("acct-x"); got != "db-token" {
		t.Fatalf("cache access=%q", got)
	}
	if got := chatgptAccountIDFromExtra(accountExtraJSON("acct-x")); got != "acc-1" {
		t.Fatalf("extra account id=%q", got)
	}
	ClearAccountCredential("acct-x")
}
