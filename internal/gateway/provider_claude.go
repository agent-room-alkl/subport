package gateway

// Claude.ai subscription (OAuth) provider.
//
// This is NOT the official Anthropic API-key path (x-api-key → api.anthropic.com
// as a paid API customer). It uses Claude Code / Claude.ai subscription OAuth
// access tokens (Authorization: Bearer …) with the anthropic-beta oauth header,
// following the proven request shape in _ref/sub2api.
//
// Credentials stay in the environment (or an in-memory token obtained by
// exchanging SUBPORT_CLAUDE_SESSION at startup). They are never stored in the
// accounts table, logged, or returned by any API.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/agent-room-alkl/subport/internal/model"
)

const (
	claudeDefaultBaseURL = "https://api.anthropic.com"
	claudeAPIVersion     = "2023-06-01"
	// Matches sub2api's MessageBetaHeaderNoTools / DefaultBetaHeader subset:
	// Claude Code-scoped OAuth requires the claude-code + oauth betas.
	claudeOAuthBeta = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14"
	claudeCLIUA     = "claude-cli/2.1.258 (external, cli)"
	claudeDefaultModel = "claude-sonnet-4-5"
)

func init() {
	providers["claude"] = claudeProvider{}
}

type claudeProvider struct{}

func (claudeProvider) Name() string { return "claude" }

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func claudeCredential() string {
	// Global path (no account id): runtime -> env/files.
	if v := claudeRuntimeAccessToken(); v != "" {
		return v
	}
	return loadClaudeAccessToken()
}

func claudeCredentialFor(accountID string) string {
	// DB/cache -> runtime -> env/files.
	if v := accountAccessToken(accountID); v != "" {
		return v
	}
	return claudeCredential()
}


func claudeBaseURL(a model.Account) string {
	if strings.TrimSpace(a.BaseURL) != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return claudeDefaultBaseURL
}

func claudeAcceptsModel(modelName string) bool {
	m := strings.ToLower(strings.TrimSpace(modelName))
	if m == "" {
		return true
	}
	// Leave GPT / Codex / Antigravity Gemini models for their providers.
	if strings.HasPrefix(m, "gpt-") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") || strings.HasPrefix(m, "o4") || strings.HasPrefix(m, "codex") || strings.HasPrefix(m, "chatgpt") {
		return false
	}
	if strings.HasPrefix(m, "gemini") || strings.HasPrefix(m, "tab_flash") || strings.HasPrefix(m, "gpt-oss") {
		return false
	}
	return true
}

func openAIToAnthropic(req ChatRequest) (anthropicRequest, error) {
	modelName := strings.TrimSpace(req.Model)
	if !claudeAcceptsModel(modelName) {
		return anthropicRequest{}, fmt.Errorf("claude: model %q belongs to another provider", modelName)
	}
	if modelName == "" {
		modelName = claudeDefaultModel
	}

	var systemParts []string
	msgs := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		switch role {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				systemParts = append(systemParts, m.Content)
			}
		case "assistant":
			msgs = append(msgs, anthropicMessage{Role: "assistant", Content: m.Content})
		default:
			// user, tool, or anything else → user (Anthropic only accepts user/assistant)
			msgs = append(msgs, anthropicMessage{Role: "user", Content: m.Content})
		}
	}
	if len(msgs) == 0 {
		return anthropicRequest{}, fmt.Errorf("claude: at least one user/assistant message is required")
	}
	// Anthropic requires alternating roles starting with user.
	if msgs[0].Role != "user" {
		msgs = append([]anthropicMessage{{Role: "user", Content: "(continue)"}}, msgs...)
	}

	out := anthropicRequest{
		Model:     modelName,
		MaxTokens: 4096,
		Messages:  msgs,
	}
	if len(systemParts) > 0 {
		out.System = strings.Join(systemParts, "\n\n")
	}
	return out, nil
}

func (claudeProvider) Call(a model.Account, req ChatRequest) (Reply, error) {
	payload, err := openAIToAnthropic(req)
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}

	url := claudeBaseURL(a) + "/v1/messages"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	key := claudeCredentialFor(a.ID)
	setClaudeHeaders(httpReq, key)

	resp, err := HTTPClientFor(a).Do(httpReq)
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(resp.Body)
	var out anthropicResponse
	_ = json.Unmarshal(raw, &out)

	if resp.StatusCode == http.StatusUnauthorized {
		// One refresh attempt when a refresh token is available.
		if refreshed, rerr := claudeTryRefresh(); rerr == nil && refreshed {
			key = claudeCredentialFor(a.ID)
			httpReq2, err2 := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			if err2 != nil {
				return Reply{}, RelayError{Err: err2, FirstByteSent: false}
			}
			setClaudeHeaders(httpReq2, key)
			resp2, err2 := HTTPClientFor(a).Do(httpReq2)
			if err2 != nil {
				return Reply{}, RelayError{Err: err2, FirstByteSent: false}
			}
			defer resp2.Body.Close()
			raw, readErr = io.ReadAll(resp2.Body)
			out = anthropicResponse{}
			_ = json.Unmarshal(raw, &out)
			resp = resp2
		}
	}

	if resp.StatusCode >= 300 {
		msg := fmt.Sprintf("upstream status %d", resp.StatusCode)
		if out.Error != nil && out.Error.Message != "" {
			msg = fmt.Sprintf("%s: %s", msg, redactSecrets(out.Error.Message, key))
		} else if len(raw) > 0 {
			msg = fmt.Sprintf("%s: %s", msg, redactSecrets(truncate(string(raw), 400), key))
		}
		return Reply{}, RelayError{Err: fmt.Errorf("%s", msg), FirstByteSent: false}
	}
	if readErr != nil {
		return Reply{}, RelayError{
			Err:           fmt.Errorf("stream truncated: %v", redactSecrets(readErr.Error(), key)),
			FirstByteSent: true,
		}
	}

	var text strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" || block.Type == "" {
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		return Reply{}, RelayError{
			Err:           fmt.Errorf("upstream returned no text content"),
			FirstByteSent: false,
		}
	}
	tokens := out.Usage.InputTokens + out.Usage.OutputTokens
	return Reply{Content: text.String(), Tokens: tokens}, nil
}

func setClaudeHeaders(req *http.Request, accessToken string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-version", claudeAPIVersion)
	req.Header.Set("anthropic-beta", claudeOAuthBeta)
	req.Header.Set("User-Agent", claudeCLIUA)
	req.Header.Set("X-App", "cli")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Stream: v1 converts via non-stream Call and emits a single OpenAI-shaped SSE
// completion. True Anthropic SSE passthrough is a TODO.
func (claudeProvider) Stream(a model.Account, req ChatRequest, w http.ResponseWriter) (int64, error) {
	reply, err := (claudeProvider{}).Call(a, req)
	if err != nil {
		return 0, err
	}
	ensureSSEHeaders(w)
	flusher, _ := w.(http.Flusher)
	payload, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]string{"content": reply.Content}}},
		"usage":   map[string]any{"total_tokens": reply.Tokens},
	})
	if _, werr := io.WriteString(w, "data: "+string(payload)+"\n\n"); werr != nil {
		return 0, RelayError{Err: werr, FirstByteSent: false}
	}
	if flusher != nil {
		flusher.Flush()
	}
	if _, werr := io.WriteString(w, "data: [DONE]\n\n"); werr != nil {
		return reply.Tokens, RelayError{Err: werr, FirstByteSent: true}
	}
	if flusher != nil {
		flusher.Flush()
	}
	return reply.Tokens, nil
}
