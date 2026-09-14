package httpapi

import "testing"

func TestImportBaseURLDefaults(t *testing.T) {
	cases := map[string]string{
		"claude":      "https://api.anthropic.com",
		"codex":       "https://chatgpt.com",
		"antigravity": "https://cloudcode-pa.googleapis.com",
		"openai":      "https://api.openai.com",
	}
	for provider, want := range cases {
		if got := importBaseURL(provider, ""); got != want {
			t.Errorf("importBaseURL(%q) = %q, want %q", provider, got, want)
		}
	}
	if got := importBaseURL("openai", " https://gateway.example/v1 "); got != "https://gateway.example/v1" {
		t.Fatalf("explicit base URL = %q", got)
	}
}
