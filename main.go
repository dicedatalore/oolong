package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"

	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/openai"
	"github.com/dicedatalore/oolong/internal/ui"
)

func main() {
	resetKey := flag.Bool("reset-key", false, "delete the stored OpenAI API key from the OS keychain and exit")
	flag.Parse()
	if *resetKey {
		if err := keystore.Delete(); err != nil {
			fmt.Fprintf(os.Stderr, "reset-key: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Stored API key deleted.")
		return
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
	p := tea.NewProgram(ui.New(client, mdStyle))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}
