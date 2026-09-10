package gateway

// Antigravity subscription (OAuth) provider.
//
// OpenAI chat/completions → cloudcode-pa.googleapis.com/v1internal:streamGenerateContent
// (Gemini-shaped body wrapped in Antigravity v1internal). Uses Google OAuth from
// the Antigravity desktop client, not Cloud API keys.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/agent-room-alkl/subport/internal/model"
)

const (
	antigravityDefaultBaseURL = "https://cloudcode-pa.googleapis.com"
	antigravityDefaultModel   = "gemini-2.5-flash"
	antigravityStreamAction   = "streamGenerateContent"
)

func init() {
	providers["antigravity"] = antigravityProvider{}
}

type antigravityProvider struct{}

func (antigravityProvider) Name() string { return "antigravity" }

func antigravityBaseURL(a model.Account) string {
	if strings.TrimSpace(a.BaseURL) != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return antigravityDefaultBaseURL
}

// antigravityAcceptsModel keeps GPT/Codex/Claude-subscription models off this
// adapter so the failover walk can try the right provider.
func antigravityAcceptsModel(modelName string) bool {
	m := strings.ToLower(strings.TrimSpace(modelName))
	if m == "" {
		return true
	}
	if strings.HasPrefix(m, "gemini") || strings.HasPrefix(m, "tab_flash") || strings.HasPrefix(m, "gpt-oss") {
		return true
	}
	// Explicit Antigravity Claude aliases from sub2api mapping (thinking variants).
	if strings.Contains(m, "thinking") && strings.HasPrefix(m, "claude") {
		return true
	}
	if strings.HasPrefix(m, "claude-opus-4-6") || strings.HasPrefix(m, "claude-sonnet-4-5") {
		// These names are also served by provider=claude; reject here unless the
		// model_route points only at antigravity. Empty reject keeps failover clean
		// when both accounts are healthy without routes.
		return false
	}
	if strings.HasPrefix(m, "claude") {
		return false
	}
	if strings.HasPrefix(m, "gpt-") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") ||
		strings.HasPrefix(m, "o4") || strings.HasPrefix(m, "codex") || strings.HasPrefix(m, "chatgpt") {
		return false
	}
	return false
}

func openAIToAntigravityBody(req ChatRequest, projectID string) ([]byte, string, error) {
	modelName := strings.TrimSpace(req.Model)
	if !antigravityAcceptsModel(modelName) {
		return nil, "", fmt.Errorf("antigravity: model %q belongs to another provider", modelName)
	}
	if modelName == "" {
		modelName = antigravityDefaultModel
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, "", fmt.Errorf("antigravity: project_id is required")
	}

	var systemParts []string
	contents := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		content := m.Content
		switch role {
		case "system":
			if strings.TrimSpace(content) != "" {
				systemParts = append(systemParts, content)
			}
		case "assistant":
			contents = append(contents, map[string]any{
				"role":  "model",
				"parts": []map[string]string{{"text": content}},
			})
		default:
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": []map[string]string{{"text": content}},
			})
		}
	}
	if len(contents) == 0 {
		return nil, "", fmt.Errorf("antigravity: at least one user/assistant message is required")
	}

	inner := map[string]any{
		"contents": contents,
	}
	if len(systemParts) > 0 {
		inner["systemInstruction"] = map[string]any{
			"role":  "user",
			"parts": []map[string]string{{"text": strings.Join(systemParts, "\n\n")}},
		}
	}

	wrapped := map[string]any{
		"project":     projectID,
		"requestId":   "agent-subport",
		"userAgent":   "antigravity",
		"requestType": "agent",
		"model":       modelName,
		"request":     inner,
	}
	body, err := json.Marshal(wrapped)
	return body, modelName, err
}

func setAntigravityHeaders(req *http.Request, accessToken string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityUserAgent())
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
}

func (antigravityProvider) Call(a model.Account, req ChatRequest) (Reply, error) {
	projectID := antigravityProjectIDFor(a.ID)
	body, _, err := openAIToAntigravityBody(req, projectID)
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}

	url := antigravityBaseURL(a) + "/v1internal:" + antigravityStreamAction + "?alt=sse"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	key := antigravityCredentialFor(a.ID)
	setAntigravityHeaders(httpReq, key)

	resp, err := HTTPClientFor(a).Do(httpReq)
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		if refreshed, rerr := antigravityTryRefresh(); rerr == nil && refreshed {
			key = antigravityCredentialFor(a.ID)
			httpReq2, err2 := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			if err2 != nil {
				return Reply{}, RelayError{Err: err2, FirstByteSent: false}
			}
			setAntigravityHeaders(httpReq2, key)
			resp2, err2 := HTTPClientFor(a).Do(httpReq2)
			if err2 != nil {
				return Reply{}, RelayError{Err: err2, FirstByteSent: false}
			}
			resp = resp2
		}
	}

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := fmt.Sprintf("upstream status %d", resp.StatusCode)
		if len(raw) > 0 {
			msg = fmt.Sprintf("%s: %s", msg, redactSecrets(truncate(string(raw), 400), key))
		}
		return Reply{}, RelayError{Err: fmt.Errorf("%s", msg), FirstByteSent: false}
	}

	content, tokens, serr := readAntigravitySSE(resp.Body)
	if serr != nil {
		if content != "" {
			return Reply{}, RelayError{Err: serr, FirstByteSent: true}
		}
		return Reply{}, RelayError{Err: serr, FirstByteSent: false}
	}
	if content == "" {
		return Reply{}, RelayError{
			Err:           fmt.Errorf("upstream returned no text content"),
			FirstByteSent: false,
		}
	}
	return Reply{Content: content, Tokens: tokens}, nil
}

// readAntigravitySSE aggregates v1internal Gemini SSE into one text reply.
func readAntigravitySSE(r io.Reader) (string, int64, error) {
	br := bufio.NewReader(r)
	var text strings.Builder
	var tokens int64

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			if text.Len() > 0 {
				return text.String(), tokens, err
			}
			return "", 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		// Prefer unwrapped response; fall back to raw Gemini generateContent shape.
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			continue
		}
		raw := []byte(payload)
		if respRaw, ok := envelope["response"]; ok && len(respRaw) > 0 && string(respRaw) != "null" {
			raw = respRaw
		}

		var gemini struct {
			Candidates []struct {
				Content *struct {
					Parts []struct {
						Text    string `json:"text"`
						Thought bool   `json:"thought"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			UsageMetadata *struct {
				PromptTokenCount     int64 `json:"promptTokenCount"`
				CandidatesTokenCount int64 `json:"candidatesTokenCount"`
				TotalTokenCount      int64 `json:"totalTokenCount"`
				ThoughtsTokenCount   int64 `json:"thoughtsTokenCount"`
			} `json:"usageMetadata"`
		}
		if err := json.Unmarshal(raw, &gemini); err != nil {
			continue
		}
		for _, c := range gemini.Candidates {
			if c.Content == nil {
				continue
			}
			for _, p := range c.Content.Parts {
				if p.Thought || p.Text == "" {
					continue
				}
				text.WriteString(p.Text)
			}
		}
		if gemini.UsageMetadata != nil {
			if gemini.UsageMetadata.TotalTokenCount > 0 {
				tokens = gemini.UsageMetadata.TotalTokenCount
			} else {
				tokens = gemini.UsageMetadata.PromptTokenCount + gemini.UsageMetadata.CandidatesTokenCount + gemini.UsageMetadata.ThoughtsTokenCount
			}
		}
	}
	if text.Len() == 0 {
		return "", 0, fmt.Errorf("antigravity stream ended without content")
	}
	return text.String(), tokens, nil
}

// Stream: v1 converts via non-stream Call and emits a single OpenAI-shaped SSE
// completion (same pattern as Claude/Codex).
func (antigravityProvider) Stream(a model.Account, req ChatRequest, w http.ResponseWriter) (int64, error) {
	reply, err := (antigravityProvider{}).Call(a, req)
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
