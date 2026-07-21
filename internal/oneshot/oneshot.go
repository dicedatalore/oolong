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
	"strings"

	"github.com/dicedatalore/oolong/internal/chat"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/provider"
)

// Run sends one user message and streams the reply to out, returning the
// process exit code. prompt comes from the command-line arguments; stdin is
// piped input ("" when the terminal is interactive) sent as context above
// the prompt.
func Run(ctx context.Context, resolver *provider.Resolver, prompt, stdin string, out, errOut io.Writer) int {
	content := combinePrompt(prompt, stdin)
	if content == "" {
		fmt.Fprintln(errOut, "nothing to ask: pass a prompt or pipe input")
		return 2
	}

	model := resolver.FirstAvailableModel()
	route := resolver.RouteFor(model)
	client := resolver.ClientFor(model)
	if client == nil {
		keyProvider, keyed := provider.KeyProvider(route.Provider)
		if route.Provider == "" {
			fmt.Fprintf(errOut, "model %q has no provider; set provider in config or pass --provider\n", model)
		} else if !keyed {
			fmt.Fprintf(errOut, "provider %q is unavailable\n", route.Provider)
		} else {
			fmt.Fprintf(errOut, "no %s API key: press ctrl+k in the picker or set %s\n", route.Provider, keystore.EnvName(keyProvider))
		}
		return 1
	}

	opts := chat.Options{ReasoningEffort: route.Model.ReasoningEffort, Verbosity: route.Model.Verbosity}
	ch := make(chan chat.StreamEvent)
	go client.StreamChat(ctx, model, []chat.Message{{Role: "user", Content: content}}, opts, ch)

	trailingNewline := true
	for ev := range ch {
		switch {
		case ev.Err != nil:
			fmt.Fprintln(errOut, ev.Err)
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
func PipedStdin(file *os.File) (stdin string, piped bool) {
	st, err := file.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return "", false
	}
	data, err := io.ReadAll(file)
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
