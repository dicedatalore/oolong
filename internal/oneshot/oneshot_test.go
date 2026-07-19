package oneshot

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dicedatalore/oolong/internal/config"
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
		Models:       []config.Model{{ID: "local-llama", BaseURL: srv.URL, ReasoningEffort: "low"}},
	}
	var out strings.Builder
	if code := Run(cfg, "explain", "package main\n", &out); code != 0 {
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

func TestOneShotNothingToAsk(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	var out strings.Builder
	if code := Run(config.Config{}, "", "", &out); code != 2 {
		t.Errorf("exit code = %d, want 2 for an empty prompt", code)
	}
}
