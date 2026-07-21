package ui

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyChatError(t *testing.T) {
	tests := []struct {
		detail  string
		summary string
		hint    string
	}{
		{"openai: HTTP 401", "Authentication failed", "ctrl+k"},
		{"google: rate limit exceeded", "Rate limited", "ctrl+r"},
		{"anthropic: maximum context length exceeded", "Context limit reached", "ctrl+u"},
		{"ollama: dial tcp: connection refused", "Couldn't reach the provider", "endpoint"},
		{"openai: unsupported image", "This model doesn't support part of the request", "ctrl+d"},
	}
	for _, tt := range tests {
		t.Run(tt.summary, func(t *testing.T) {
			got := classifyChatError(errors.New(tt.detail))
			if got.summary != tt.summary || !strings.Contains(got.hint, tt.hint) || got.detail != tt.detail {
				t.Errorf("classify = %#v", got)
			}
		})
	}
}
