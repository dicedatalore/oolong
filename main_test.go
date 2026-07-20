package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/provider"
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
		{"provider without model", []string{"--provider", "anthropic"}, 2, "", "--provider requires --model"},
		{"unknown provider", []string{"--model", "x", "--provider", "unknown"}, 2, "", `unsupported provider "unknown"`},
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

func TestRunWithInjectedResetFailure(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runWith([]string{"--reset-key"}, dependencies{
		stdout:     &stdout,
		stderr:     &stderr,
		deleteKeys: func() error { return errors.New("keychain unavailable") },
	})
	if code != 1 || !strings.Contains(stderr.String(), "keychain unavailable") {
		t.Fatalf("runWith() = %d, stderr %q", code, stderr.String())
	}
}

func TestProviderEnvironmentOverride(t *testing.T) {
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
	resolver := provider.NewResolver(cfg)
	resolver.Getenv = func(name string) string {
		if name == "OPENAI_BASE_URL" {
			return "https://env.example/v1"
		}
		return ""
	}
	for _, id := range []string{"openai-default", "openai-explicit"} {
		if route := resolver.RouteFor(id); route.Provider != provider.OpenAI || route.BaseURL != "https://env.example/v1" {
			t.Errorf("OpenAI route %q = %+v", id, route)
		}
	}
	if route := resolver.RouteFor("claude"); route.Provider != provider.Anthropic || route.BaseURL != "https://anthropic.example" {
		t.Errorf("Anthropic route changed by OpenAI override: %+v", route)
	}
}
