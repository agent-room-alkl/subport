package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/agent-room-alkl/subport/internal/gateway"
	"github.com/agent-room-alkl/subport/internal/model"
)

// handleResponses serves POST /v1/responses (OpenAI Responses API shape).
// Workable MVP: convert input to ChatRequest, Relay, return a responses-shaped JSON.
func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	key, owner, keyErr := s.Store.KeyBySecret(bearer(r))
	if keyErr != nil {
		fail(w, http.StatusUnauthorized, "invalid or missing API key")
		return
	}

	var in struct {
		Model        string          `json:"model"`
		Instructions string          `json:"instructions"`
		Input        json.RawMessage `json:"input"`
		Stream       bool            `json:"stream"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if in.Model == "" {
		in.Model = "gpt-4o-mini"
	}

	req := gateway.ChatRequest{Model: in.Model, Stream: in.Stream}
	if strings.TrimSpace(in.Instructions) != "" {
		req.Messages = append(req.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "system", Content: in.Instructions})
	}
	for _, m := range parseResponsesInput(in.Input) {
		req.Messages = append(req.Messages, m)
	}
	if len(req.Messages) == 0 {
		fail(w, http.StatusBadRequest, "input is required")
		return
	}

	held, admitErr := admit(s.Store, owner, req)
	if admitErr != nil {
		if isQuotaExhausted(admitErr) {
			fail(w, http.StatusPaymentRequired, "quota exhausted")
			return
		}
		fail(w, http.StatusInternalServerError, "could not check quota")
		return
	}
	settled := false
	defer func() {
		if !settled {
			_ = s.Store.ReleaseQuota(owner.ID, held)
		}
	}()

	var res gateway.Result
	var relayErr error
	if req.Stream {
		// Native OpenAI Responses SSE (transformed from chat-completions frames).
		sw := gateway.NewResponsesSSEWriter(w, req.Model)
		res, relayErr = s.Sched.RelayStream(req, sw)
	} else {
		res, relayErr = s.Sched.Relay(req)
	}
	s.Store.TouchKey(key.ID, time.Now().UTC().Format("2006-01-02 15:04"))

	if relayErr != nil {
		s.settleFailed(owner, key, req, res, held, &settled, relayErr, w)
		return
	}

	tokens := res.Tokens
	if tokens <= 0 {
		tokens = int64(len(res.Text))
	}
	counts := model.TokenCounts{Total: tokens}
	cost, billedAt := s.Store.PriceCall(owner.ID, req.Model, counts)
	_ = s.Store.SettleUsage(model.UsageLog{
		UserID: owner.ID, KeyID: key.ID, Model: req.Model,
		AccountID: res.Account.ID, Tokens: tokens,
		TokenParts: counts, BilledAt: billedAt, Cost: cost,
		Status: "success", Attempts: res.Attempts,
	}, held)
	settled = true

	if req.Stream {
		return
	}
	jsonOut(w, map[string]any{
		"id":     "resp_subport",
		"object": "response",
		"model":  req.Model,
		"status": "completed",
		"output": []any{
			map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": res.Text},
				},
			},
		},
		"usage": map[string]any{"total_tokens": tokens},
	})
}

func parseResponsesInput(raw json.RawMessage) []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
} {
	var out []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if len(raw) == 0 {
		return out
	}
	// string input
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		if strings.TrimSpace(asString) != "" {
			out = append(out, struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{Role: "user", Content: asString})
		}
		return out
	}
	// array of items
	var items []map[string]any
	if json.Unmarshal(raw, &items) != nil {
		return out
	}
	for _, it := range items {
		role, _ := it["role"].(string)
		if role == "" {
			role = "user"
		}
		content := ""
		switch c := it["content"].(type) {
		case string:
			content = c
		case []any:
			var parts []string
			for _, p := range c {
				if m, ok := p.(map[string]any); ok {
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
			content = strings.Join(parts, "")
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		out = append(out, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: role, Content: content})
	}
	return out
}

// handleMessages serves Anthropic-compatible POST /v1/messages.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	key, owner, keyErr := s.Store.KeyBySecret(bearer(r))
	if keyErr != nil {
		fail(w, http.StatusUnauthorized, "invalid or missing API key")
		return
	}

	var in struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		System    any    `json:"system"`
		Stream    bool   `json:"stream"`
		Messages  []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if in.Model == "" {
		in.Model = "claude-sonnet-4-5"
	}

	req := gateway.ChatRequest{Model: in.Model, Stream: in.Stream}
	if sys := anthropicSystemToString(in.System); sys != "" {
		req.Messages = append(req.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "system", Content: sys})
	}
	for _, m := range in.Messages {
		req.Messages = append(req.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: m.Role, Content: anthropicContentToString(m.Content)})
	}
	if len(req.Messages) == 0 {
		fail(w, http.StatusBadRequest, "messages are required")
		return
	}

	held, admitErr := admit(s.Store, owner, req)
	if admitErr != nil {
		if isQuotaExhausted(admitErr) {
			fail(w, http.StatusPaymentRequired, "quota exhausted")
			return
		}
		fail(w, http.StatusInternalServerError, "could not check quota")
		return
	}
	settled := false
	defer func() {
		if !settled {
			_ = s.Store.ReleaseQuota(owner.ID, held)
		}
	}()

	var res gateway.Result
	var relayErr error
	if req.Stream {
		// Native Anthropic Messages SSE (transformed from chat-completions frames).
		sw := gateway.NewAnthropicSSEWriter(w, req.Model)
		res, relayErr = s.Sched.RelayStream(req, sw)
	} else {
		res, relayErr = s.Sched.Relay(req)
	}
	s.Store.TouchKey(key.ID, time.Now().UTC().Format("2006-01-02 15:04"))

	if relayErr != nil {
		s.settleFailed(owner, key, req, res, held, &settled, relayErr, w)
		return
	}

	tokens := res.Tokens
	if tokens <= 0 {
		tokens = int64(len(res.Text))
	}
	counts := model.TokenCounts{Total: tokens}
	cost, billedAt := s.Store.PriceCall(owner.ID, req.Model, counts)
	_ = s.Store.SettleUsage(model.UsageLog{
		UserID: owner.ID, KeyID: key.ID, Model: req.Model,
		AccountID: res.Account.ID, Tokens: tokens,
		TokenParts: counts, BilledAt: billedAt, Cost: cost,
		Status: "success", Attempts: res.Attempts,
	}, held)
	settled = true

	if req.Stream {
		return
	}
	jsonOut(w, map[string]any{
		"id":    "msg_subport",
		"type":  "message",
		"role":  "assistant",
		"model": req.Model,
		"content": []any{
			map[string]any{"type": "text", "text": res.Text},
		},
		"stop_reason": "end_turn",
		"usage": map[string]any{
			"input_tokens":  0,
			"output_tokens": tokens,
		},
	})
}

func anthropicSystemToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var parts []string
		for _, p := range t {
			if m, ok := p.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func anthropicContentToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var parts []string
		for _, p := range t {
			if m, ok := p.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

// settleFailed mirrors handleChat failure billing / error response path.
func (s *Server) settleFailed(owner model.User, key model.APIKey, req gateway.ChatRequest, res gateway.Result, held int64, settled *bool, relayErr error, w http.ResponseWriter) {
	var re gateway.RelayError
	broken := asRelay(relayErr, &re) && re.FirstByteSent
	status := "failed"
	var cost int64
	if broken {
		status = "stream_broken"
		cost = 50
	}
	_ = s.Store.SettleUsage(model.UsageLog{
		UserID: owner.ID, KeyID: key.ID, Model: req.Model,
		AccountID: res.Account.ID, Status: status, StreamBroken: broken,
		Cost: cost, Attempts: res.Attempts,
	}, held)
	*settled = true
	if broken {
		_, _ = s.Store.CompensateBrokenStreams(owner.ID, s.CompCfg)
		if req.Stream {
			return
		}
		http.Error(w, relayErr.Error(), http.StatusBadGateway)
		return
	}
	http.Error(w, "no healthy account", http.StatusServiceUnavailable)
}