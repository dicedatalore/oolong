//go:build e2e

package main

import "github.com/zalando/go-keyring"

// The E2E binary must never inspect or mutate a developer's real OS keychain.
// This build-tagged initializer replaces it with go-keyring's in-memory store.
func init() {
	keyring.MockInit()
}
