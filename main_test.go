package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/version"
)

func TestRunEarlyCommands(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"version", []string{"--version"}, 0, "oolong test-version\n", ""},
		{"unknown config command", []string{"config", "wat"}, 2, "", `unknown command "config wat"`},
		{"resume with one shot", []string{"--resume", "chat.md", "hello"}, 2, "", "--resume opens the TUI"},
		{"help", []string{"--help"}, 0, "", "Usage:"},
		{"bad flag", []string{"--wat"}, 2, "", "flag provided but not defined"},
	}
	oldVersion := version.Version
	version.Version = "test-version"
	t.Cleanup(func() { version.Version = oldVersion })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			if got := run(tt.args, &stdout, &stderr); got != tt.wantCode {
				t.Fatalf("run() = %d, want %d; stderr: %s", got, tt.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestRunConfigInit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	var stdout, stderr strings.Builder
	if code := run([]string{"config", "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(config init) = %d; stderr: %s", code, stderr.String())
	}
	wantPath := filepath.Join(dir, "oolong", "config.toml")
	if !strings.Contains(stdout.String(), wantPath) {
		t.Errorf("stdout = %q, want config path %q", stdout.String(), wantPath)
	}
}

func TestApplyConfigOverrides(t *testing.T) {
	cfg := config.Config{
		Provider:     "openai",
		BaseURL:      "https://configured.example/v1",
		DefaultModel: "configured-model",
		Models: []config.Model{
			{ID: "openai-default", BaseURL: "https://model.example/v1"},
			{ID: "openai-explicit", Provider: "openai", BaseURL: "https://model.example/v1"},
			{ID: "claude", Provider: "anthropic", BaseURL: "https://anthropic.example"},
		},
	}
	applyConfigOverrides(&cfg, "https://env.example/v1", "flag-model")
	if cfg.Provider != "" || cfg.BaseURL != "" {
		t.Errorf("global OpenAI endpoint not cleared: provider=%q baseURL=%q", cfg.Provider, cfg.BaseURL)
	}
	if cfg.DefaultModel != "flag-model" {
		t.Errorf("default model = %q, want flag-model", cfg.DefaultModel)
	}
	for _, i := range []int{0, 1} {
		if cfg.Models[i].Provider != "openai" || cfg.Models[i].BaseURL != "" {
			t.Errorf("OpenAI model %d not redirected to env endpoint: %+v", i, cfg.Models[i])
		}
	}
	if got := cfg.Models[2]; got.Provider != "anthropic" || got.BaseURL != "https://anthropic.example" {
		t.Errorf("Anthropic model changed by OpenAI override: %+v", got)
	}
}
