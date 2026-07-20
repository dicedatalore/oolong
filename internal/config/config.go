// Package config loads Oolong's optional TOML config file from
// ~/.config/oolong/config.toml (or $XDG_CONFIG_HOME/oolong/config.toml).
// Every key is optional: a missing file or empty config leaves Oolong
// behaving exactly as it does out of the box.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Model is one entry of the model catalog. Rates are USD per 1M tokens.
// ReasoningEffort and Verbosity are passed to the API as-is and validated
// there — the supported values vary by model generation (gpt-5.6 takes
// none|low|medium|high|xhigh), and pinning a list here would break the
// config's job of outliving Oolong releases.
type Model struct {
	ID              string  `toml:"id"`
	Description     string  `toml:"description"`
	InputRate       float64 `toml:"input_rate"`
	OutputRate      float64 `toml:"output_rate"`
	ReasoningEffort string  `toml:"reasoning_effort"`
	Verbosity       string  `toml:"verbosity"`
	BaseURL         string  `toml:"base_url"`       // optional per-model provider endpoint
	Provider        string  `toml:"provider"`       // "openai", "anthropic", "google", or "ollama"
	ContextWindow   int     `toml:"context_window"` // tokens; enables the ctx meter in the chat header
}

type Config struct {
	DefaultModel    string  `toml:"default_model"`    // skip the picker on launch when set
	TranscriptDir   string  `toml:"transcript_dir"`   // OOLONG_TRANSCRIPT_DIR env var still wins
	Accent          string  `toml:"accent"`           // primary accent color, "#RRGGBB"
	SecondaryAccent string  `toml:"secondary_accent"` // secondary accent color, "#RRGGBB"
	SimplePicker    bool    `toml:"simple_picker"`    // start the model picker in its simple view
	BaseURL         string  `toml:"base_url"`         // optional provider endpoint for every model
	Provider        string  `toml:"provider"`         // blank preserves the OpenAI default
	Models          []Model `toml:"models"`           // replaces the built-in catalog when present
}

// OfficialBaseURL is the endpoint the OpenAI SDK talks to by default.
const OfficialBaseURL = "https://api.openai.com/v1"

// CustomEndpoint reports whether url points somewhere other than the
// official OpenAI API. Custom OpenAI-compatible endpoints may use their own
// authentication and are allowed to start without an OpenAI key.
func CustomEndpoint(url string) bool {
	return url != "" && strings.TrimSuffix(url, "/") != OfficialBaseURL
}

// Catalog returns the models the picker offers: the config's catalog when
// present, otherwise the built-in list.
func (c Config) Catalog() []Model {
	if len(c.Models) > 0 {
		return c.Models
	}
	return DefaultModels
}

// CustomCatalog reports whether the catalog came from the config file.
func (c Config) CustomCatalog() bool { return len(c.Models) > 0 }

// Path returns the config file location: $XDG_CONFIG_HOME/oolong/config.toml,
// defaulting to ~/.config/oolong/config.toml.
func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "oolong", "config.toml")
}

// Load reads and validates the config file. A missing file is not an error.
// The returned Config is always usable: on a parse error it is the zero
// (all-defaults) config, and invalid values are dropped individually so one
// bad key never blocks launch — the error says what was ignored.
func Load() (Config, error) {
	path := Path()
	if path == "" {
		return Config{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("config: %v", err)
	}
	return parse(string(data))
}

// scaffold is the fully-commented starter config written by `oolong config
// init`. Every key is commented out, so the file parses as the zero config
// until the user uncomments something.
const scaffold = `# Oolong configuration — every key is optional; delete what you don't use.
# Docs: https://github.com/dicedatalore/oolong#configuration

# Open this model on launch instead of the picker.
# default_model = "gpt-5.6-terra"

# Where ctrl+s saves transcripts (the OOLONG_TRANSCRIPT_DIR env var wins).
# transcript_dir = "~/notes/chats"

# Primary accent color.
# accent = "#FFAF87"

# Secondary accent color, used for assistant messages and the logo gradient.
# secondary_accent = "#7D56F4"

# Start the model picker in its simple view: one line per model, no
# descriptions or rates. Tab toggles the views either way.
# simple_picker = true

# Any OpenAI-compatible endpoint (Ollama, LM Studio, OpenRouter, …). Applies
# to every model unless one sets its own base_url below. The OPENAI_BASE_URL
# env var overrides both. No API key is required for local endpoints.
# base_url = "http://localhost:11434/v1"
# provider = "openai"             # protocol for the global endpoint

# Replaces the built-in model catalog when present. Any model your API key
# can access works; provider errors are shown when a model is used.
# [[models]]
# id = "gpt-5.6-terra"
# provider = "openai"
# description = "Balances intelligence and cost"
# input_rate = 2.50            # USD per 1M input tokens
# output_rate = 15.00          # USD per 1M output tokens
# reasoning_effort = "medium"  # none | low | medium | high | xhigh (model-dependent)
# verbosity = "low"            # low | medium | high
# context_window = 400000      # tokens; shows a ctx meter in the chat header
# base_url = ""                # per-model endpoint, overrides the global one
#
# [[models]]
# id = "gemma3"
# provider = "ollama"
# description = "Local Gemma through Ollama"
# base_url = "http://localhost:11434"
#
# [[models]]
# id = "claude-sonnet-5"
# provider = "anthropic"
# description = "Anthropic Claude Sonnet"
#
# [[models]]
# id = "gemini-3.5-flash"
# provider = "google"
# description = "Google Gemini Flash"
`

// Init writes the scaffold config file and returns its path. An existing
// config is never overwritten.
func Init() (string, error) {
	path := Path()
	if path == "" {
		return "", fmt.Errorf("config: cannot determine the config directory")
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("config already exists: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(scaffold), 0o644)
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validProvider(p string) bool {
	return p == "openai" || p == "anthropic" || p == "google" || p == "ollama"
}

// validEndpoint reports whether s parses as an absolute http(s) URL.
func validEndpoint(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// parse decodes and validates one config document. Split from Load so tests
// can exercise it without touching the filesystem.
func parse(data string) (Config, error) {
	var c Config
	if err := toml.Unmarshal([]byte(data), &c); err != nil {
		msg := err.Error()
		var perr toml.ParseError
		if errors.As(err, &perr) {
			msg = perr.ErrorWithPosition()
		}
		return Config{}, fmt.Errorf("config: %s", msg)
	}

	var problems []string
	drop := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if c.Accent != "" && !hexColor.MatchString(c.Accent) {
		drop("accent %q is not a #RRGGBB color", c.Accent)
		c.Accent = ""
	}
	if c.SecondaryAccent != "" && !hexColor.MatchString(c.SecondaryAccent) {
		drop("secondary_accent %q is not a #RRGGBB color", c.SecondaryAccent)
		c.SecondaryAccent = ""
	}
	if c.BaseURL != "" && !validEndpoint(c.BaseURL) {
		drop("base_url %q is not an http(s) URL", c.BaseURL)
		c.BaseURL = ""
	}
	if c.Provider != "" && !validProvider(c.Provider) {
		drop("provider %q is not supported", c.Provider)
		c.Provider = ""
	}
	models := c.Models[:0]
	for _, m := range c.Models {
		switch {
		case m.ID == "":
			drop("[[models]] entry without an id")
			continue
		case m.InputRate < 0 || m.OutputRate < 0:
			drop("model %s has a negative rate", m.ID)
			continue
		case m.ContextWindow < 0:
			drop("model %s has a negative context_window", m.ID)
			continue
		}
		if m.BaseURL != "" && !validEndpoint(m.BaseURL) {
			drop("model %s base_url %q is not an http(s) URL", m.ID, m.BaseURL)
			m.BaseURL = ""
		}
		if m.Provider != "" && !validProvider(m.Provider) {
			drop("model %s provider %q is not supported", m.ID, m.Provider)
			m.Provider = ""
		}
		models = append(models, m)
	}
	c.Models = models

	if c.DefaultModel != "" && !c.hasModel(c.DefaultModel) {
		drop("default_model %q is not in the model catalog", c.DefaultModel)
		c.DefaultModel = ""
	}

	if len(problems) > 0 {
		return c, fmt.Errorf("config: ignored %s", strings.Join(problems, "; "))
	}
	return c, nil
}

func (c Config) hasModel(id string) bool {
	for _, m := range c.Catalog() {
		if m.ID == id {
			return true
		}
	}
	return false
}

// Efforts lists the reasoning effort levels the picker's ←/→ session
// override steps through, lowest to highest — the set the current gpt-5.6
// generation accepts. "" means the parameter is omitted from requests.
// A model that rejects a level reports it clearly on the next send.
var Efforts = []string{"none", "low", "medium", "high", "xhigh"}
