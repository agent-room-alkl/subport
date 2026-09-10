package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
)

func TestClaudeAdapterSendsAnthropicMessages(t *testing.T) {
	var gotPath, gotAuth, gotVersion, gotBeta, gotUA string
	var gotBody anthropicRequest

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("anthropic-version")
		gotBeta = r.Header.Get("anthropic-beta")
		gotUA = r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"content":[{"type":"text","text":"hello from claude"}],
			"usage":{"input_tokens":11,"output_tokens":7}
		}`))
	}))
	defer stub.Close()

	t.Setenv("SUBPORT_PROVIDER_KEY_CLAUDE", "oauth-access-token-test")
	claudeSetRuntime("", "", 0) // clear in-memory so env wins

	acct := model.Account{ID: "acct-claude-1", Provider: "claude", BaseURL: stub.URL}
	req := ChatRequest{Model: "claude-sonnet-4-20250514"}
	req.Messages = append(req.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "system", Content: "be brief"})
	req.Messages = append(req.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: "hi"})

	reply, err := CallUpstream(acct, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotAuth != "Bearer oauth-access-token-test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotVersion != claudeAPIVersion {
		t.Errorf("anthropic-version = %q", gotVersion)
	}
	if !strings.Contains(gotBeta, "oauth-2025-04-20") || !strings.Contains(gotBeta, "claude-code-20250219") {
		t.Errorf("anthropic-beta = %q, want oauth + claude-code betas", gotBeta)
	}
	if !strings.HasPrefix(gotUA, "claude-cli/") {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotBody.System != "be brief" {
		t.Errorf("system = %q", gotBody.System)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Role != "user" || gotBody.Messages[0].Content != "hi" {
		t.Errorf("messages = %+v", gotBody.Messages)
	}
	if reply.Content != "hello from claude" {
		t.Errorf("content = %q", reply.Content)
	}
	if reply.Tokens != 18 {
		t.Errorf("tokens = %d, want 18", reply.Tokens)
	}
}

func TestClaudeProviderRegistered(t *testing.T) {
	p, err := ProviderFor("claude")
	if err != nil {
		t.Fatalf("ProviderFor(claude): %v", err)
	}
	if p.Name() != "claude" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestOpenAIToAnthropicMapsRoles(t *testing.T) {
	req := ChatRequest{Model: "claude-sonnet-4-5"}
	req.Messages = append(req.Messages,
		struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "system", Content: "sys"},
		struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "user", Content: "u1"},
		struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "assistant", Content: "a1"},
		struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "user", Content: "u2"},
	)
	out, err := openAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Model != "claude-sonnet-4-5" {
		t.Errorf("model = %q", out.Model)
	}
	if out.System != "sys" {
		t.Errorf("system = %q", out.System)
	}
	if len(out.Messages) != 3 {
		t.Fatalf("len messages = %d", len(out.Messages))
	}
}

func TestClaudeRejectsGPTModel(t *testing.T) {
	_, err := openAIToAnthropic(ChatRequest{Model: "gpt-4o"})
	if err == nil {
		t.Fatal("expected error for gpt model on claude provider")
	}
}

func TestClaudeRedactsCredentialInError(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"Invalid Bearer oauth-secret-xyz"}}`))
	}))
	defer stub.Close()

	t.Setenv("SUBPORT_PROVIDER_KEY_CLAUDE", "oauth-secret-xyz")
	claudeSetRuntime("", "", 0)

	req := ChatRequest{}
	req.Messages = append(req.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: "hi"})
	_, err := CallUpstream(model.Account{Provider: "claude", BaseURL: stub.URL}, req)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "oauth-secret-xyz") {
		t.Fatalf("credential leaked: %q", err.Error())
	}
}