// Package keystore stores provider API keys in the OS keychain.
package keystore

import (
	"os"

	"github.com/zalando/go-keyring"
)

const service = "oolong"

type Provider string

const (
	OpenAI    Provider = "openai"
	Anthropic Provider = "anthropic"
	Google    Provider = "google"
)

func account(provider Provider) string { return string(provider) + "_api_key" }

// EnvName is the environment variable that supplies a provider's key.
// Google uses GEMINI_API_KEY, the Gemini API's documented variable.
func EnvName(provider Provider) string {
	switch provider {
	case Anthropic:
		return "ANTHROPIC_API_KEY"
	case Google:
		return "GEMINI_API_KEY"
	default:
		return "OPENAI_API_KEY"
	}
}

func Get(provider Provider) (string, error) {
	return keyring.Get(service, account(provider))
}

func Set(provider Provider, key string) error {
	return keyring.Set(service, account(provider), key)
}

func Delete(provider Provider) error {
	err := keyring.Delete(service, account(provider))
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}

// DeleteAll removes every provider credential managed by Oolong.
func DeleteAll() error {
	var first error
	for _, provider := range []Provider{OpenAI, Anthropic, Google} {
		if err := Delete(provider); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Resolve returns the provider key from its environment variable if set,
// otherwise from the OS keychain. Keychain errors mean no stored key.
func Resolve(provider Provider) string {
	if key := os.Getenv(EnvName(provider)); key != "" {
		return key
	}
	if key, err := Get(provider); err == nil && key != "" {
		return key
	}
	return ""
}

// Status describes credential availability without exposing the secret.
func Status(provider Provider) string {
	if os.Getenv(EnvName(provider)) != "" {
		return "environment"
	}
	if key, err := Get(provider); err == nil && key != "" {
		return "keychain"
	}
	return "not set"
}

func Any() bool {
	return Resolve(OpenAI) != "" || Resolve(Anthropic) != "" || Resolve(Google) != ""
}
