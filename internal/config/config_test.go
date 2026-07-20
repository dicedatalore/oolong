package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string              // substring of the returned error, "" for none
		check   func(Config) string // returns a failure description, "" if ok
	}{
		{
			name: "empty config keeps defaults",
			data: "",
			check: func(c Config) string {
				if c.DefaultModel != "" || c.TranscriptDir != "" || c.Accent != "" {
					return "scalar fields not zero"
				}
				if c.CustomCatalog() {
					return "empty config reports a custom catalog"
				}
				if len(c.Catalog()) != len(DefaultModels) {
					return "catalog is not the built-in list"
				}
				return ""
			},
		},
		{
			name: "full config",
			data: `
default_model = "gpt-5.4"
transcript_dir = "~/notes/chats"
accent = "#FFAF87"
simple_picker = true

[[models]]
id = "gpt-5.4"
description = "Previous generation"
input_rate = 1.25
output_rate = 10.00
reasoning_effort = "medium"
verbosity = "low"
`,
			check: func(c Config) string {
				if c.DefaultModel != "gpt-5.4" || c.TranscriptDir != "~/notes/chats" || c.Accent != "#FFAF87" {
					return "scalar fields not parsed"
				}
				if !c.SimplePicker {
					return "simple_picker not parsed"
				}
				if !c.CustomCatalog() || len(c.Catalog()) != 1 {
					return "custom catalog not used"
				}
				m := c.Catalog()[0]
				if m.ID != "gpt-5.4" || m.InputRate != 1.25 || m.OutputRate != 10.00 {
					return "model fields not parsed"
				}
				if m.ReasoningEffort != "medium" || m.Verbosity != "low" {
					return "reasoning fields not parsed"
				}
				return ""
			},
		},
		{
			name:    "malformed toml returns defaults",
			data:    "default_model = [broken",
			wantErr: "config:",
			check: func(c Config) string {
				if len(c.Catalog()) != len(DefaultModels) {
					return "catalog is not the built-in list after parse error"
				}
				return ""
			},
		},
		{
			name:  "unknown key is not an error",
			data:  "future_option = true",
			check: func(c Config) string { return "" },
		},
		{
			name:    "bad accent dropped",
			data:    `accent = "peach"`,
			wantErr: `accent "peach"`,
			check: func(c Config) string {
				if c.Accent != "" {
					return "bad accent kept"
				}
				return ""
			},
		},
		{
			name: "model without id dropped, rest kept",
			data: `
[[models]]
description = "no id"

[[models]]
id = "gpt-5.4"
`,
			wantErr: "without an id",
			check: func(c Config) string {
				if len(c.Models) != 1 || c.Models[0].ID != "gpt-5.4" {
					return "surviving model list wrong"
				}
				return ""
			},
		},
		{
			name: "negative rate drops the model",
			data: `
[[models]]
id = "gpt-5.4"
input_rate = -1.0
`,
			wantErr: "negative rate",
			check: func(c Config) string {
				if c.CustomCatalog() {
					return "model with negative rate kept"
				}
				return ""
			},
		},
		{
			// The API is the authority on effort/verbosity values: a level
			// Oolong doesn't know yet must pass through, not be dropped.
			name: "unknown effort and verbosity pass through",
			data: `
[[models]]
id = "gpt-5.7-nova"
reasoning_effort = "galactic"
verbosity = "chatty"
`,
			check: func(c Config) string {
				if len(c.Models) != 1 {
					return "model dropped over an unknown effort"
				}
				if c.Models[0].ReasoningEffort != "galactic" || c.Models[0].Verbosity != "chatty" {
					return "effort/verbosity not kept as written"
				}
				return ""
			},
		},
		{
			name: "base_url global and per-model",
			data: `
base_url = "http://localhost:11434/v1"

[[models]]
id = "llama3.3"
base_url = "http://localhost:1234/v1"
context_window = 128000
`,
			check: func(c Config) string {
				if c.BaseURL != "http://localhost:11434/v1" {
					return "global base_url not parsed"
				}
				if c.Models[0].BaseURL != "http://localhost:1234/v1" {
					return "per-model base_url not parsed"
				}
				if c.Models[0].ContextWindow != 128000 {
					return "context_window not parsed"
				}
				return ""
			},
		},
		{
			name: "ollama provider is opt in globally and per model",
			data: `
base_url = "http://localhost:11434"
provider = "ollama"
[[models]]
id = "gemma3"
base_url = "http://other-host:11434/v1"
provider = "ollama"
`,
			check: func(c Config) string {
				if c.Provider != "ollama" || c.Models[0].Provider != "ollama" {
					return "ollama providers not parsed"
				}
				return ""
			},
		},
		{
			name:    "unknown providers are dropped",
			data:    "provider = \"automatic\"",
			wantErr: `provider "automatic"`,
			check: func(c Config) string {
				if c.Provider != "" {
					return "unknown provider kept"
				}
				return ""
			},
		},
		{
			name: "openai provider can be explicit",
			data: "provider = \"openai\"",
			check: func(c Config) string {
				if c.Provider != "openai" {
					return "explicit OpenAI provider not parsed"
				}
				return ""
			},
		},
		{
			name: "anthropic provider can be explicit",
			data: `
[[models]]
id = "claude-sonnet-5"
provider = "anthropic"
`,
			check: func(c Config) string {
				if c.Models[0].Provider != "anthropic" {
					return "explicit Anthropic provider not parsed"
				}
				return ""
			},
		},
		{
			name:    "bad global base_url dropped",
			data:    `base_url = "localhost:11434"`,
			wantErr: `base_url "localhost:11434"`,
			check: func(c Config) string {
				if c.BaseURL != "" {
					return "bad base_url kept"
				}
				return ""
			},
		},
		{
			name: "bad model base_url dropped, model kept",
			data: `
[[models]]
id = "llama3.3"
base_url = "not a url"
`,
			wantErr: `model llama3.3 base_url`,
			check: func(c Config) string {
				if len(c.Models) != 1 || c.Models[0].BaseURL != "" {
					return "model or its bad base_url mishandled"
				}
				return ""
			},
		},
		{
			name: "negative context_window drops the model",
			data: `
[[models]]
id = "llama3.3"
context_window = -1
`,
			wantErr: "negative context_window",
			check: func(c Config) string {
				if c.CustomCatalog() {
					return "model with negative context_window kept"
				}
				return ""
			},
		},
		{
			name:    "default_model must be in the catalog",
			data:    `default_model = "gpt-nope"`,
			wantErr: `default_model "gpt-nope"`,
			check: func(c Config) string {
				if c.DefaultModel != "" {
					return "unknown default_model kept"
				}
				return ""
			},
		},
		{
			name: "default_model may come from the custom catalog",
			data: `
default_model = "gpt-5.4"

[[models]]
id = "gpt-5.4"
`,
			check: func(c Config) string {
				if c.DefaultModel != "gpt-5.4" {
					return "custom-catalog default_model dropped"
				}
				return ""
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parse(tt.data)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("parse() error = %v, want none", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parse() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parse() error = %v, want substring %q", err, tt.wantErr)
				}
			}
			if msg := tt.check(c); msg != "" {
				t.Error(msg)
			}
		})
	}
}

func TestCustomEndpoint(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"", false},
		{OfficialBaseURL, false},
		{OfficialBaseURL + "/", false},
		{"http://localhost:11434/v1", true},
		{"https://openrouter.ai/api/v1", true},
	}
	for _, tt := range tests {
		if got := CustomEndpoint(tt.url); got != tt.want {
			t.Errorf("CustomEndpoint(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestHasCustomEndpointIncludesPerModelRoutes(t *testing.T) {
	c := Config{Models: []Model{{ID: "gemma3", BaseURL: "http://localhost:11434", Provider: "ollama"}}}
	if !c.HasCustomEndpoint() {
		t.Error("per-model Ollama route was not detected")
	}
	if (Config{}).HasCustomEndpoint() {
		t.Error("default config reports a custom endpoint")
	}
}

func TestAnthropicEndpointStillRequiresAKey(t *testing.T) {
	c := Config{Models: []Model{{ID: "claude-test", Provider: "anthropic", BaseURL: "https://api.anthropic.com"}}}
	if c.HasCustomEndpoint() {
		t.Error("Anthropic endpoint was treated as a keyless custom route")
	}
}

func TestAnthropicGlobalProviderAppliesToModelEndpoints(t *testing.T) {
	c := Config{Provider: "anthropic", Models: []Model{{ID: "claude-test", BaseURL: "https://api.anthropic.com"}}}
	if c.HasCustomEndpoint() {
		t.Error("inherited Anthropic endpoint was treated as keyless")
	}
}

func TestLoadRespectsXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "oolong"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "oolong", "config.toml"),
		[]byte(`accent = "#7D56F4"`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Accent != "#7D56F4" {
		t.Errorf("Accent = %q, want #7D56F4", c.Accent)
	}
}

func TestInitWritesScaffoldOnce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := Init()
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if path != Path() {
		t.Errorf("Init() path = %q, want %q", path, Path())
	}
	// The scaffold is fully commented out, so it must load as the zero
	// (all-defaults) config with no error.
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() after Init() error = %v", err)
	}
	if c.DefaultModel != "" || c.CustomCatalog() {
		t.Errorf("scaffold config is not all-defaults: %+v", c)
	}
	// A second init must refuse to clobber the existing file.
	if _, err := Init(); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second Init() error = %v, want already-exists", err)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want none", err)
	}
	if c.CustomCatalog() {
		t.Error("missing file produced a custom catalog")
	}
}
