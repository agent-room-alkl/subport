package gateway

import (
	"errors"
	"log"
	"net"
	"strings"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

// Failure class thresholds. Hitting a threshold marks the account unhealthy
// and sets CooldownUntil (temp-unsched via existing fields).
const (
	TimeoutPauseThreshold = 3
	ForbiddenPauseThreshold = 2
	RateLimitPauseThreshold = 3

	TimeoutCooldown   = 5 * time.Minute
	ForbiddenCooldown = 15 * time.Minute
	RateLimitCooldown = 2 * time.Minute
)

// FailureClass categorises upstream errors for auto-pause counters.
type FailureClass string

const (
	FailureNone      FailureClass = ""
	FailureTimeout   FailureClass = "timeout"
	FailureForbidden FailureClass = "403"
	FailureRateLimit FailureClass = "429"
	FailureOther     FailureClass = "other"
)

// AccountHealthStore persists auto-pause / cooldown fields.
type AccountHealthStore interface {
	ApplyAccountHealth(id string, healthy bool, cooldownUntil, lastError string, timeouts, c403, c429 int) error
	GetAccount(id string) (model.Account, error)
}

// ClassifyUpstreamError maps a Call/Relay error to a failure class.
func ClassifyUpstreamError(err error) FailureClass {
	if err == nil {
		return FailureNone
	}
	msg := strings.ToLower(err.Error())
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return FailureTimeout
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "i/o timeout") {
		return FailureTimeout
	}
	if strings.Contains(msg, "status 429") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "rate_limit") || strings.Contains(msg, "too many requests") {
		return FailureRateLimit
	}
	if strings.Contains(msg, "status 403") || strings.Contains(msg, "forbidden") {
		return FailureForbidden
	}
	return FailureOther
}

// NoteUpstreamResult updates consecutive counters and may pause the account.
// Success resets the relevant counters. Never logs secrets (errors are already redacted).
func (s *Scheduler) NoteUpstreamResult(accountID string, callErr error) {
	if s == nil || accountID == "" {
		return
	}
	class := ClassifyUpstreamError(callErr)
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.accounts {
		if s.accounts[i].ID == accountID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	a := &s.accounts[idx]
	if callErr == nil {
		a.ConsecutiveTimeouts = 0
		a.Consecutive403 = 0
		a.Consecutive429 = 0
		a.LastError = ""
		s.persistHealthLocked(*a)
		return
	}

	// Cap stored last_error length; never include credentials (already redacted upstream).
	errMsg := callErr.Error()
	if len(errMsg) > 240 {
		errMsg = errMsg[:240] + "..."
	}
	a.LastError = errMsg

	paused := false
	var cooldown time.Duration
	switch class {
	case FailureTimeout:
		a.ConsecutiveTimeouts++
		if a.ConsecutiveTimeouts >= TimeoutPauseThreshold {
			paused = true
			cooldown = TimeoutCooldown
		}
	case FailureForbidden:
		a.Consecutive403++
		if a.Consecutive403 >= ForbiddenPauseThreshold {
			paused = true
			cooldown = ForbiddenCooldown
		}
	case FailureRateLimit:
		a.Consecutive429++
		if a.Consecutive429 >= RateLimitPauseThreshold {
			paused = true
			cooldown = RateLimitCooldown
		}
	}

	if paused {
		a.Healthy = false
		a.CooldownUntil = time.Now().UTC().Add(cooldown).Format(time.RFC3339)
		log.Printf("auto-pause: account=%s class=%s timeouts=%d 403=%d 429=%d cooldown_until=%s",
			a.ID, class, a.ConsecutiveTimeouts, a.Consecutive403, a.Consecutive429, a.CooldownUntil)
	}
	s.persistHealthLocked(*a)
}

func (s *Scheduler) persistHealthLocked(a model.Account) {
	if s.healthStore == nil {
		return
	}
	// Persist outside the hot path would be nicer; keep it sync for correctness.
	go func(acc model.Account) {
		_ = s.healthStore.ApplyAccountHealth(
			acc.ID, acc.Healthy, acc.CooldownUntil, acc.LastError,
			acc.ConsecutiveTimeouts, acc.Consecutive403, acc.Consecutive429,
		)
	}(a)
}

// ReapCooldowns re-enables accounts whose CooldownUntil has passed.
func (s *Scheduler) ReapCooldowns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	n := 0
	for i := range s.accounts {
		a := &s.accounts[i]
		if a.Healthy || strings.TrimSpace(a.CooldownUntil) == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, a.CooldownUntil)
		if err != nil {
			t, err = time.Parse(time.RFC3339Nano, a.CooldownUntil)
		}
		if err != nil || t.After(now) {
			continue
		}
		a.Healthy = true
		a.CooldownUntil = ""
		a.ConsecutiveTimeouts = 0
		a.Consecutive403 = 0
		a.Consecutive429 = 0
		a.LastError = ""
		n++
		log.Printf("auto-pause: account=%s cooldown expired, re-enabled", a.ID)
		s.persistHealthLocked(*a)
	}
	return n
}

// SetHealthStore wires persistence for auto-pause state.
func (s *Scheduler) SetHealthStore(hs AccountHealthStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthStore = hs
}

// StartHealthReaper periodically re-enables cooled-down accounts.
func StartHealthReaper(stop <-chan struct{}, sched *Scheduler, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				_ = sched.ReapCooldowns()
			}
		}
	}()
}

// accountSchedulable reports whether Pick may select this account.
// Temp-unsched: Healthy=false OR CooldownUntil in the future.
func accountSchedulable(a model.Account, now time.Time) bool {
	if !a.Healthy {
		return false
	}
	cu := strings.TrimSpace(a.CooldownUntil)
	if cu == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, cu)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, cu)
	}
	if err != nil {
		return true
	}
	return !t.After(now)
}