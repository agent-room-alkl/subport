package gateway

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestResolveModelAliasAndChain(t *testing.T) {
	SetModelAliasConfig(ModelAliasConfig{
		Aliases: map[string]string{
			"sonnet": "claude-sonnet-4-5-20250929",
		},
		Fallbacks: map[string][]string{
			"claude-sonnet-4-5-20250929": {"claude-haiku-4-5-20251001", "claude-3-5-haiku-20241022"},
		},
		MaxFallbackTries: 2,
	})
	if got := ResolveModelAlias("sonnet"); got != "claude-sonnet-4-5-20250929" {
		t.Fatalf("alias=%q", got)
	}
	chain := ModelAttemptChain("sonnet")
	if len(chain) != 3 {
		t.Fatalf("chain=%v", chain)
	}
	if chain[0] != "claude-sonnet-4-5-20250929" || chain[1] != "claude-haiku-4-5-20251001" {
		t.Fatalf("chain=%v", chain)
	}
	SetModelAliasConfig(ModelAliasConfig{MaxFallbackTries: 1, Fallbacks: map[string][]string{
		"claude-sonnet-4-5-20250929": {"a", "b"},
	}, Aliases: map[string]string{"sonnet": "claude-sonnet-4-5-20250929"}})
	chain = ModelAttemptChain("sonnet")
	if len(chain) != 2 {
		t.Fatalf("capped chain=%v", chain)
	}
}

func TestParseResetsAtAndMapUpstream(t *testing.T) {
	raw := `{"error":{"type":"rate_limit_error","message":"rate_limit"},"resetsAt":"2026-09-14T12:00:00Z"}`
	info := MapUpstreamError(fmt.Errorf("upstream status 429: %s", raw))
	if info.Kind != QuotaKindRateLimit {
		t.Fatalf("kind=%s", info.Kind)
	}
	if info.ResetsAt != "2026-09-14T12:00:00Z" {
		t.Fatalf("resetsAt=%q", info.ResetsAt)
	}
	if !strings.Contains(info.Message, "resetsAt=") {
		t.Fatalf("message=%q", info.Message)
	}
	info2 := MapUpstreamError(fmt.Errorf("upstream status 402: out_of_credits"))
	if info2.Kind != QuotaKindOutOfCredits {
		t.Fatalf("kind=%s", info2.Kind)
	}
	d := CooldownDurationForError(fmt.Errorf("upstream status 429"), time.Now().UTC())
	if d != 60*time.Second {
		t.Fatalf("cooldown=%v", d)
	}
}

func TestMapUpstreamErrorRedactsBearerTokens(t *testing.T) {
	info := MapUpstreamError(fmt.Errorf("upstream failed: Authorization: Bearer secret-value-123"))
	if strings.Contains(info.Message, "secret-value-123") {
		t.Fatalf("mapped error leaked bearer token: %q", info.Message)
	}
	if !strings.Contains(info.Message, "[REDACTED]") {
		t.Fatalf("mapped error did not retain a useful redaction marker: %q", info.Message)
	}
}
