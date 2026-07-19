// Package oneshot implements one-shot mode: `oolong "question"` (and
// `cat main.go | oolong "explain"`) streams the answer straight to stdout
// with no TUI, which makes Oolong scriptable. It reuses the same client,
// catalog, and endpoint rules as the chat screen.
package oneshot

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/ollama"
	"github.com/dicedatalore/oolong/internal/openai"
)

// Run sends one user message and streams the reply to out, returning the
// process exit code. prompt comes from the command-line arguments; stdin is
// piped input ("" when the terminal is interactive) sent as context above
// the prompt.
func Run(cfg config.Config, prompt, stdin string, out io.Writer) int {
	content := combinePrompt(prompt, stdin)
	if content == "" {
		fmt.Fprintln(os.Stderr, "nothing to ask: pass a prompt or pipe input")
		return 2
	}

	model := cfg.DefaultModel
	if model == "" {
		model = cfg.Catalog()[0].ID
	}
	var cm config.Model
	for _, entry := range cfg.Catalog() {
		if entry.ID == model {
			cm = entry
			break
		}
	}
	endpoint := cm.BaseURL
	provider := cm.Provider
	if endpoint == "" {
		endpoint = cfg.BaseURL
	}
	if provider == "" {
		provider = cfg.Provider
	}

	key := keystore.Resolve()
	if key == "" && os.Getenv("OPENAI_BASE_URL") == "" && !config.CustomEndpoint(endpoint) {
		fmt.Fprintln(os.Stderr, "no API key: run oolong once to store one, or set OPENAI_API_KEY")
		return 1
	}
	var client openai.ChatClient
	if provider == "ollama" {
		client = ollama.New(endpoint)
	} else if endpoint != "" {
		client = openai.New(key, openai.WithBaseURL(endpoint))
	} else {
		client = openai.New(key)
	}

	// Ctrl+C cancels the request context; StreamChat notices and closes the
	// channel, so the loop below ends without a terminal event.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := openai.Options{ReasoningEffort: cm.ReasoningEffort, Verbosity: cm.Verbosity}
	ch := make(chan openai.StreamEvent)
	go client.StreamChat(ctx, model, []openai.Message{{Role: "user", Content: content}}, opts, ch)

	trailingNewline := true
	for ev := range ch {
		switch {
		case ev.Err != nil:
			fmt.Fprintln(os.Stderr, ev.Err)
			return 1
		case ev.Delta != "":
			io.WriteString(out, ev.Delta)
			trailingNewline = strings.HasSuffix(ev.Delta, "\n")
		}
	}
	if ctx.Err() != nil {
		return 130 // interrupted
	}
	if !trailingNewline {
		io.WriteString(out, "\n")
	}
	return 0
}

// PipedStdin reads piped standard input in full; piped is false when stdin
// is an interactive terminal (or unreadable), which selects the TUI.
func PipedStdin() (stdin string, piped bool) {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return "", false
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// combinePrompt merges piped input and the argument prompt into one user
// message: the piped content is context, the prompt below it is the
// question. Either part may be empty.
func combinePrompt(prompt, stdin string) string {
	stdin = strings.TrimRight(stdin, "\n")
	switch {
	case stdin == "":
		return prompt
	case prompt == "":
		return stdin
	}
	return stdin + "\n\n" + prompt
}
