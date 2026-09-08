// Package model holds the types shared across store, gateway and httpapi.
// It deliberately has no dependencies of its own, so no import cycle is
// possible between the layers that use it.
package model

import "time"

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	Salt         string    `json:"salt"`
	Role         string    `json:"role"` // "admin" | "user"
	QuotaTotal   int64     `json:"quota_total"`
	QuotaUsed    int64     `json:"quota_used"`
	CreatedAt    time.Time `json:"created_at"`
}

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

type APIKey struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	SecretSHA string    `json:"secret_sha"` // only the hash is stored
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  string    `json:"last_used"`
}

// UsageLog is one billable attempt. StreamBroken and Compensated exist from
// day one so the stream-break billing policy can be decided later without a
// migration - see docs/MERGE_REPORT.md on pre-consume/settle/refund.
type UsageLog struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	KeyID        string    `json:"key_id"`
	Model        string    `json:"model"`
	AccountID    string    `json:"account_id"`
	Tokens       int64     `json:"tokens"`
	Cost         int64     `json:"cost"`
	Status       string    `json:"status"` // success | failed | stream_broken
	StreamBroken bool      `json:"stream_broken"`
	Compensated  bool      `json:"compensated"`
	Attempts     int       `json:"attempts"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Account is one upstream credential. Priority is the tier: the scheduler
// exhausts a tier horizontally before descending to the next one.
type Account struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Provider            string  `json:"provider"`
	BaseURL             string  `json:"base_url"`
	Priority            int     `json:"priority"`
	Healthy             bool    `json:"healthy"`
	Load                float64 `json:"load"`
	CooldownUntil       string  `json:"cooldown_until"`
	LastError           string  `json:"last_error"`
	ConsecutiveTimeouts int     `json:"consecutive_timeouts"`
	Consecutive403      int     `json:"consecutive_403"`
}

// CompensationConfig controls the auto-compensation engine. A stream cut is
// billed for what was produced; when the broken rate over a window of recent
// calls crosses Threshold, the system credits the cost back and marks each
// broken log Compensated=true so it can never be credited twice.
type CompensationConfig struct {
	Threshold  float64 // e.g. 0.3 = 30% broken rate triggers compensation
	WindowSize int     // consider the last N calls for the rate
	MinBroken  int     // at least this many broken calls to trigger (avoid 1-off noise)
}

// CompensationResult is what CompensateBrokenStreams returns, so the caller
// (and the admin view) can report what happened.
type CompensationResult struct {
	Triggered        bool    `json:"triggered"`
	BrokenCount      int     `json:"broken_count"`
	TotalCount       int     `json:"total_count"`
	Rate             float64 `json:"rate"`
	Credited         int64   `json:"credited"`
	CompensatedCount int     `json:"compensated_count"`
}

// PublicUser strips the password hash and salt. Never serialise User directly.
func PublicUser(u User) map[string]any {
	return map[string]any{
		"id": u.ID, "username": u.Username, "role": u.Role,
		"quota_total": u.QuotaTotal, "quota_used": u.QuotaUsed,
		"created_at": u.CreatedAt,
	}
}

// PublicKey omits SecretSHA - the secret is shown once at creation and never again.
func PublicKey(k APIKey) map[string]any {
	return map[string]any{
		"id": k.ID, "name": k.Name, "prefix": k.Prefix,
		"enabled": k.Enabled, "created_at": k.CreatedAt, "last_used": k.LastUsed,
	}
}

// PublicAccount is the admin view, shaped to the front-end contract in
// web/admin/README.md.
func PublicAccount(a Account) map[string]any {
	state := "healthy"
	if !a.Healthy {
		state = "paused"
	}
	return map[string]any{
		"id": a.ID, "label": a.Name, "provider": a.Provider,
		"tier": a.Priority, "state": state, "load": a.Load,
		"cooldown_until": a.CooldownUntil, "last_error": a.LastError,
		"consecutive_timeouts": a.ConsecutiveTimeouts,
		"consecutive_403":      a.Consecutive403,
	}
}
