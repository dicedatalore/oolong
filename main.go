package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"

	provideranthropic "github.com/dicedatalore/oolong/internal/anthropic"
	"github.com/dicedatalore/oolong/internal/config"
	providergoogle "github.com/dicedatalore/oolong/internal/google"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/ollama"
	"github.com/dicedatalore/oolong/internal/oneshot"
	"github.com/dicedatalore/oolong/internal/openai"
	"github.com/dicedatalore/oolong/internal/ui"
	"github.com/dicedatalore/oolong/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run owns command-line routing and returns the process exit code. Keeping
// os.Exit in main makes the routing testable without starting subprocesses.
func run(argv []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("oolong", flag.ContinueOnError)
	flags.SetOutput(stderr)
	resetKey := flags.Bool("reset-key", false, "delete stored API keys from the OS keychain and exit")
	showVersion := flags.Bool("version", false, "print the version and exit")
	model := flags.String("model", "", "open a chat with this model id, skipping the picker")
	resume := flags.String("resume", "", "resume a conversation from a transcript saved with ctrl+s")
	flags.Usage = func() {
		fmt.Fprint(stderr, `Usage:
  oolong                   open the chat TUI
  oolong "prompt"          one-shot: stream the answer to stdout, no TUI
  ... | oolong ["prompt"]  send piped input as context (one-shot)
  oolong config init       write a commented starter config.toml

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
		fmt.Fprintln(stdout, "oolong "+version.String())
		return 0
	}
	if *resetKey {
		if err := keystore.DeleteAll(); err != nil {
			fmt.Fprintf(stderr, "reset-key: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Stored API keys deleted.")
		return 0
	}
	args := flags.Args()
	if len(args) > 0 && args[0] == "config" {
		// "config" is reserved so a typoed subcommand doesn't get sent to a
		// model as a one-shot prompt.
		if len(args) == 2 && args[1] == "init" {
			path, err := config.Init()
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			fmt.Fprintln(stdout, "Wrote "+path)
			return 0
		}
		fmt.Fprintf(stderr, "unknown command %q (did you mean \"config init\"?)\n", strings.Join(args, " "))
		return 2
	}

	// A bad config file must never block launch: Load always returns a
	// usable config, and the error surfaces as a notice inside the UI.
	cfg, cfgErr := config.Load()
	applyConfigOverrides(&cfg, os.Getenv("OPENAI_BASE_URL"), *model)
	var cfgNotice string
	if cfgErr != nil {
		cfgNotice = cfgErr.Error()
	}

	// Positional arguments (or piped stdin) mean one-shot mode: stream the
	// answer to stdout and exit, no TUI.
	if stdin, piped := oneshot.PipedStdin(); len(args) > 0 || piped {
		if *resume != "" {
			fmt.Fprintln(stderr, "--resume opens the TUI and cannot be combined with a one-shot prompt")
			return 2
		}
		if cfgNotice != "" {
			fmt.Fprintln(stderr, cfgNotice)
		}
		return oneshot.Run(cfg, strings.Join(args, " "), stdin, stdout)
	}

	// Query the terminal background before Bubble Tea owns the tty; doing it
	// mid-session leaks the terminal's OSC reply into the UI as garbage text.
	mdStyle := styles.LightStyle
	if termenv.HasDarkBackground() {
		mdStyle = styles.DarkStyle
	}

	// A custom endpoint launches even without a key: local servers
	// (Ollama, LM Studio) don't use one.
	var client openai.ChatClient
	provider := cfg.Provider
	if provider == "" {
		provider = "openai"
	}
	keyProvider := keystore.OpenAI
	if provider == "anthropic" {
		keyProvider = keystore.Anthropic
	} else if provider == "google" {
		keyProvider = keystore.Google
	}
	if key := keystore.Resolve(keyProvider); key != "" || cfg.HasCustomEndpoint() {
		if provider == "anthropic" {
			if cfg.BaseURL != "" {
				client = provideranthropic.New(key, provideranthropic.WithBaseURL(cfg.BaseURL))
			} else {
				client = provideranthropic.New(key)
			}
		} else if provider == "google" {
			if cfg.BaseURL != "" {
				client = providergoogle.New(key, providergoogle.WithBaseURL(cfg.BaseURL))
			} else {
				client = providergoogle.New(key)
			}
		} else if provider == "ollama" {
			client = ollama.New(cfg.BaseURL)
		} else if cfg.BaseURL != "" {
			client = openai.New(key, openai.WithBaseURL(cfg.BaseURL))
		} else {
			client = openai.New(key)
		}
	}
	uiModel := ui.New(client, mdStyle, cfg, cfgNotice)
	if *resume != "" {
		t, err := ui.LoadTranscript(*resume)
		if err != nil {
			fmt.Fprintf(stderr, "resume: %v\n", err)
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
		fmt.Fprintf(stderr, "error running program: %v\n", err)
		return 1
	}
	return 0
}

// applyConfigOverrides applies process-level settings after loading the
// config file. Command-line values win over config, and OPENAI_BASE_URL wins
// over configured OpenAI endpoints while leaving other providers untouched.
func applyConfigOverrides(cfg *config.Config, openAIBaseURL, model string) {
	if openAIBaseURL != "" {
		// The env var wins over configured OpenAI endpoints — and keeps the
		// standard OpenAI behaviors (key validation, availability check),
		// so a driver pointing at a fake server still exercises them. The
		// SDK picks the env var up on its own.
		if cfg.Provider == "" || cfg.Provider == "openai" {
			cfg.BaseURL = ""
			cfg.Provider = ""
		}
		for i := range cfg.Models {
			if cfg.Models[i].Provider == "" || cfg.Models[i].Provider == "openai" {
				cfg.Models[i].BaseURL = ""
				cfg.Models[i].Provider = "openai"
			}
		}
	}
	if model != "" {
		// The flag wins over the config's default_model and is passed
		// through unvalidated: any model the API key can access works,
		// and a typo surfaces as a clear API error on the first send.
		cfg.DefaultModel = model
	}
}
