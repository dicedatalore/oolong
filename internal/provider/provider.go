// Package provider resolves configured models into concrete provider routes
// and constructs the matching clients. It is the single routing authority for
// both interactive and one-shot sessions.
package provider

import (
	"os"

	provideranthropic "github.com/dicedatalore/oolong/internal/anthropic"
	"github.com/dicedatalore/oolong/internal/config"
	providergoogle "github.com/dicedatalore/oolong/internal/google"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/ollama"
	"github.com/dicedatalore/oolong/internal/openai"
)

type Name string

const (
	OpenAI    Name = "openai"
	Anthropic Name = "anthropic"
	Google    Name = "google"
	Ollama    Name = "ollama"
)

// Route is the fully resolved destination for one model. Provider and BaseURL
// have already inherited global configuration and environment overrides.
type Route struct {
	Provider Name
	Model    config.Model
	BaseURL  string
}

// Resolver owns configuration, environment, and credential lookup. The
// function fields make routing deterministic in tests without changing the
// process environment or touching the OS keychain.
type Resolver struct {
	Config      config.Config
	Getenv      func(string) string
	ResolveKey  func(keystore.Provider) string
	BuildClient func(Route, string) openai.ChatClient
}

func NewResolver(cfg config.Config) *Resolver {
	return &Resolver{Config: cfg, Getenv: os.Getenv, ResolveKey: keystore.Resolve, BuildClient: NewClient}
}

func (r *Resolver) getenv(name string) string {
	if r.Getenv == nil {
		return ""
	}
	return r.Getenv(name)
}

func (r *Resolver) key(provider keystore.Provider) string {
	if r.ResolveKey == nil {
		return ""
	}
	return r.ResolveKey(provider)
}

// RouteFor resolves a model id. Unknown ids inherit the global route so an
// explicit --model can address models omitted from the catalog.
func (r *Resolver) RouteFor(id string) Route {
	var model config.Model
	for _, candidate := range r.Config.Catalog() {
		if candidate.ID == id {
			model = candidate
			break
		}
	}
	if model.ID == "" {
		model.ID = id
	}
	globalProvider := Name(r.Config.Provider)
	if globalProvider == "" {
		globalProvider = OpenAI
	}
	provider := Name(model.Provider)
	if provider == "" {
		provider = globalProvider
	}
	baseURL := model.BaseURL
	if baseURL == "" && provider == globalProvider {
		baseURL = r.Config.BaseURL
	}
	if provider == OpenAI {
		if envURL := r.getenv("OPENAI_BASE_URL"); envURL != "" {
			baseURL = envURL
		}
	}
	return Route{Provider: provider, Model: model, BaseURL: baseURL}
}

func KeyProvider(name Name) (keystore.Provider, bool) {
	switch name {
	case Anthropic:
		return keystore.Anthropic, true
	case Google:
		return keystore.Google, true
	case OpenAI:
		return keystore.OpenAI, true
	default:
		return "", false
	}
}

// Available reports whether the route can be attempted with current local
// configuration. Custom OpenAI endpoints may implement their own auth and
// Ollama is keyless; official keyed providers require a configured key.
func (r *Resolver) Available(route Route) bool {
	if route.Provider == Ollama {
		return true
	}
	keyProvider, keyed := KeyProvider(route.Provider)
	if !keyed {
		return false
	}
	if r.key(keyProvider) != "" {
		return true
	}
	return route.Provider == OpenAI && config.CustomEndpoint(route.BaseURL)
}

func (r *Resolver) ClientFor(id string) openai.ChatClient {
	route := r.RouteFor(id)
	if !r.Available(route) {
		return nil
	}
	var key string
	if keyProvider, ok := KeyProvider(route.Provider); ok {
		key = r.key(keyProvider)
	}
	build := r.BuildClient
	if build == nil {
		build = NewClient
	}
	return build(route, key)
}

// NewClient constructs a client for an already-resolved route.
func NewClient(route Route, key string) openai.ChatClient {
	switch route.Provider {
	case Anthropic:
		if route.BaseURL != "" {
			return provideranthropic.New(key, provideranthropic.WithBaseURL(route.BaseURL))
		}
		return provideranthropic.New(key)
	case Google:
		if route.BaseURL != "" {
			return providergoogle.New(key, providergoogle.WithBaseURL(route.BaseURL))
		}
		return providergoogle.New(key)
	case Ollama:
		return ollama.New(route.BaseURL)
	default:
		if route.BaseURL != "" {
			return openai.New(key, openai.WithBaseURL(route.BaseURL))
		}
		return openai.New(key)
	}
}

func (r *Resolver) FirstAvailableModel() string {
	if r.Config.DefaultModel != "" {
		return r.Config.DefaultModel
	}
	for _, model := range r.Config.Catalog() {
		if r.Available(r.RouteFor(model.ID)) {
			return model.ID
		}
	}
	return r.Config.Catalog()[0].ID
}

func (r *Resolver) AnyAvailable() bool {
	for _, model := range r.Config.Catalog() {
		if r.Available(r.RouteFor(model.ID)) {
			return true
		}
	}
	return false
}

// RouteForProvider returns the first configured route for a provider, falling
// back to that provider's official endpoint when it has no catalog entry.
func (r *Resolver) RouteForProvider(name Name) Route {
	global := r.RouteFor("")
	if global.Provider == name {
		return global
	}
	for _, model := range r.Config.Catalog() {
		route := r.RouteFor(model.ID)
		if route.Provider == name {
			return route
		}
	}
	return Route{Provider: name}
}

func (r *Resolver) ValidateKey(name Name, key string) error {
	route := r.RouteForProvider(name)
	switch name {
	case Anthropic:
		return provideranthropic.ValidateKeyAt(key, route.BaseURL)
	case Google:
		return providergoogle.ValidateKeyAt(key, route.BaseURL)
	default:
		// Custom OpenAI-compatible endpoints may not implement /models and
		// may use non-OpenAI authentication, so store their keys as supplied.
		if config.CustomEndpoint(route.BaseURL) {
			return nil
		}
		return openai.ValidateKeyAt(key, route.BaseURL)
	}
}
