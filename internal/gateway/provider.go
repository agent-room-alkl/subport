package gateway

// Upstream providers.
//
// Everything above this file - scheduling, failover, the first-byte rule -
// is provider-agnostic. A provider's only job is to turn one ChatRequest into
// one Reply, or into a RelayError that says whether output had already begun.
// Adding a provider must never require touching the scheduler.
//
// Credentials are read from the environment and never stored, logged, or
// returned by any API. An account row carries where to call, not what to call
// it with.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

// Reply is one upstream answer. Tokens is the provider's own usage count when
// it reports one, and 0 when it does not - callers must treat 0 as "unknown"
// rather than "free", or a provider that omits usage would silently bill
// nothing.
type Reply struct {
	Content string
	Tokens  int64
}

type Provider interface {
	Name() string
	Call(a model.Account, req ChatRequest) (Reply, error)
	Stream(a model.Account, req ChatRequest, w http.ResponseWriter) (tokens int64, err error)
}

var providers = map[string]Provider{
	"mock":   mockProvider{},
	"openai": openAIProvider{},
}

// ProviderFor fails closed. An account naming a provider we do not implement
// must stop the request, not quietly fall back to the demo upstream - that
// would look like it was working while talking to nothing real.
func ProviderFor(name string) (Provider, error) {
	p, ok := providers[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, fmt.Errorf("no adapter for provider %q", name)
	}
	return p, nil
}

// credentialFor resolves an account's upstream key from the environment.
// SUBPORT_PROVIDER_KEY_<PROVIDER> wins over the shared SUBPORT_PROVIDER_KEY so
// several providers can be configured at once.
func credentialFor(provider string) string {
	specific := "SUBPORT_PROVIDER_KEY_" + strings.ToUpper(strings.TrimSpace(provider))
	if v := os.Getenv(specific); v != "" {
		return v
	}
	return os.Getenv("SUBPORT_PROVIDER_KEY")
}

// bearerPattern matches an Authorization-shaped token anywhere in a string.
// Upstreams and proxies sometimes echo the request headers back inside an
// error body; we must never pass that through to a caller.
var bearerPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`)

// redactSecrets scrubs an upstream-supplied string before it is shown to
// anyone. It removes the exact credential we presented, then any remaining
// Authorization-shaped token, so a hostile or careless upstream cannot use
// its error message as a channel for handing our key back to the caller.
//
// The provider's own wording is preserved otherwise - distinguishing a quota
// problem from a rate limit is the reason for carrying it at all.
func redactSecrets(msg, credential string) string {
	if credential != "" {
		msg = strings.ReplaceAll(msg, credential, "[REDACTED]")
	}
	return bearerPattern.ReplaceAllString(msg, "Bearer [REDACTED]")
}

// ---------------------------------------------------------------- mock
//
// The in-repo demo upstream. Kept deliberately: when a real provider starts
// failing, running the same request through mock separates "the adapter is
// wrong" from "the gateway is wrong".

type mockProvider struct{}

func (mockProvider) Name() string { return "mock" }

func (mockProvider) Call(a model.Account, req ChatRequest) (Reply, error) {
	body, _ := json.Marshal(req)
	resp, err := http.Post(
		a.BaseURL+"/mock/upstream?account="+a.ID,
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		// Connection never established, so nothing was written to the client.
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	defer resp.Body.Close()

	var out struct {
		Content string `json:"content"`
	}
	decodeErr := json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 {
		return Reply{}, RelayError{
			Err:           fmt.Errorf("upstream status %d", resp.StatusCode),
			FirstByteSent: false,
		}
	}
	if decodeErr != nil {
		// A 200 whose body is truncated: the connection was established and
		// output began, so this is the stream-broken case. Billed for what was
		// produced and marked for compensation, never retried elsewhere.
		return Reply{}, RelayError{
			Err:           fmt.Errorf("stream truncated: %v", decodeErr),
			FirstByteSent: true,
		}
	}
	return Reply{Content: out.Content}, nil
}

// ---------------------------------------------------------------- openai
//
// OpenAI-compatible chat completions. This one adapter also covers the many
// providers that expose the same shape; only BaseURL and the key change.

type openAIProvider struct{}

func (openAIProvider) Name() string { return "openai" }

type openAIRequest struct {
	Model         string               `json:"model"`
	Messages      []openAIChatMessage  `json:"messages"`
	Stream        bool                 `json:"stream"`
	StreamOptions *openAIStreamOptions `json:"stream_options,omitempty"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int64 `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

var upstreamClient = &http.Client{Timeout: 120 * time.Second}

func (openAIProvider) Call(a model.Account, req ChatRequest) (Reply, error) {
	msgs := make([]openAIChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, openAIChatMessage{Role: m.Role, Content: m.Content})
	}
	body, err := json.Marshal(openAIRequest{Model: req.Model, Messages: msgs})
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}

	url := strings.TrimRight(a.BaseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	key := credentialFor(a.Provider)
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := upstreamClient.Do(httpReq)
	if err != nil {
		// No response at all: safe to try the next account.
		return Reply{}, RelayError{Err: err, FirstByteSent: false}
	}
	defer resp.Body.Close()

	var out openAIResponse
	decodeErr := json.NewDecoder(resp.Body).Decode(&out)

	if resp.StatusCode >= 300 {
		msg := fmt.Sprintf("upstream status %d", resp.StatusCode)
		if out.Error != nil && out.Error.Message != "" {
			// Carry the provider's own words - "insufficient_quota" and
			// "rate_limit" need different operator responses, and a bare
			// status code hides which one happened. But scrub first: this
			// text can reach an API client, and an upstream or proxy that
			// echoes the Authorization header back would otherwise hand our
			// own credential to the caller.
			msg = fmt.Sprintf("%s: %s", msg, redactSecrets(out.Error.Message, key))
		}
		return Reply{}, RelayError{Err: fmt.Errorf("%s", msg), FirstByteSent: false}
	}
	if decodeErr != nil {
		// This one DOES reach the caller as a 502 body, since a broken stream
		// is reported rather than replayed. A decode error can quote the bytes
		// it choked on, so it goes through the same scrub.
		return Reply{}, RelayError{
			Err:           fmt.Errorf("stream truncated: %v", redactSecrets(decodeErr.Error(), key)),
			FirstByteSent: true,
		}
	}
	if len(out.Choices) == 0 {
		return Reply{}, RelayError{
			Err:           fmt.Errorf("upstream returned no choices"),
			FirstByteSent: false,
		}
	}
	return Reply{
		Content: out.Choices[0].Message.Content,
		Tokens:  out.Usage.TotalTokens,
	}, nil
}
