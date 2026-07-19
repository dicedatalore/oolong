package keystore

import (
	"testing"

	"github.com/zalando/go-keyring"
)

func TestProviderKeysAreSeparated(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	if err := Set(OpenAI, "openai-secret"); err != nil {
		t.Fatal(err)
	}
	if err := Set(Anthropic, "anthropic-secret"); err != nil {
		t.Fatal(err)
	}
	if got := Resolve(OpenAI); got != "openai-secret" {
		t.Errorf("OpenAI key = %q", got)
	}
	if got := Resolve(Anthropic); got != "anthropic-secret" {
		t.Errorf("Anthropic key = %q", got)
	}
}

func TestEnvironmentOverridesKeychain(t *testing.T) {
	keyring.MockInit()
	if err := Set(Anthropic, "stored"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "environment")
	if got := Resolve(Anthropic); got != "environment" {
		t.Errorf("key = %q", got)
	}
	if got := Status(Anthropic); got != "environment" {
		t.Errorf("status = %q", got)
	}
}

func TestDeleteAll(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	_ = Set(OpenAI, "one")
	_ = Set(Anthropic, "two")
	if err := DeleteAll(); err != nil {
		t.Fatal(err)
	}
	if Any() {
		t.Error("keys remain after DeleteAll")
	}
}
