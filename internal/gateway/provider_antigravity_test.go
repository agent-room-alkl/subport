package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
)

func TestAntigravityAcceptsModel(t *testing.T) {
	if !antigravityAcceptsModel("gemini-2.5-flash") {
		t.Fatal("expected gemini accepted")
	}
	if !antigravityAcceptsModel("gemini-3-pro-high") {
		t.Fatal("expected gemini-3 accepted")
	}
	if antigravityAcceptsModel("gpt-5.5") {
		t.Fatal("gpt must be rejected for failover")
	}
	if antigravityAcceptsModel("claude-sonnet-4-5") {
		t.Fatal("plain claude must be rejected for failover to provider=claude")
	}
	if !antigravityAcceptsModel("claude-sonnet-4-5-thinking") {
		t.Fatal("antigravity thinking alias should be accepted")
	}
}

func TestAntigravityCallAggregatesSSE(t *testing.T) {
	antigravityCredMu.Lock()
	prev := antigravityRuntime
	antigravityRuntime = antigravityTokenPair{
		AccessToken:  "test-access",
		RefreshToken: "test-refresh",
		ProjectID:    "proj-123",
	}
	antigravityCredMu.Unlock()
	defer func() {
		antigravityCredMu.Lock()
		antigravityRuntime = prev
		antigravityCredMu.Unlock()
	}()

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access" {
			t.Errorf("auth=%q", r.Header.Get("Authorization"))
		}
		if !strings.Contains(r.URL.Path, "/v1internal:streamGenerateContent") {
			t.Errorf("path=%s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var wrapped map[string]any
		if err := json.Unmarshal(body, &wrapped); err != nil {
			t.Errorf("body: %v", err)
		}
		if wrapped["project"] != "proj-123" {
			t.Errorf("project=%v", wrapped["project"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hello\"},{\"text\":\" world\"}]}}],\"usageMetadata\":{\"totalTokenCount\":9}}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer stub.Close()

	acct := model.Account{ID: "acct-antigravity-1", Provider: "antigravity", BaseURL: stub.URL}
	reply, err := (antigravityProvider{}).Call(acct, ChatRequest{
		Model: "gemini-2.5-flash",
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if reply.Content != "Hello world" {
		t.Fatalf("content=%q", reply.Content)
	}
	if reply.Tokens != 9 {
		t.Fatalf("tokens=%d", reply.Tokens)
	}
}

func TestAntigravityRejectsCodexModel(t *testing.T) {
	acct := model.Account{ID: "acct-antigravity-1", Provider: "antigravity", BaseURL: "http://127.0.0.1:9"}
	_, err := (antigravityProvider{}).Call(acct, ChatRequest{
		Model: "gpt-5.5",
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{{Role: "user", Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "belongs to another provider") {
		t.Fatalf("expected reject, got %v", err)
	}
}
