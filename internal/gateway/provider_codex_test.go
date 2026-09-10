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

func TestCodexAdapterSendsResponsesAPI(t *testing.T) {
	var gotPath, gotAuth, gotAccount, gotOriginator, gotUA string
	var gotBody codexRequest

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		gotOriginator = r.Header.Get("originator")
		gotUA = r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello from codex\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":9,\"output_tokens\":4,\"total_tokens\":13}}}\n\n")
	}))
	defer stub.Close()

	t.Setenv("SUBPORT_CODEX_ACCESS_TOKEN", "oauth-access-token-test")
	t.Setenv("SUBPORT_CODEX_ACCOUNT_ID", "acct-test-chatgpt")
	codexSetRuntime("", "", "", 0)

	acct := model.Account{ID: "acct-codex-1", Provider: "codex", BaseURL: stub.URL}
	req := ChatRequest{Model: "gpt-5.5"}
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
	if gotPath != "/backend-api/codex/responses" {
		t.Errorf("path = %q, want /backend-api/codex/responses", gotPath)
	}
	if gotAuth != "Bearer oauth-access-token-test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotAccount != "acct-test-chatgpt" {
		t.Errorf("chatgpt-account-id = %q", gotAccount)
	}
	if gotOriginator != codexOriginator {
		t.Errorf("originator = %q", gotOriginator)
	}
	if !strings.HasPrefix(gotUA, "codex-tui/") {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotBody.Instructions != "be brief" {
		t.Errorf("instructions = %q", gotBody.Instructions)
	}
	if gotBody.Store {
		t.Errorf("store should be false for OAuth")
	}
	if !gotBody.Stream {
		t.Errorf("stream should be true for Codex OAuth")
	}
	if len(gotBody.Input) != 1 || gotBody.Input[0].Role != "user" {
		t.Errorf("input = %+v", gotBody.Input)
	}
	if reply.Content != "hello from codex" {
		t.Errorf("content = %q", reply.Content)
	}
	if reply.Tokens != 13 {
		t.Errorf("tokens = %d, want 13", reply.Tokens)
	}
}

func TestCodexRejectsClaudeModel(t *testing.T) {
	acct := model.Account{ID: "acct-codex-1", Provider: "codex", BaseURL: "http://127.0.0.1:9"}
	req := ChatRequest{Model: "claude-sonnet-4-5"}
	req.Messages = append(req.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: "hi"})
	_, err := CallUpstream(acct, req)
	if err == nil {
		t.Fatal("expected error for claude model on codex provider")
	}
}

func TestCodexProviderRegistered(t *testing.T) {
	p, err := ProviderFor("codex")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "codex" {
		t.Errorf("name = %q", p.Name())
	}
}
