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
	"github.com/dicedatalore/oolong/internal/openai"
	"github.com/dicedatalore/oolong/internal/ui"
	"github.com/dicedatalore/oolong/internal/version"
)

func main() {
	resetKey := flag.Bool("reset-key", false, "delete the stored OpenAI API key from the OS keychain and exit")
	showVersion := flag.Bool("version", false, "print the version and exit")
	model := flag.String("model", "", "open a chat with this model id, skipping the picker")
	flag.Parse()
	if *showVersion {
		fmt.Println("oolong " + version.String())
		return
	}
	if *resetKey {
		if err := keystore.Delete(); err != nil {
			fmt.Fprintf(os.Stderr, "reset-key: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Stored API key deleted.")
		return
	}
	if args := flag.Args(); len(args) > 0 {
		if len(args) == 2 && args[0] == "config" && args[1] == "init" {
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

	// Query the terminal background before Bubble Tea owns the tty; doing it
	// mid-session leaks the terminal's OSC reply into the UI as garbage text.
	mdStyle := styles.LightStyle
	if termenv.HasDarkBackground() {
		mdStyle = styles.DarkStyle
	}

	var client *openai.Client
	if key := keystore.Resolve(); key != "" {
		client = openai.New(key)
	}
	p := tea.NewProgram(ui.New(client, mdStyle, cfg, cfgNotice))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}
