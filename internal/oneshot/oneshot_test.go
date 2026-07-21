package oneshot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/provider"
)

func TestCombinePrompt(t *testing.T) {
	tests := []struct {
		prompt, stdin, want string
	}{
		{"explain", "", "explain"},
		{"", "package main\n", "package main"},
		{"explain", "package main\n", "package main\n\nexplain"},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := combinePrompt(tt.prompt, tt.stdin); got != tt.want {
			t.Errorf("combinePrompt(%q, %q) = %q, want %q", tt.prompt, tt.stdin, got, tt.want)
		}
	}
}

func TestOneShotStreamsToWriter(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, s := range []string{"Hello", " world"} {
			fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\n", s)
		}
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "sk-test")

	cfg := config.Config{
		DefaultModel: "local-llama",
		Models:       []config.Model{{ID: "local-llama", Provider: "openai", BaseURL: srv.URL, ReasoningEffort: "low"}},
	}
	var out strings.Builder
	if code := Run(context.Background(), provider.NewResolver(cfg), "explain", "package main\n", &out, &out); code != 0 {
		t.Fatalf("Run() exit code = %d, want 0", code)
	}
	// The reply lands on the writer with a trailing newline added.
	if got := out.String(); got != "Hello world\n" {
		t.Errorf("output = %q, want %q", got, "Hello world\n")
	}
	// The request carries the combined message and the catalog's options.
	for _, want := range []string{
		`package main\n\nexplain`,
		`"model":"local-llama"`,
		`"effort":"low"`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("request body missing %s:\n%s", want, body)
		}
	}
}

func TestOneShotAnthropicProvider(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello Claude\"}}\n\n")
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n")
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("OPENAI_API_KEY", "")

	cfg := config.Config{
		DefaultModel: "claude-test",
		Models:       []config.Model{{ID: "claude-test", Provider: "anthropic", BaseURL: srv.URL}},
	}
	var out strings.Builder
	if code := Run(context.Background(), provider.NewResolver(cfg), "hello", "", &out, &out); code != 0 {
		t.Fatalf("Run() exit code = %d, want 0", code)
	}
	if got := out.String(); got != "Hello Claude\n" {
		t.Errorf("output = %q", got)
	}
	if !strings.Contains(string(body), `"model":"claude-test"`) {
		t.Errorf("request missing Anthropic model: %s", body)
	}
}

func TestChooseModelUsesAvailableProviderKey(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	var want string
	for _, m := range config.DefaultModels {
		if m.Provider == "anthropic" {
			want = m.ID
			break
		}
	}
	if got := provider.NewResolver(config.Config{}).FirstAvailableModel(); got != want {
		t.Errorf("chooseModel() = %q, want first Anthropic default %q", got, want)
	}
}

func TestChooseModelPrefersGoogleWhenOnlyItsKeyIsSet(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "AIza-test")
	var want string
	for _, m := range config.DefaultModels {
		if m.Provider == "google" {
			want = m.ID
			break
		}
	}
	if got := provider.NewResolver(config.Config{}).FirstAvailableModel(); got != want {
		t.Errorf("chooseModel() = %q, want first Google default %q", got, want)
	}
}

func TestOneShotNothingToAsk(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	var out strings.Builder
	if code := Run(context.Background(), provider.NewResolver(config.Config{}), "", "", &out, &out); code != 2 {
		t.Errorf("exit code = %d, want 2 for an empty prompt", code)
	}
}
