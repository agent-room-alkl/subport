package gateway

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

const claudeWebBase = "https://claude.ai"

// HasAccountCookie reports whether the per-account credential cache has a
// non-empty cookie for accountID. Never logs cookie contents.
func HasAccountCookie(accountID string) bool {
	accountID = trimSpace(accountID)
	if accountID == "" {
		return false
	}
	return model.CookieFromExtraJSON(accountExtraJSON(accountID)) != ""
}

// AccountCookie returns the stored cookie for accountID (empty if none).
// Never log the return value.
func AccountCookie(accountID string) string {
	return model.CookieFromExtraJSON(accountExtraJSON(trimSpace(accountID)))
}

// FetchClaudeCookieIdentity resolves safe account metadata using only the
// supplied account Cookie. Cookie handling remains per-request and never uses
// a shared browser cookie jar, which prevents identities converging across rows.
func FetchClaudeCookieIdentity(a model.Account, cookie string) (model.ClaudeCookieIdentity, error) {
	identity := model.ClaudeCookieIdentity{}
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return identity, fmt.Errorf("account has no cookie")
	}
	client := upstreamClient
	status, raw, accountErr := claudeAIDo(client, http.MethodGet, claudeWebBase+"/api/account", cookie, nil, "application/json")
	if accountErr == nil && status < 300 {
		var account map[string]any
		if json.Unmarshal(raw, &account) == nil {
			identity.Email = firstString(account, "email_address", "email")
			identity.Name = firstString(account, "display_name", "full_name", "name")
			identity.Plan = firstString(account, "subscription_type", "plan", "rate_limit_tier")
			applyClaudeMembershipIdentity(&identity, account["memberships"])
			if org, ok := account["organization"].(map[string]any); ok {
				applyClaudeOrganizationIdentity(&identity, org)
			}
		}
	} else if accountErr == nil {
		accountErr = fmt.Errorf("account: upstream status %d", status)
	}

	if identity.OrgUUID == "" {
		if orgUUID, _, err := claudeAIFetchOrgUUID(client, cookie); err == nil {
			identity.OrgUUID = orgUUID
			if identity.Email == "" {
				accountErr = nil
			}
		} else if accountErr == nil {
			accountErr = err
		}
	}
	if identity.Email == "" && identity.OrgUUID == "" {
		if accountErr == nil {
			accountErr = fmt.Errorf("Claude account identity was not returned")
		}
		return identity, accountErr
	}
	identity.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
	return identity, accountErr
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func applyClaudeMembershipIdentity(identity *model.ClaudeCookieIdentity, memberships any) {
	items, ok := memberships.([]any)
	if !ok {
		return
	}
	for _, item := range items {
		membership, _ := item.(map[string]any)
		org, _ := membership["organization"].(map[string]any)
		if len(org) == 0 {
			continue
		}
		applyClaudeOrganizationIdentity(identity, org)
		if identity.Plan == "" {
			identity.Plan = firstString(membership, "subscription_type", "plan", "rate_limit_tier")
		}
		return
	}
}

func applyClaudeOrganizationIdentity(identity *model.ClaudeCookieIdentity, org map[string]any) {
	if identity.OrgUUID == "" {
		identity.OrgUUID = firstString(org, "uuid", "id")
	}
	if identity.OrgName == "" {
		identity.OrgName = firstString(org, "name", "display_name")
	}
}

// ClaudeCookieSmokeResult is the admin-safe outcome of a cookie-based ping.
type ClaudeCookieSmokeResult struct {
	OK         bool   `json:"ok"`
	Path       string `json:"path"` // "cookie" | "oauth"
	LatencyMS  int64  `json:"latency_ms"`
	OrgUUID    string `json:"org_uuid,omitempty"`
	ConvUUID   string `json:"conv_uuid,omitempty"`
	Error      string `json:"error,omitempty"`
	MessageZH  string `json:"message_zh,omitempty"`
	MessageEN  string `json:"message_en,omitempty"`
	ResetsAt   string `json:"resets_at,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Model      string `json:"model,omitempty"`
}

// ClaudeCookieSmokeTest creates a claude.ai conversation and sends a short
// completion using ONLY the given account's cookie. Isolation: callers must
// pass the cookie loaded for that account id — never another account's.
func ClaudeCookieSmokeTest(a model.Account, cookie, modelName string) ClaudeCookieSmokeResult {
	start := time.Now()
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = "claude-sonnet-5"
	}
	out := ClaudeCookieSmokeResult{Path: "cookie", Model: modelName}
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		out.LatencyMS = time.Since(start).Milliseconds()
		out.Error = "account has no cookie / 账号未配置 Cookie"
		out.MessageZH = "账号未配置 Cookie"
		out.MessageEN = "account has no cookie"
		return out
	}

	client := upstreamClient
	orgUUID, status, err := claudeAIFetchOrgUUID(client, cookie)
	if err != nil {
		out.LatencyMS = time.Since(start).Milliseconds()
		info := MapUpstreamError(err)
		out.HTTPStatus = status
		if info.HTTPStatus == 0 {
			out.HTTPStatus = status
		}
		out.Error = info.Message
		out.MessageZH = info.MessageZH
		out.MessageEN = info.MessageEN
		out.ResetsAt = info.ResetsAt
		return out
	}
	out.OrgUUID = orgUUID

	convUUID, status, err := claudeAICreateConversation(client, cookie, orgUUID, modelName)
	if err != nil {
		out.LatencyMS = time.Since(start).Milliseconds()
		info := MapUpstreamError(err)
		out.HTTPStatus = status
		out.Error = info.Message
		out.MessageZH = info.MessageZH
		out.MessageEN = info.MessageEN
		out.ResetsAt = info.ResetsAt
		return out
	}
	out.ConvUUID = convUUID

	status, err = claudeAICompletionPing(client, cookie, orgUUID, convUUID, modelName)
	out.LatencyMS = time.Since(start).Milliseconds()
	out.HTTPStatus = status
	if err != nil {
		info := MapUpstreamError(err)
		if info.HTTPStatus == 0 {
			info.HTTPStatus = status
		}
		out.HTTPStatus = info.HTTPStatus
		out.Error = info.Message
		out.MessageZH = info.MessageZH
		out.MessageEN = info.MessageEN
		out.ResetsAt = info.ResetsAt
		return out
	}
	out.OK = true
	out.MessageZH = "Cookie 测通成功"
	out.MessageEN = "Cookie smoke test OK"
	return out
}

func claudeAIHeaders(cookie, accept string) http.Header {
	h := make(http.Header)
	h.Set("Cookie", cookie)
	h.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36")
	h.Set("anthropic-client-platform", "web_claude_ai")
	h.Set("Origin", "https://claude.ai")
	h.Set("Referer", "https://claude.ai/")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	if accept == "" {
		accept = "application/json"
	}
	h.Set("Accept", accept)
	return h
}

func claudeAIDo(client *http.Client, method, url string, cookie string, body any, accept string) (int, []byte, error) {
	if client == nil {
		client = upstreamClient
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header = claudeAIHeaders(cookie, accept)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, raw, nil
}

func claudeAIFetchOrgUUID(client *http.Client, cookie string) (string, int, error) {
	status, raw, err := claudeAIDo(client, http.MethodGet, claudeWebBase+"/api/organizations", cookie, nil, "application/json")
	if err != nil {
		return "", 0, err
	}
	if status >= 300 {
		return "", status, fmt.Errorf("upstream status %d: %s", status, truncate(string(raw), 400))
	}
	var orgs []map[string]any
	if json.Unmarshal(raw, &orgs) != nil {
		// sometimes wrapped
		var wrap struct {
			Organizations []map[string]any `json:"organizations"`
		}
		if json.Unmarshal(raw, &wrap) == nil {
			orgs = wrap.Organizations
		}
	}
	for _, o := range orgs {
		for _, k := range []string{"uuid", "id"} {
			if v, ok := o[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v), status, nil
			}
		}
	}
	return "", status, fmt.Errorf("upstream status %d: no organizations (session may be stale)", status)
}

func claudeAICreateConversation(client *http.Client, cookie, orgUUID, modelName string) (string, int, error) {
	convID := newClaudeConvUUID()
	body := map[string]any{
		"uuid":  convID,
		"name":  "",
		"model": modelName,
	}
	url := fmt.Sprintf("%s/api/organizations/%s/chat_conversations", claudeWebBase, orgUUID)
	status, raw, err := claudeAIDo(client, http.MethodPost, url, cookie, body, "application/json")
	if err != nil {
		return "", 0, err
	}
	if status >= 300 {
		return "", status, fmt.Errorf("upstream status %d: %s", status, truncate(string(raw), 400))
	}
	var resp map[string]any
	_ = json.Unmarshal(raw, &resp)
	for _, k := range []string{"uuid", "id"} {
		if v, ok := resp[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), status, nil
		}
	}
	// Some responses echo the request uuid implicitly; use ours.
	return convID, status, nil
}

func claudeAICompletionPing(client *http.Client, cookie, orgUUID, convUUID, modelName string) (int, error) {
	humanMessageUUID := newClaudeConvUUID()
	assistantMessageUUID := newClaudeConvUUID()
	body := map[string]any{
		"prompt":              "Reply with exactly: OK",
		"parent_message_uuid": humanMessageUUID,
		"timezone":            "Pacific/Auckland",
		"locale":              "en-US",
		"model":               modelName,
		"effort":              "medium",
		"thinking_mode":       "auto",
		"tools":               []any{},
		"turn_message_uuids": map[string]string{
			"human_message_uuid":     humanMessageUUID,
			"assistant_message_uuid": assistantMessageUUID,
		},
		"attachments":           []any{},
		"files":                 []any{},
		"sync_sources":          []any{},
		"completion_request_id": newClaudeConvUUID(),
		"rendering_mode":        "messages",
	}
	url := fmt.Sprintf("%s/api/organizations/%s/chat_conversations/%s/completion", claudeWebBase, orgUUID, convUUID)
	status, raw, err := claudeAIDo(client, http.MethodPost, url, cookie, body, "text/event-stream")
	if err != nil {
		return 0, err
	}
	if status >= 300 {
		return status, fmt.Errorf("upstream status %d: %s", status, truncate(string(raw), 400))
	}
	if sseErr := claudeAISSEError(raw); sseErr != nil {
		return status, sseErr
	}
	return status, nil
}

// claudeAISSEError inspects structured SSE error events. Successful
// message_start metadata may legitimately contain fields such as
// rate_limit_tier, so substring matching the entire stream creates false
// failures even after Claude accepted and completed the request.
func claudeAISSEError(raw []byte) error {
	eventName := ""
	for _, rawLine := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			eventName = ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "event:")))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(data), &payload)
		payloadType := strings.ToLower(firstString(payload, "type"))
		if eventName != "error" && payloadType != "error" {
			continue
		}
		message := firstString(payload, "message", "detail")
		if upstreamErr, ok := payload["error"].(map[string]any); ok {
			if nested := firstString(upstreamErr, "message", "detail", "type"); nested != "" {
				message = nested
			}
		} else if upstreamErr, ok := payload["error"].(string); ok && strings.TrimSpace(upstreamErr) != "" {
			message = strings.TrimSpace(upstreamErr)
		}
		if message == "" {
			message = truncate(data, 400)
		}
		return fmt.Errorf("upstream SSE error: %s", message)
	}
	return nil
}

func newClaudeConvUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
