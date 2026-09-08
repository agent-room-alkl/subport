package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agent-room-alkl/subport/internal/model"
)

// The openai adapter is only useful if a real provider receives the right
// path, the right auth header and the right body. Asserting on what the
// upstream ACTUALLY received is the point - checking our own return value
// would pass even if we called the wrong URL entirely.
func TestOpenAIAdapterSendsCorrectRequest(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var gotBody openAIRequest

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello from upstream"}}],"usage":{"total_tokens":4242}}`))
	}))
	defer stub.Close()

	t.Setenv("SUBPORT_PROVIDER_KEY_OPENAI", "sk-test-secret")

	acct := model.Account{ID: "acct-1", Provider: "openai", BaseURL: stub.URL}
	req := ChatRequest{Model: "gpt-4o"}
	req.Messages = append(req.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: "hi"})

	reply, err := CallUpstream(acct, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test-secret" {
		t.Errorf("Authorization = %q, want Bearer sk-test-secret", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody.Model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", gotBody.Model)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Content != "hi" {
		t.Errorf("messages not forwarded: %+v", gotBody.Messages)
	}
	if reply.Content != "hello from upstream" {
		t.Errorf("content = %q", reply.Content)
	}
	// Real usage must come through, otherwise billing silently falls back to
	// counting characters.
	if reply.Tokens != 4242 {
		t.Errorf("tokens = %d, want 4242 from the provider's usage block", reply.Tokens)
	}
}

// A provider-specific key must win over the shared one, so several providers
// can be configured at the same time.
func TestProviderSpecificKeyOverridesShared(t *testing.T) {
	var gotAuth string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer stub.Close()

	t.Setenv("SUBPORT_PROVIDER_KEY", "shared-key")
	t.Setenv("SUBPORT_PROVIDER_KEY_OPENAI", "specific-key")

	if _, err := CallUpstream(model.Account{Provider: "openai", BaseURL: stub.URL}, ChatRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer specific-key" {
		t.Errorf("Authorization = %q, want the provider-specific key to win", gotAuth)
	}
}

// An unknown provider must fail closed. Falling through to the mock would look
// like it was working while talking to nothing real.
func TestUnknownProviderFailsClosed(t *testing.T) {
	_, err := CallUpstream(model.Account{ID: "x", Provider: "definitely-not-a-provider"}, ChatRequest{})
	if err == nil {
		t.Fatal("expected an error for an unknown provider, got nil")
	}
	if !strings.Contains(err.Error(), "no adapter for provider") {
		t.Errorf("error = %q, want it to name the missing adapter", err.Error())
	}
	var relayErr RelayError
	if !asRelayErr(err, &relayErr) {
		t.Fatal("expected a RelayError so the scheduler can classify it")
	}
	if relayErr.FirstByteSent {
		t.Error("a configuration fault happens before any output; FirstByteSent must be false")
	}
}

// An upstream error status must stay retryable (pre-first-byte), and must
// carry the provider's own message so an operator can tell a quota problem
// from a rate limit.
func TestUpstreamErrorIsRetryableAndKeepsProviderMessage(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit reached","type":"rate_limit_error"}}`))
	}))
	defer stub.Close()

	_, err := CallUpstream(model.Account{Provider: "openai", BaseURL: stub.URL}, ChatRequest{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "rate limit reached") {
		t.Errorf("error = %q, want the provider's message preserved", err.Error())
	}
	var relayErr RelayError
	if !asRelayErr(err, &relayErr) || relayErr.FirstByteSent {
		t.Error("an error status arrives before output; it must stay retryable")
	}
}

// A 200 with a truncated body is the stream-broken case: output began, so it
// must NOT be replayed on another account.
func TestTruncatedBodyIsNotReplayable(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"half a rep`))
	}))
	defer stub.Close()

	_, err := CallUpstream(model.Account{Provider: "openai", BaseURL: stub.URL}, ChatRequest{})
	if err == nil {
		t.Fatal("expected an error for a truncated body")
	}
	var relayErr RelayError
	if !asRelayErr(err, &relayErr) {
		t.Fatal("expected a RelayError")
	}
	if !relayErr.FirstByteSent {
		t.Error("a truncated 200 means output began; FirstByteSent must be true so it is never replayed")
	}
}

func asRelayErr(err error, target *RelayError) bool {
	re, ok := err.(RelayError)
	if ok {
		*target = re
	}
	return ok
}

// The reject that produced this test: an upstream (or a proxy in front of it)
// that echoes the request's Authorization header inside its error body. We
// carry the provider's wording through to the caller, so without scrubbing we
// would hand our own credential to whoever made the API call.
func TestUpstreamEchoingAuthorizationDoesNotLeakCredential(t *testing.T) {
	const secret = "sk-super-secret-should-never-leak"

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad auth: ` + r.Header.Get("Authorization") + `","type":"invalid_request_error"}}`))
	}))
	defer stub.Close()

	t.Setenv("SUBPORT_PROVIDER_KEY_OPENAI", secret)

	_, err := CallUpstream(model.Account{Provider: "openai", BaseURL: stub.URL}, ChatRequest{})
	if err == nil {
		t.Fatal("expected an error for a 401")
	}
	got := err.Error()
	if strings.Contains(got, secret) {
		t.Fatalf("credential leaked to the caller: %q", got)
	}
	if strings.Contains(got, "Bearer "+secret) {
		t.Fatalf("credential leaked in header form: %q", got)
	}
	// The reason for carrying provider text at all must survive redaction.
	if !strings.Contains(got, "upstream status 401") || !strings.Contains(got, "bad auth") {
		t.Errorf("redaction destroyed the diagnostic: %q", got)
	}
}

// Redaction must not depend on recognising our own key: a proxy can echo some
// other Authorization value, and an operator reading logs should never have to
// wonder whether a Bearer token in an error string was real.
func TestRedactSecretsStripsAnyBearerShape(t *testing.T) {
	cases := []struct {
		name, msg, key, wantAbsent string
	}{
		{"our key, bare", "quota exhausted for sk-mine-1234", "sk-mine-1234", "sk-mine-1234"},
		{"our key, header form", "rejected Bearer sk-mine-1234 upstream", "sk-mine-1234", "sk-mine-1234"},
		{"someone else's bearer", "proxy said Bearer sk-other-9999 is bad", "sk-mine-1234", "sk-other-9999"},
		{"lowercase bearer", "auth failed: bearer sk-other-9999", "", "sk-other-9999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSecrets(tc.msg, tc.key)
			if strings.Contains(got, tc.wantAbsent) {
				t.Errorf("redactSecrets(%q) = %q, still contains %q", tc.msg, got, tc.wantAbsent)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Errorf("redactSecrets(%q) = %q, want a redaction marker", tc.msg, got)
			}
		})
	}
}

// Redaction is a scrub, not a gag: an error with no secret in it must come
// through untouched, or operators lose the ability to tell an out-of-quota
// account from a rate-limited one.
func TestRedactSecretsLeavesCleanMessagesAlone(t *testing.T) {
	msg := "You exceeded your current quota (insufficient_quota)"
	if got := redactSecrets(msg, "sk-mine-1234"); got != msg {
		t.Errorf("redactSecrets mangled a clean message: %q", got)
	}
}

// The scrub has to hold at the seam, not just inside the adapter: Relay's
// returned error is what httpapi hands to an API client on a broken stream.
// Two hostile peers in one tier also re-prove horizontal-first failover with
// the real openai adapter rather than the mock.
func TestRelayDoesNotLeakCredentialAcrossFailover(t *testing.T) {
	const secret = "sk-relay-secret-must-not-escape"

	var hits int
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"proxy rejected ` + r.Header.Get("Authorization") + `"}}`))
	}))
	defer stub.Close()

	t.Setenv("SUBPORT_PROVIDER_KEY_OPENAI", secret)

	s := NewScheduler([]model.Account{
		{ID: "a1", Provider: "openai", BaseURL: stub.URL, Priority: 1, Healthy: true},
		{ID: "a2", Provider: "openai", BaseURL: stub.URL, Priority: 1, Healthy: true},
	})

	_, err := s.Relay(ChatRequest{})
	if err == nil {
		t.Fatal("expected the walk to exhaust both hostile accounts")
	}
	if hits < 2 {
		t.Errorf("upstream hit %d times, want both tier-1 peers tried before giving up", hits)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential reached Relay's caller: %q", err.Error())
	}
}
