package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"

	"github.com/dicedatalore/oolong/internal/clipboard"
	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/oneshot"
	"github.com/dicedatalore/oolong/internal/provider"
	"github.com/dicedatalore/oolong/internal/ui"
	"github.com/dicedatalore/oolong/internal/version"
)

func main() {
	os.Exit(runWith(os.Args[1:], dependencies{
		stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr,
		getenv: os.Getenv, loadConfig: config.Load, initConfig: config.Init,
		deleteKeys: keystore.DeleteAll, resolveKey: keystore.Resolve,
		keyStatus: keystore.Status, clipboardSupported: clipboard.Supported,
	}))
}

type dependencies struct {
	stdin              *os.File
	stdout, stderr     io.Writer
	getenv             func(string) string
	loadConfig         func() (config.Config, error)
	initConfig         func() (string, error)
	deleteKeys         func() error
	resolveKey         func(keystore.Provider) string
	keyStatus          func(keystore.Provider) string
	clipboardSupported func() bool
}

// run is the convenient test entry point; runWith contains the fully injected
// application boundary used by main.
func run(argv []string, stdout, stderr io.Writer) int {
	return runWith(argv, dependencies{
		stdin: os.Stdin, stdout: stdout, stderr: stderr,
		getenv: os.Getenv, loadConfig: config.Load, initConfig: config.Init,
		deleteKeys: keystore.DeleteAll, resolveKey: keystore.Resolve,
		keyStatus: keystore.Status, clipboardSupported: clipboard.Supported,
	})
}

func runWith(argv []string, deps dependencies) int {
	flags := flag.NewFlagSet("oolong", flag.ContinueOnError)
	flags.SetOutput(deps.stderr)
	resetKey := flags.Bool("reset-key", false, "delete stored API keys from the OS keychain and exit")
	showVersion := flags.Bool("version", false, "print the version and exit")
	model := flags.String("model", "", "open a chat with this model id, skipping the picker")
	providerName := flags.String("provider", "", "provider for a --model not listed in the catalog")
	resume := flags.String("resume", "", "resume a conversation from a transcript saved with ctrl+s")
	flags.Usage = func() {
		fmt.Fprint(deps.stderr, `Usage:
  oolong                   open the chat TUI
  oolong "prompt"          one-shot: stream the answer to stdout, no TUI
  ... | oolong ["prompt"]  send piped input as context (one-shot)
  oolong config init       write a commented starter config.toml
  oolong doctor            inspect local setup without contacting providers

Flags:
`)
		flags.PrintDefaults()
	}
	if err := flags.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(deps.stdout, "oolong "+version.String())
		return 0
	}
	if *resetKey {
		if err := deps.deleteKeys(); err != nil {
			fmt.Fprintf(deps.stderr, "reset-key: %v\n", err)
			return 1
		}
		fmt.Fprintln(deps.stdout, "Stored API keys deleted.")
		return 0
	}
	args := flags.Args()
	if len(args) > 0 && args[0] == "config" {
		// "config" is reserved so a typoed subcommand doesn't get sent to a
		// model as a one-shot prompt.
		if len(args) == 2 && args[1] == "init" {
			path, err := deps.initConfig()
			if err != nil {
				fmt.Fprintln(deps.stderr, err)
				return 1
			}
			fmt.Fprintln(deps.stdout, "Wrote "+path)
			return 0
		}
		fmt.Fprintf(deps.stderr, "unknown command %q (did you mean \"config init\"?)\n", strings.Join(args, " "))
		return 2
	}

	// A bad config file must never block launch: Load always returns a
	// usable config, and the error surfaces as a notice inside the UI.
	cfg, cfgErr := deps.loadConfig()
	if *providerName != "" {
		if *model == "" {
			fmt.Fprintln(deps.stderr, "--provider requires --model")
			return 2
		}
		switch provider.Name(*providerName) {
		case provider.OpenAI, provider.Anthropic, provider.Google, provider.Ollama:
			cfg.Provider = *providerName
		default:
			fmt.Fprintf(deps.stderr, "unsupported provider %q\n", *providerName)
			return 2
		}
	}
	if *model != "" {
		cfg.DefaultModel = *model
	}
	resolver := provider.NewResolver(cfg)
	resolver.Getenv = deps.getenv
	resolver.ResolveKey = deps.resolveKey
	var cfgNotice string
	if cfgErr != nil {
		cfgNotice = cfgErr.Error()
	}
	if len(args) == 1 && args[0] == "doctor" {
		return runDoctor(deps.stdout, cfg, cfgErr, resolver, deps.keyStatus, deps.clipboardSupported)
	}

	// Positional arguments (or piped stdin) mean one-shot mode: stream the
	// answer to stdout and exit, no TUI.
	if stdin, piped := oneshot.PipedStdin(deps.stdin); len(args) > 0 || piped {
		if *resume != "" {
			fmt.Fprintln(deps.stderr, "--resume opens the TUI and cannot be combined with a one-shot prompt")
			return 2
		}
		if cfgNotice != "" {
			fmt.Fprintln(deps.stderr, cfgNotice)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return oneshot.Run(ctx, resolver, strings.Join(args, " "), stdin, deps.stdout, deps.stderr)
	}

	// Query the terminal background before Bubble Tea owns the tty; doing it
	// mid-session leaks the terminal's OSC reply into the UI as garbage text.
	mdStyle := styles.LightStyle
	if termenv.HasDarkBackground() {
		mdStyle = styles.DarkStyle
	}

	uiModel := ui.NewWithResolver(resolver, mdStyle, cfg, cfgNotice)
	if *resume != "" {
		t, err := ui.LoadTranscript(*resume)
		if err != nil {
			fmt.Fprintf(deps.stderr, "resume: %v\n", err)
			return 1
		}
		if *model != "" {
			// An explicit --model outranks the model named in the file.
			t.Model = *model
		}
		uiModel = uiModel.Resume(t)
	}
	p := tea.NewProgram(uiModel)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(deps.stderr, "error running program: %v\n", err)
		return 1
	}
	return 0
}
