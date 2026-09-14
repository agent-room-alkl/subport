package gateway

import (
	"strings"
	"testing"
)

func TestClaudeAISSESuccessMayContainRateLimitMetadata(t *testing.T) {
	raw := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"rate_limit_tier\":\"default_claude_max_5x\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	if err := claudeAISSEError(raw); err != nil {
		t.Fatalf("successful SSE was misclassified: %v", err)
	}
}

func TestClaudeAISSEActualError(t *testing.T) {
	raw := []byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"Too many requests\"}}\n\n")
	err := claudeAISSEError(raw)
	if err == nil || !strings.Contains(err.Error(), "Too many requests") {
		t.Fatalf("expected structured SSE error, got %v", err)
	}
}
