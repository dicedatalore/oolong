// Package config loads Oolong's optional TOML config file from
// ~/.config/oolong/config.toml (or $XDG_CONFIG_HOME/oolong/config.toml).
// Every key is optional: a missing file or empty config leaves Oolong
// behaving exactly as it does out of the box.
package config

import (
	"errors"
	"fmt"
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
}

type Config struct {
	DefaultModel  string  `toml:"default_model"`  // skip the picker on launch when set
	TranscriptDir string  `toml:"transcript_dir"` // OOLONG_TRANSCRIPT_DIR env var still wins
	Accent        string  `toml:"accent"`         // primary accent color, "#RRGGBB"
	Models        []Model `toml:"models"`         // replaces the built-in catalog when present
}

// Builtin is the model catalog compiled into Oolong, used when the config
// file does not provide its own [[models]] catalog.
// Rates per https://openai.com/api/pricing.
var Builtin = []Model{
	{ID: "gpt-5.6-luna", Description: "For cost-sensitive workloads", InputRate: 1.00, OutputRate: 6.00},
	{ID: "gpt-5.6-terra", Description: "Balances intelligence and cost", InputRate: 2.50, OutputRate: 15.00},
	{ID: "gpt-5.6-sol", Description: "For complex professional work", InputRate: 5.00, OutputRate: 30.00},
}

// Catalog returns the models the picker offers: the config's catalog when
// present, otherwise the built-in list.
func (c Config) Catalog() []Model {
	if len(c.Models) > 0 {
		return c.Models
	}
	return Builtin
}

// CustomCatalog reports whether the catalog came from the config file; such
// models are checked against the API before the picker displays them.
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

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

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
	models := c.Models[:0]
	for _, m := range c.Models {
		switch {
		case m.ID == "":
			drop("[[models]] entry without an id")
			continue
		case m.InputRate < 0 || m.OutputRate < 0:
			drop("model %s has a negative rate", m.ID)
			continue
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
