package ui

import "strings"

type chatErrorInfo struct {
	summary string
	hint    string
	detail  string
}

// classifyChatError turns provider-specific prose into a stable recovery path
// while retaining the original message for the details view.
func classifyChatError(err error) *chatErrorInfo {
	detail := err.Error()
	lower := strings.ToLower(detail)
	info := &chatErrorInfo{summary: "Request failed", hint: "ctrl+r retry • ctrl+t change model", detail: detail}
	switch {
	case containsAny(lower, "unauthorized", "authentication", "invalid api key", "incorrect api key", "http 401"):
		info.summary = "Authentication failed"
		info.hint = "ctrl+k update the provider key"
	case containsAny(lower, "rate limit", "too many requests", "http 429"):
		info.summary = "Rate limited"
		info.hint = "wait, then ctrl+r retry"
	case containsAny(lower, "quota", "billing", "credit balance", "insufficient_quota"):
		info.summary = "Provider quota unavailable"
		info.hint = "check provider billing • ctrl+t change model"
	case containsAny(lower, "context length", "context window", "maximum context", "too many tokens", "prompt is too long"):
		info.summary = "Context limit reached"
		info.hint = "ctrl+u shorten the prompt • ctrl+t change model"
	case containsAny(lower, "deadline exceeded", "timed out", "timeout"):
		info.summary = "Request timed out"
		info.hint = "ctrl+r retry • ctrl+t change model"
	case containsAny(lower, "connection refused", "no such host", "network is unreachable", "connection reset", "unexpected eof", "eof"):
		info.summary = "Couldn't reach the provider"
		info.hint = "check the endpoint • ctrl+r retry"
	case containsAny(lower, "unsupported", "not supported", "invalid parameter", "unknown parameter"):
		info.summary = "This model doesn't support part of the request"
		info.hint = "ctrl+u edit • ctrl+d remove an attachment"
	case containsAny(lower, "http 500", "http 502", "http 503", "http 504", "overloaded", "internal server error", "service unavailable"):
		info.summary = "Provider temporarily unavailable"
		info.hint = "ctrl+r retry • ctrl+t change model"
	}
	return info
}

func containsAny(s string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(s, value) {
			return true
		}
	}
	return false
}
