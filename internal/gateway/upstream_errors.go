package gateway

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// UpstreamQuotaKind classifies quota / rate-limit style upstream failures.
type UpstreamQuotaKind string

const (
	QuotaKindNone          UpstreamQuotaKind = ""
	QuotaKindRateLimit     UpstreamQuotaKind = "rate_limit"
	QuotaKindExceededLimit UpstreamQuotaKind = "exceeded_limit"
	QuotaKindOutOfCredits  UpstreamQuotaKind = "out_of_credits"
)

// UpstreamErrorInfo is a client-safe view of an upstream failure.
// Secrets must never appear in Message / MessageEN / MessageZH.
type UpstreamErrorInfo struct {
	Kind       UpstreamQuotaKind `json:"kind,omitempty"`
	HTTPStatus int               `json:"http_status,omitempty"`
	ResetsAt   string            `json:"resets_at,omitempty"` // RFC3339 when known
	Message    string            `json:"message"`             // bilingual combined
	MessageZH  string            `json:"message_zh"`
	MessageEN  string            `json:"message_en"`
	Retryable  bool              `json:"retryable"`
}

var (
	reStatusCode = regexp.MustCompile(`(?i)(?:status|http)\s*(\d{3})`)
	reResetsAt   = regexp.MustCompile(`(?i)resets?_?at["'\s:=]+([0-9T:\-\.+Z]+)`)
	reRetryAfter = regexp.MustCompile(`(?i)retry[-_ ]?after["'\s:=]+(\d+)`)
)

// ParseResetsAt extracts a reset timestamp from an error body or message.
// Accepts RFC3339, unix seconds, or Retry-After seconds (relative).
func ParseResetsAt(raw string, now time.Time) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	// JSON object with resetsAt / resets_at
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) == nil {
		for _, k := range []string{"resetsAt", "resets_at", "reset_at", "resetAt"} {
			if v, ok := obj[k]; ok {
				if s := parseResetsValue(v, now); s != "" {
					return s, true
				}
			}
			if errObj, ok := obj["error"].(map[string]any); ok {
				if v, ok := errObj[k]; ok {
					if s := parseResetsValue(v, now); s != "" {
						return s, true
					}
				}
			}
		}
	}
	if m := reResetsAt.FindStringSubmatch(raw); len(m) == 2 {
		if s := parseResetsValue(m[1], now); s != "" {
			return s, true
		}
	}
	if m := reRetryAfter.FindStringSubmatch(raw); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return now.UTC().Add(time.Duration(n) * time.Second).Format(time.RFC3339), true
		}
	}
	return "", false
}

func parseResetsValue(v any, now time.Time) string {
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return ""
		}
		if ts, err := time.Parse(time.RFC3339, s); err == nil {
			return ts.UTC().Format(time.RFC3339)
		}
		if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return ts.UTC().Format(time.RFC3339)
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			if n > 1_000_000_000_000 { // ms
				n = n / 1000
			}
			if n > 1_000_000_000 {
				return time.Unix(n, 0).UTC().Format(time.RFC3339)
			}
			// small int: treat as retry-after seconds
			if n > 0 && n < 86400*7 {
				return now.UTC().Add(time.Duration(n) * time.Second).Format(time.RFC3339)
			}
		}
	case float64:
		n := int64(t)
		if n > 1_000_000_000_000 {
			n = n / 1000
		}
		if n > 1_000_000_000 {
			return time.Unix(n, 0).UTC().Format(time.RFC3339)
		}
		if n > 0 && n < 86400*7 {
			return now.UTC().Add(time.Duration(n) * time.Second).Format(time.RFC3339)
		}
	}
	return ""
}

// ClassifyQuotaError maps upstream error text to a quota/rate-limit kind.
func ClassifyQuotaError(msg string) UpstreamQuotaKind {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "out_of_credits") || strings.Contains(low, "out of credits") ||
		strings.Contains(low, "insufficient credits") || strings.Contains(low, "credit balance"):
		return QuotaKindOutOfCredits
	case strings.Contains(low, "exceeded_limit") || strings.Contains(low, "limit exceeded") ||
		strings.Contains(low, "usage limit") || strings.Contains(low, "quota exceeded") ||
		strings.Contains(low, "extra usage") || strings.Contains(low, "overloaded_error"):
		return QuotaKindExceededLimit
	case strings.Contains(low, "rate_limit") || strings.Contains(low, "rate limit") ||
		strings.Contains(low, "too many requests") || strings.Contains(low, "status 429"):
		return QuotaKindRateLimit
	default:
		return QuotaKindNone
	}
}

// MapUpstreamError builds an honest bilingual client message for common Claude
// quota / rate-limit failures. Never embeds secrets.
func MapUpstreamError(err error) UpstreamErrorInfo {
	info := UpstreamErrorInfo{
		MessageEN: "Upstream request failed",
		MessageZH: "上游请求失败",
		Retryable: false,
	}
	if err == nil {
		info.Message = combineBilingual(info.MessageZH, info.MessageEN, "")
		return info
	}
	// The mapper is also used directly by admin smoke paths, so do not assume
	// every caller already scrubbed an echoed Authorization header.
	raw := redactSecrets(err.Error(), "")
	if len(raw) > 800 {
		raw = raw[:800]
	}
	if m := reStatusCode.FindStringSubmatch(raw); len(m) == 2 {
		if n, e := strconv.Atoi(m[1]); e == nil {
			info.HTTPStatus = n
		}
	}
	info.Kind = ClassifyQuotaError(raw)
	if info.Kind == QuotaKindNone && info.HTTPStatus == 429 {
		info.Kind = QuotaKindRateLimit
	}
	now := time.Now().UTC()
	if resets, ok := ParseResetsAt(raw, now); ok {
		info.ResetsAt = resets
	}

	switch info.Kind {
	case QuotaKindRateLimit:
		info.MessageZH = "触发频率限制（429），请稍后重试或轮换账号"
		info.MessageEN = "Rate limited (429). Retry later or rotate accounts"
		info.Retryable = true
	case QuotaKindExceededLimit:
		info.MessageZH = "已超出用量上限（exceeded_limit），请等待配额重置或更换账号"
		info.MessageEN = "Usage limit exceeded (exceeded_limit). Wait for reset or switch accounts"
		info.Retryable = true
	case QuotaKindOutOfCredits:
		info.MessageZH = "额度不足（out_of_credits），请充值或更换账号"
		info.MessageEN = "Out of credits. Top up or switch accounts"
		info.Retryable = false
	default:
		if info.HTTPStatus >= 500 {
			info.MessageZH = "上游服务暂时不可用（5xx）"
			info.MessageEN = "Upstream temporarily unavailable (5xx)"
			info.Retryable = true
		} else if info.HTTPStatus == 401 || info.HTTPStatus == 403 {
			info.MessageZH = "凭证无效或无权访问，请重新授权 / 粘贴 Cookie"
			info.MessageEN = "Invalid or unauthorized credentials; re-auth or paste Cookie"
			info.Retryable = false
		} else {
			// Keep a short redacted snippet of the original for diagnosis.
			snip := raw
			if len(snip) > 160 {
				snip = snip[:160] + "…"
			}
			info.MessageEN = snip
			info.MessageZH = "上游错误: " + snip
		}
	}
	info.Message = combineBilingual(info.MessageZH, info.MessageEN, info.ResetsAt)
	return info
}

func combineBilingual(zh, en, resetsAt string) string {
	msg := zh + " / " + en
	if strings.TrimSpace(resetsAt) != "" {
		msg += fmt.Sprintf(" (resetsAt=%s)", resetsAt)
	}
	return msg
}

// CooldownDurationForError returns how long to cool an account after err.
// Default 60s for rate limits; uses resetsAt when parseable and in the future.
func CooldownDurationForError(err error, now time.Time) time.Duration {
	const defaultCD = 60 * time.Second
	if err == nil {
		return 0
	}
	raw := err.Error()
	if resets, ok := ParseResetsAt(raw, now); ok {
		if t, e := time.Parse(time.RFC3339, resets); e == nil {
			d := t.Sub(now)
			if d > time.Second && d < 24*time.Hour {
				return d
			}
		}
	}
	kind := ClassifyQuotaError(raw)
	switch kind {
	case QuotaKindRateLimit, QuotaKindExceededLimit:
		return defaultCD
	default:
		if strings.Contains(strings.ToLower(raw), "status 429") {
			return defaultCD
		}
		return 0
	}
}

// IsRetryableUpstreamStatus reports whether Relay should try another model
// fallback (429 or 5xx).
func IsRetryableUpstreamStatus(err error) bool {
	if err == nil {
		return false
	}
	info := MapUpstreamError(err)
	if info.HTTPStatus == 429 || info.HTTPStatus >= 500 {
		return true
	}
	switch info.Kind {
	case QuotaKindRateLimit, QuotaKindExceededLimit:
		return true
	}
	return false
}
