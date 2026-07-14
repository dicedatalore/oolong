package main

import (
	"os"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "bubble-chat"
	keyringUser    = "openai_api_key"
)

func keyringGet() (string, error) {
	return keyring.Get(keyringService, keyringUser)
}

func keyringSet(key string) error {
	return keyring.Set(keyringService, keyringUser, key)
}

func keyringDelete() error {
	return keyring.Delete(keyringService, keyringUser)
}

// resolveAPIKey returns the API key from the environment if set, otherwise
// from the OS keychain. Any keychain error is treated as "no stored key".
func resolveAPIKey() string {
	if k := os.Getenv("OPENAI_API_KEY"); k != "" {
		return k
	}
	if k, err := keyringGet(); err == nil && k != "" {
		return k
	}
	return ""
}
