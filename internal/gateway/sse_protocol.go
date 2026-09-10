package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// protocolSSEWriter converts OpenAI chat-completions SSE (what RelayStream /
// providers emit today) into a client-facing protocol shape without changing
// upstream adapters. First client byte is deferred until the first upstream
// content delta arrives, preserving Subport first-byte failover. stream_broken
// error frames are re-emitted in the target protocol; RelayError.FirstByteSent
// compensation is unchanged because it keys off the error, not the frame body.
type protocolSSEWriter struct {
	http.ResponseWriter
	format  string // "anthropic" | "responses"
	model   string
	buf     bytes.Buffer
	started bool
	closed  bool // saw [DONE] or terminal error
	textLen int
	msgID   string
	respID  string
}

// NewAnthropicSSEWriter wraps w so chat-completions SSE is rewritten as
// Anthropic Messages streaming events (message_start, content_block_*, message_stop).
func NewAnthropicSSEWriter(w http.ResponseWriter, model string) http.ResponseWriter {
	return &protocolSSEWriter{ResponseWriter: w, format: "anthropic", model: model, msgID: "msg_subport"}
}

// NewResponsesSSEWriter wraps w so chat-completions SSE is rewritten as
// OpenAI Responses streaming events (response.created, response.output_text.delta, response.completed).
func NewResponsesSSEWriter(w http.ResponseWriter, model string) http.ResponseWriter {
	return &protocolSSEWriter{ResponseWriter: w, format: "responses", model: model, respID: "resp_subport"}
}

func (p *protocolSSEWriter) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	_, _ = p.buf.Write(b)
	for {
		data := p.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := string(data[:idx])
		p.buf.Next(idx + 1)
		if err := p.handleLine(strings.TrimRight(line, "\r")); err != nil {
			return len(b), err
		}
	}
	return len(b), nil
}

func (p *protocolSSEWriter) Flush() {
	if fl, ok := p.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

func (p *protocolSSEWriter) handleLine(line string) error {
	if line == "" || p.closed {
		return nil
	}
	if !strings.HasPrefix(line, "data:") {
		return nil
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "[DONE]" {
		return p.writeDone()
	}
	if data == "" {
		return nil
	}
	var frame map[string]any
	if err := json.Unmarshal([]byte(data), &frame); err != nil {
		return nil
	}
	if errObj, ok := frame["error"].(map[string]any); ok {
		return p.writeBroken(errObj)
	}
	content := extractChatDeltaContent(frame)
	if content == "" {
		return nil
	}
	return p.writeDelta(content)
}

func extractChatDeltaContent(frame map[string]any) string {
	choices, _ := frame["choices"].([]any)
	var out strings.Builder
	for _, c := range choices {
		m, _ := c.(map[string]any)
		if m == nil {
			continue
		}
		delta, _ := m["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		if s, ok := delta["content"].(string); ok {
			out.WriteString(s)
		}
	}
	return out.String()
}

func (p *protocolSSEWriter) ensureStarted() error {
	if p.started {
		return nil
	}
	ensureSSEHeaders(p.ResponseWriter)
	p.started = true
	switch p.format {
	case "anthropic":
		if err := p.writeAnthropicEvent("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            p.msgID,
				"type":          "message",
				"role":          "assistant",
				"model":         p.model,
				"content":       []any{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		}); err != nil {
			return err
		}
		return p.writeAnthropicEvent("content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]any{"type": "text", "text": ""},
		})
	case "responses":
		return p.writeResponsesEvent("response.created", map[string]any{
			"type": "response.created",
			"response": map[string]any{
				"id":     p.respID,
				"object": "response",
				"model":  p.model,
				"status": "in_progress",
			},
		})
	}
	return nil
}

func (p *protocolSSEWriter) writeDelta(content string) error {
	if err := p.ensureStarted(); err != nil {
		return err
	}
	p.textLen += len(content)
	switch p.format {
	case "anthropic":
		return p.writeAnthropicEvent("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": content},
		})
	case "responses":
		return p.writeResponsesEvent("response.output_text.delta", map[string]any{
			"type":  "response.output_text.delta",
			"delta": content,
		})
	}
	return nil
}

func (p *protocolSSEWriter) writeDone() error {
	if p.closed {
		return nil
	}
	if err := p.ensureStarted(); err != nil {
		return err
	}
	p.closed = true
	switch p.format {
	case "anthropic":
		if err := p.writeAnthropicEvent("content_block_stop", map[string]any{
			"type": "content_block_stop", "index": 0,
		}); err != nil {
			return err
		}
		if err := p.writeAnthropicEvent("message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": p.textLen},
		}); err != nil {
			return err
		}
		return p.writeAnthropicEvent("message_stop", map[string]any{"type": "message_stop"})
	case "responses":
		return p.writeResponsesEvent("response.completed", map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":     p.respID,
				"object": "response",
				"model":  p.model,
				"status": "completed",
				"usage":  map[string]any{"total_tokens": p.textLen},
			},
		})
	}
	return nil
}

func (p *protocolSSEWriter) writeBroken(errObj map[string]any) error {
	if !p.started {
		ensureSSEHeaders(p.ResponseWriter)
		p.started = true
	}
	p.closed = true
	msg, _ := errObj["message"].(string)
	typ, _ := errObj["type"].(string)
	if typ == "" {
		typ = "stream_broken"
	}
	switch p.format {
	case "anthropic":
		return p.writeAnthropicEvent("error", map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    typ,
				"message": msg,
			},
		})
	case "responses":
		return p.writeResponsesEvent("error", map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    typ,
				"message": msg,
			},
		})
	}
	return nil
}

func (p *protocolSSEWriter) writeAnthropicEvent(event string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("event: ")
	b.WriteString(event)
	b.WriteString("\ndata: ")
	b.Write(raw)
	b.WriteString("\n\n")
	if _, err := io.WriteString(p.ResponseWriter, b.String()); err != nil {
		return err
	}
	p.Flush()
	return nil
}

func (p *protocolSSEWriter) writeResponsesEvent(event string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("event: ")
	b.WriteString(event)
	b.WriteString("\ndata: ")
	b.Write(raw)
	b.WriteString("\n\n")
	if _, err := io.WriteString(p.ResponseWriter, b.String()); err != nil {
		return err
	}
	p.Flush()
	return nil
}
