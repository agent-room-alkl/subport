package gateway

// A broken stream has to tell the client something the client can parse.
// Once bytes are out there is no status code left to set, so the only channel
// still open is another SSE frame.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
)

// parseSSEErrorFrame returns the error object carried by the last data: frame,
// or nil when that frame is not an error. It parses rather than string-matches
// on purpose: the point of the change is that the frame is well formed, and a
// substring check would pass on a malformed one.
func parseSSEErrorFrame(body string) map[string]any {
	var last string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			last = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if last == "" || last == "[DONE]" {
		return nil
	}
	var frame map[string]any
	if err := json.Unmarshal([]byte(last), &frame); err != nil {
		return nil
	}
	errObj, _ := frame["error"].(map[string]any)
	return errObj
}

// hasDoneFrame reports whether any frame in the body is the terminator itself.
func hasDoneFrame(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "data: [DONE]" {
			return true
		}
	}
	return false
}

func TestBrokenStreamEndsWithAParseableErrorFrame(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"half a sen\"}}]}\n\n")
		fl.Flush()
		// Ends without [DONE]: the completion died mid-flight.
	}))
	defer stub.Close()

	rec := httptest.NewRecorder()
	s := NewScheduler([]model.Account{{ID: "a1", Provider: "openai", BaseURL: stub.URL, Priority: 1, Healthy: true}})
	_, err := s.RelayStream(ChatRequest{Stream: true, Model: "gpt-4o"}, rec)
	if err == nil {
		t.Fatal("expected a broken stream")
	}

	body := rec.Body.String()
	errObj := parseSSEErrorFrame(body)
	if errObj == nil {
		t.Fatalf("the client got no parseable error frame; body=%q", body)
	}
	if errObj["type"] != "stream_broken" {
		t.Errorf("error type = %v, want stream_broken", errObj["type"])
	}
	if msg, _ := errObj["message"].(string); msg == "" {
		t.Error("the error frame carries no message")
	}
	// The early content must survive: reporting the cut must not eat what was
	// already produced, and already billed for.
	if !strings.Contains(body, "half a sen") {
		t.Errorf("early content lost: %q", body)
	}
	// A cut must never claim completion. Checked per frame, not as a substring
	// of the whole body: the error message itself says "without [DONE]", and a
	// substring check would fail on its own wording.
	if hasDoneFrame(body) {
		t.Error("a broken stream emitted a [DONE] frame, which tells the client the opposite of what happened")
	}
}

func TestCleanStreamHasNoErrorFrame(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"all good\"}}]}\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer stub.Close()

	rec := httptest.NewRecorder()
	s := NewScheduler([]model.Account{{ID: "a1", Provider: "openai", BaseURL: stub.URL, Priority: 1, Healthy: true}})
	if _, err := s.RelayStream(ChatRequest{Stream: true, Model: "gpt-4o"}, rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := rec.Body.String()
	if parseSSEErrorFrame(body) != nil {
		t.Errorf("a clean stream carries an error frame: %q", body)
	}
	if !hasDoneFrame(body) {
		t.Errorf("a clean stream lost its [DONE] frame: %q", body)
	}
}

// A failure BEFORE the first byte still has its headers, so it must keep
// producing a real HTTP status rather than a frame. Nothing was written, so
// there is no stream for a frame to live in - and inventing one would turn a
// retryable failure into something that looks like partial output.
func TestPreFirstByteFailureEmitsNoFrame(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limit reached"}}`)
	}))
	defer stub.Close()

	rec := httptest.NewRecorder()
	s := NewScheduler([]model.Account{{ID: "a1", Provider: "openai", BaseURL: stub.URL, Priority: 1, Healthy: true}})
	_, err := s.RelayStream(ChatRequest{Stream: true, Model: "gpt-4o"}, rec)
	if err == nil {
		t.Fatal("expected an error")
	}
	if body := rec.Body.String(); body != "" {
		t.Errorf("nothing had been written to the client yet, so nothing should have been emitted; got %q", body)
	}
	var re RelayError
	if !asRelayErr(err, &re) || re.FirstByteSent {
		t.Error("a pre-first-byte failure must stay retryable")
	}
}
