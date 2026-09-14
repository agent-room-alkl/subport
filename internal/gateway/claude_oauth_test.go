package gateway

import "testing"

func TestClassifyClaudeExchangeHelperUnavailable(t *testing.T) {
	for _, message := range []string{
		"claude_session_exchange.py not found (set SUBPORT_CLAUDE_EXCHANGE_SCRIPT)",
		"python helper failed: exec: python: executable file not found",
		"ModuleNotFoundError: No module named 'curl_cffi'",
	} {
		got := classifyClaudeExchangeErr(testError(message))
		if got == nil || got.Code != "helper_unavailable" {
			t.Fatalf("classify %q: %#v", message, got)
		}
	}
}

type testError string

func (e testError) Error() string { return string(e) }
