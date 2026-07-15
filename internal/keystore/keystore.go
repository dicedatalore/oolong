// Package keystore stores the OpenAI API key in the OS keychain.
package keystore

import (
	"os"

	"github.com/zalando/go-keyring"
)

const (
	service = "oolong"
	user    = "openai_api_key"
)

func Get() (string, error) {
	return keyring.Get(service, user)
}

func Set(key string) error {
	return keyring.Set(service, user, key)
}

func Delete() error {
	return keyring.Delete(service, user)
}

// Resolve returns the API key from the environment if set, otherwise from
// the OS keychain. Any keychain error is treated as "no stored key".
func Resolve() string {
	if k := os.Getenv("OPENAI_API_KEY"); k != "" {
		return k
	}
	if k, err := Get(); err == nil && k != "" {
		return k
	}
	return ""
}
