package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/ollama"
	"github.com/dicedatalore/oolong/internal/oneshot"
	"github.com/dicedatalore/oolong/internal/openai"
	"github.com/dicedatalore/oolong/internal/ui"
	"github.com/dicedatalore/oolong/internal/version"
)

func main() {
	resetKey := flag.Bool("reset-key", false, "delete stored API keys from the OS keychain and exit")
	showVersion := flag.Bool("version", false, "print the version and exit")
	model := flag.String("model", "", "open a chat with this model id, skipping the picker")
	resume := flag.String("resume", "", "resume a conversation from a transcript saved with ctrl+s")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), `Usage:
  oolong                   open the chat TUI
  oolong "prompt"          one-shot: stream the answer to stdout, no TUI
  ... | oolong ["prompt"]  send piped input as context (one-shot)
  oolong config init       write a commented starter config.toml

Flags:
`)
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("oolong " + version.String())
		return
	}
	if *resetKey {
		if err := keystore.DeleteAll(); err != nil {
			fmt.Fprintf(os.Stderr, "reset-key: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Stored API keys deleted.")
		return
	}
	args := flag.Args()
	if len(args) > 0 && args[0] == "config" {
		// "config" is reserved so a typoed subcommand doesn't get sent to a
		// model as a one-shot prompt.
		if len(args) == 2 && args[1] == "init" {
			path, err := config.Init()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("Wrote " + path)
			return
		}
		fmt.Fprintf(os.Stderr, "unknown command %q (did you mean \"config init\"?)\n", strings.Join(args, " "))
		os.Exit(2)
	}

	// A bad config file must never block launch: Load always returns a
	// usable config, and the error surfaces as a notice inside the UI.
	cfg, cfgErr := config.Load()
	if os.Getenv("OPENAI_BASE_URL") != "" {
		// The env var wins over every configured endpoint — and keeps the
		// standard OpenAI behaviors (key validation, availability check),
		// so a driver pointing at a fake server still exercises them. The
		// SDK picks the env var up on its own.
		cfg.BaseURL = ""
		cfg.Provider = ""
		for i := range cfg.Models {
			cfg.Models[i].BaseURL = ""
			cfg.Models[i].Provider = ""
		}
	}
	if *model != "" {
		// The flag wins over the config's default_model and is passed
		// through unvalidated: any model the API key can access works,
		// and a typo surfaces as a clear API error on the first send.
		cfg.DefaultModel = *model
	}
	var cfgNotice string
	if cfgErr != nil {
		cfgNotice = cfgErr.Error()
	}

	// Positional arguments (or piped stdin) mean one-shot mode: stream the
	// answer to stdout and exit, no TUI.
	if stdin, piped := oneshot.PipedStdin(); len(args) > 0 || piped {
		if *resume != "" {
			fmt.Fprintln(os.Stderr, "--resume opens the TUI and cannot be combined with a one-shot prompt")
			os.Exit(2)
		}
		if cfgNotice != "" {
			fmt.Fprintln(os.Stderr, cfgNotice)
		}
		os.Exit(oneshot.Run(cfg, strings.Join(args, " "), stdin, os.Stdout))
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
	if key := keystore.Resolve(keystore.OpenAI); key != "" || cfg.HasCustomEndpoint() {
		if cfg.Provider == "ollama" {
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
			fmt.Fprintf(os.Stderr, "resume: %v\n", err)
			os.Exit(1)
		}
		if *model != "" {
			// An explicit --model outranks the model named in the file.
			t.Model = *model
		}
		uiModel = uiModel.Resume(t)
	}
	p := tea.NewProgram(uiModel)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}
