package provider

import (
	"testing"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/keystore"
)

func isolatedResolver(cfg config.Config, keys map[keystore.Provider]string, env map[string]string) *Resolver {
	r := NewResolver(cfg)
	r.ResolveKey = func(provider keystore.Provider) string { return keys[provider] }
	r.Getenv = func(name string) string { return env[name] }
	return r
}

func TestRouteResolution(t *testing.T) {
	cfg := config.Config{
		Provider: "anthropic",
		BaseURL:  "https://global.example",
		Models: []config.Model{
			{ID: "inherited"},
			{ID: "openai", Provider: "openai", BaseURL: "https://configured.example/v1"},
			{ID: "ollama", Provider: "ollama", BaseURL: "http://localhost:11434"},
		},
	}
	r := isolatedResolver(cfg, nil, map[string]string{"OPENAI_BASE_URL": "https://env.example/v1"})
	tests := []struct {
		id       string
		provider Name
		baseURL  string
	}{
		{"inherited", Anthropic, "https://global.example"},
		{"openai", OpenAI, "https://env.example/v1"},
		{"ollama", Ollama, "http://localhost:11434"},
	}
	for _, tt := range tests {
		route := r.RouteFor(tt.id)
		if route.Provider != tt.provider || route.BaseURL != tt.baseURL {
			t.Errorf("RouteFor(%q) = %+v, want %s at %s", tt.id, route, tt.provider, tt.baseURL)
		}
	}
}

func TestGlobalEndpointDoesNotLeakAcrossProviders(t *testing.T) {
	cfg := config.Config{
		Provider: "openai",
		BaseURL:  "https://openai-proxy.example/v1",
		Models:   []config.Model{{ID: "claude", Provider: "anthropic"}},
	}
	route := isolatedResolver(cfg, nil, nil).RouteFor("claude")
	if route.BaseURL != "" {
		t.Fatalf("Anthropic route inherited OpenAI endpoint %q", route.BaseURL)
	}
}

func TestPerModelKeylessRouteDoesNotCreateGlobalOpenAIAvailability(t *testing.T) {
	cfg := config.Config{Models: []config.Model{{ID: "local", Provider: "ollama", BaseURL: "http://localhost:11434"}}}
	r := isolatedResolver(cfg, nil, nil)
	if !r.Available(r.RouteFor("local")) {
		t.Fatal("Ollama route is not available")
	}
	if r.Available(r.RouteFor("unknown-openai")) {
		t.Fatal("unkeyed official OpenAI route became available through a per-model local endpoint")
	}
	if got := r.FirstAvailableModel(); got != "local" {
		t.Fatalf("FirstAvailableModel() = %q, want local", got)
	}
}

func TestFirstAvailableModelUsesProviderCredentials(t *testing.T) {
	cfg := config.Config{Models: []config.Model{
		{ID: "openai", Provider: "openai"},
		{ID: "claude", Provider: "anthropic"},
	}}
	r := isolatedResolver(cfg, map[keystore.Provider]string{keystore.Anthropic: "test"}, nil)
	if got := r.FirstAvailableModel(); got != "claude" {
		t.Fatalf("FirstAvailableModel() = %q, want claude", got)
	}
}
