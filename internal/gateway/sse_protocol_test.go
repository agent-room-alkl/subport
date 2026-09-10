package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicSSEWriterTransformsChatCompletions(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewAnthropicSSEWriter(rec, "claude-sonnet-4-5")

	_, err := w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Write([]byte("data: [DONE]\n\n"))
	if err != nil {
		t.Fatal(err)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		`"text":"Hi"`,
		"event: content_block_stop",
		"event: message_delta",
		"event: message_stop",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("chat [DONE] must not leak through:\n%s", body)
	}
	if strings.Contains(body, `"choices"`) {
		t.Fatalf("chat completions shape must not leak:\n%s", body)
	}
}

func TestResponsesSSEWriterTransformsChatCompletions(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewResponsesSSEWriter(rec, "gpt-4o-mini")

	_, err := w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Yo\"}}]}\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Write([]byte("data: [DONE]\n\n"))
	if err != nil {
		t.Fatal(err)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"event: response.created",
		"event: response.output_text.delta",
		`"delta":"Yo"`,
		"event: response.completed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("chat [DONE] must not leak through:\n%s", body)
	}
}

func TestAnthropicSSEWriterStreamBroken(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewAnthropicSSEWriter(rec, "claude-sonnet-4-5")

	_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]string{"message": "cut", "type": "stream_broken"},
	})
	_, err := w.Write([]byte("data: " + string(payload) + "\n\n"))
	if err != nil {
		t.Fatal(err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("want error event:\n%s", body)
	}
	if !strings.Contains(body, `"type":"stream_broken"`) {
		t.Fatalf("want stream_broken preserved:\n%s", body)
	}
	if strings.Contains(body, "event: message_stop") {
		t.Fatalf("broken stream must not emit message_stop:\n%s", body)
	}
}

func TestProtocolSSEWriterDefersFirstByteUntilDelta(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewAnthropicSSEWriter(rec, "claude-sonnet-4-5")

	_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{}}]}\n\n"))
	if rec.Body.Len() != 0 {
		t.Fatalf("expected no client bytes before content, got %q", rec.Body.String())
	}
}
