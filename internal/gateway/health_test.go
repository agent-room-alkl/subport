package gateway

import (
	"fmt"
	"testing"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

func TestClassifyUpstreamError(t *testing.T) {
	cases := []struct {
		err  error
		want FailureClass
	}{
		{nil, FailureNone},
		{fmt.Errorf("upstream status 429: rate_limit"), FailureRateLimit},
		{fmt.Errorf("upstream status 403: forbidden"), FailureForbidden},
		{fmt.Errorf("i/o timeout"), FailureTimeout},
		{fmt.Errorf("context deadline exceeded"), FailureTimeout},
		{fmt.Errorf("upstream status 500"), FailureOther},
	}
	for _, c := range cases {
		if got := ClassifyUpstreamError(c.err); got != c.want {
			t.Fatalf("ClassifyUpstreamError(%v)=%q want %q", c.err, got, c.want)
		}
	}
}

func TestNeedsRefresh(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	if !needsRefresh("", now) {
		t.Fatal("empty expiry should refresh")
	}
	soon := now.Add(10 * time.Minute).Format(time.RFC3339)
	if !needsRefresh(soon, now) {
		t.Fatal("near expiry should refresh")
	}
	later := now.Add(2 * time.Hour).Format(time.RFC3339)
	if needsRefresh(later, now) {
		t.Fatal("far expiry should skip")
	}
}

func TestAutoPauseOnRepeated429(t *testing.T) {
	s := NewScheduler([]model.Account{{
		ID: "a1", Provider: "mock", Priority: 1, Healthy: true,
	}})
	for i := 0; i < RateLimitPauseThreshold; i++ {
		s.NoteUpstreamResult("a1", fmt.Errorf("upstream status 429"))
	}
	accts := s.Accounts()
	if accts[0].Healthy {
		t.Fatal("expected account paused after 429 threshold")
	}
	if accts[0].CooldownUntil == "" {
		t.Fatal("expected cooldown_until set")
	}
	if accts[0].Consecutive429 < RateLimitPauseThreshold {
		t.Fatalf("consecutive_429=%d", accts[0].Consecutive429)
	}
}

func TestPickSkipsCooldown(t *testing.T) {
	s := NewScheduler([]model.Account{
		{ID: "a1", Provider: "mock", Priority: 1, Healthy: true, CooldownUntil: time.Now().UTC().Add(time.Hour).Format(time.RFC3339)},
		{ID: "a2", Provider: "mock", Priority: 1, Healthy: true},
	})
	got, ok := s.Pick(0, "gpt-4o-mini")
	if !ok || got.ID != "a2" {
		t.Fatalf("Pick got %#v ok=%v want a2", got, ok)
	}
}