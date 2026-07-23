// Package doctor reports Oolong's local configuration and capability state.
package doctor

import (
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/provider"
	"github.com/dicedatalore/oolong/internal/version"
)

// Run reports local state without making provider requests or reading secret
// values.
func Run(out io.Writer, cfg config.Config, cfgErr error, resolver *provider.Resolver,
	keyStatus func(keystore.Provider) string, clipboardSupported func() bool,
) int {
	fmt.Fprintln(out, "Oolong "+version.String())
	path := config.Path()
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintln(out, "Config: "+path)
	} else if os.IsNotExist(err) {
		fmt.Fprintln(out, "Config: not found (using defaults; create with `oolong config init`)")
	} else {
		fmt.Fprintf(out, "Config: %s (%v)\n", path, err)
	}
	if cfgErr != nil {
		fmt.Fprintln(out, "Config warning: "+cfgErr.Error())
	} else {
		fmt.Fprintln(out, "Config status: OK")
	}

	fmt.Fprintln(out, "Credentials:")
	for _, p := range []keystore.Provider{keystore.OpenAI, keystore.Anthropic, keystore.Google} {
		status := "not set"
		if keyStatus != nil {
			status = keyStatus(p)
		}
		fmt.Fprintf(out, "  %-9s %s\n", providerLabel(provider.Name(p)), status)
	}

	fmt.Fprintln(out, "Configured providers:")
	var names []provider.Name
	for _, model := range cfg.Catalog() {
		route := resolver.RouteFor(model.ID)
		if route.Provider != "" && !slices.Contains(names, route.Provider) {
			names = append(names, route.Provider)
		}
	}
	if len(names) == 0 {
		fmt.Fprintln(out, "  none")
	}
	for _, name := range names {
		route := resolver.RouteForProvider(name)
		status := "needs credentials"
		if resolver.Available(route) {
			status = "available locally"
		}
		if route.BaseURL != "" {
			status += " (" + route.BaseURL + ")"
		}
		fmt.Fprintf(out, "  %-9s %s\n", providerLabel(name), status)
	}

	clipboard := "text only (image support unavailable in this build)"
	if clipboardSupported != nil && clipboardSupported() {
		clipboard = "text and images"
	}
	fmt.Fprintln(out, "Clipboard: "+clipboard)
	fmt.Fprintln(out, "No provider network requests were made.")
	if cfgErr != nil {
		return 1
	}
	return 0
}

func providerLabel(name provider.Name) string {
	switch name {
	case provider.OpenAI:
		return "OpenAI"
	case provider.Anthropic:
		return "Anthropic"
	case provider.Google:
		return "Google"
	case provider.Ollama:
		return "Ollama"
	default:
		return string(name)
	}
}
