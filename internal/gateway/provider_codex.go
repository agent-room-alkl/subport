package gateway

// ChatGPT Codex subscription (OAuth) provider.
//
// OpenAI chat/completions → chatgpt.com/backend-api/codex/responses (Responses API).
// Uses Codex CLI / ChatGPT subscription OAuth, not Platform sk- API keys.

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
	codexDefaultBaseURL = "https://chatgpt.com"
	codexResponsesPath  = "/backend-api/codex/responses"
	codexDefaultModel   = "gpt-5.5"
	codexOriginator     = "codex-tui"
	codexCLIVersion     = "0.146.0"
	codexCLIUserAgent   = codexOriginator + "/" + codexCLIVersion + " (Windows NT 10.0; Win64) xterm-256color"
	codexDefaultInstructions = "You are a helpful coding assistant."
)

func init() {
	providers["codex"] = codexProvider{}
}

type codexProvider struct{}

func (codexProvider) Name() string { return "codex" }

type codexContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexInputItem struct {
	Role    string             `json:"role"`
	Content []codexContentPart `json:"content"`
}

type codexRequest struct {
	Model            string           `json:"model"`
	Instructions     string           `json:"instructions,omitempty"`
	Input            []codexInputItem `json:"input"`
	Stream           bool             `json:"stream"`
	Store            bool             `json:"store"`
}

type codexResponse struct {
	Status string `json:"status"`
	Output []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage *struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func codexBaseURL(a model.Account) string {
	if strings.TrimSpace(a.BaseURL) != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return codexDefaultBaseURL
}

func codexAcceptsModel(modelName string) bool {
	m := strings.ToLower(strings.TrimSpace(modelName))
	if m == "" {
		return true
	}
	// Leave Claude / Antigravity Gemini models for their providers.
	if strings.HasPrefix(m, "claude") {
		return false
	}
	if strings.HasPrefix(m, "gemini") || strings.HasPrefix(m, "tab_flash") || strings.HasPrefix(m, "gpt-oss") {
		return false
	}
	return true
}

func openAIToCodex(req ChatRequest) (codexRequest, error) {
	modelName := strings.TrimSpace(req.Model)
	if !codexAcceptsModel(modelName) {
		return codexRequest{}, fmt.Errorf("codex: model %q belongs to another provider", modelName)
	}
	if modelName == "" || strings.HasPrefix(strings.ToLower(modelName), "claude") {
		modelName = codexDefaultModel
	}

	var systemParts []string
	inputs := make([]codexInputItem, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		content := m.Content
		switch role {
		case "system":
			if strings.TrimSpace(content) != "" {
				systemParts = append(systemParts, content)
			}
		case "assistant":
			inputs = append(inputs, codexInputItem{
				Role: "assistant",
				Content: []codexContentPart{{
					Type: "output_text",
					Text: content,
				}},
			})
		default:
			inputs = append(inputs, codexInputItem{
				Role: "user",
				Content: []codexContentPart{{
					Type: "input_text",
					Text: content,
				}},
			})
		}
	}
	if len(inputs) == 0 {
		return codexRequest{}, fmt.Errorf("codex: at least one user/assistant message is required")
	}

	instructions := codexDefaultInstructions
	if len(systemParts) > 0 {
		instructions = strings.Join(systemParts, "\n\n")
	}

	return codexRequest{
		Model:           modelName,
		Instructions:    instructions,
		Input:           inputs,
		// ChatGPT Codex OAuth endpoint rejects stream:false ("Stream must be set to true").
		Stream:          true,
		Store:           false,
	}, nil
}

func setCodexHeaders(req *http.Request, accessToken, accountID string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", codexCLIUserAgent)
	req.Header.Set("originator", codexOriginator)
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	if accountID != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}
	// ChatGPT internal API expects Host chatgpt.com (even if BaseURL is overridden in tests).
	if strings.Contains(req.URL.Host, "chatgpt.com") {
		req.Host = "chatgpt.com"
	}
}

func extractCodexText(out codexResponse) string {
	var text strings.Builder
	for _, item := range out.Output {
		if item.Type != "" && item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" || part.Type == "text" || part.Type == "" {
				text.WriteString(part.Text)
			}
		}
	}
	return text.String()
}

func (codexProvider) Call(a model.Account, req ChatRequest) (Reply, error) {
	payload, err := openAIToCodex(req)
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}

	url := codexBaseURL(a) + codexResponsesPath
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	key := codexCredentialFor(a.ID)
	accountID := codexAccountIDFor(a.ID)
	setCodexHeaders(httpReq, key, accountID)

	resp, err := HTTPClientFor(a).Do(httpReq)
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		if refreshed, rerr := codexTryRefresh(); rerr == nil && refreshed {
			key = codexCredentialFor(a.ID)
			accountID = codexAccountIDFor(a.ID)
			httpReq2, err2 := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
			if err2 != nil {
				return Reply{}, RelayError{Err: err2, FirstByteSent: false}
			}
			setCodexHeaders(httpReq2, key, accountID)
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
		var out codexResponse
		_ = json.Unmarshal(raw, &out)
		if out.Error != nil && out.Error.Message != "" {
			msg = fmt.Sprintf("%s: %s", msg, redactSecrets(out.Error.Message, key))
		} else if len(raw) > 0 {
			msg = fmt.Sprintf("%s: %s", msg, redactSecrets(truncate(string(raw), 400), key))
		}
		return Reply{}, RelayError{Err: fmt.Errorf("%s", msg), FirstByteSent: false}
	}

	content, tokens, serr := readCodexSSE(resp.Body)
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

// readCodexSSE aggregates ChatGPT Codex Responses SSE into one text reply.
func readCodexSSE(r io.Reader) (string, int64, error) {
	br := bufio.NewReader(r)
	var text strings.Builder
	var tokens int64
	completed := false

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
		var evt struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response *struct {
				Status string `json:"status"`
				Output []struct {
					Type    string `json:"type"`
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"output"`
				Usage *struct {
					InputTokens  int64 `json:"input_tokens"`
					OutputTokens int64 `json:"output_tokens"`
					TotalTokens  int64 `json:"total_tokens"`
				} `json:"usage"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"response"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}
		switch evt.Type {
		case "response.output_text.delta":
			text.WriteString(evt.Delta)
		case "response.completed", "response.done":
			completed = true
			if evt.Response != nil {
				if text.Len() == 0 {
					for _, item := range evt.Response.Output {
						if item.Type != "" && item.Type != "message" {
							continue
						}
						for _, part := range item.Content {
							if part.Type == "output_text" || part.Type == "text" || part.Type == "" {
								text.WriteString(part.Text)
							}
						}
					}
				}
				if evt.Response.Usage != nil {
					if evt.Response.Usage.TotalTokens > 0 {
						tokens = evt.Response.Usage.TotalTokens
					} else {
						tokens = evt.Response.Usage.InputTokens + evt.Response.Usage.OutputTokens
					}
				}
			}
		case "response.failed", "error":
			msg := "codex response failed"
			if evt.Error != nil && evt.Error.Message != "" {
				msg = evt.Error.Message
			} else if evt.Response != nil && evt.Response.Error != nil && evt.Response.Error.Message != "" {
				msg = evt.Response.Error.Message
			}
			return text.String(), tokens, fmt.Errorf("%s", msg)
		}
	}
	if text.Len() == 0 && !completed {
		return "", 0, fmt.Errorf("codex stream ended without content")
	}
	return text.String(), tokens, nil
}

// Stream: v1 converts via non-stream Call and emits a single OpenAI-shaped SSE
// completion. True Codex SSE passthrough is a TODO.
func (codexProvider) Stream(a model.Account, req ChatRequest, w http.ResponseWriter) (int64, error) {
	reply, err := (codexProvider{}).Call(a, req)
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
