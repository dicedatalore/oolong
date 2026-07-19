package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/zalando/go-keyring"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/keystore"
)

func newKeyManagerModel(t *testing.T) tea.Model {
	t.Helper()
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 90, Height: 28})
	if model.(Model).state != statePicker {
		t.Fatal("a keyless launch did not remain on the picker")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if model.(Model).state != stateKeyManager {
		t.Fatal("ctrl+k did not open the key manager")
	}
	return model
}

func TestNoKeysPromptForKeyManager(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 90, Height: 28})
	am := model.(Model)
	if am.state != statePicker {
		t.Fatalf("state = %v, want picker", am.state)
	}
	if got := len(am.picker.Items()); got != 0 {
		t.Errorf("keyless picker shows %d default models, want none", got)
	}
	if !strings.Contains(am.viewPicker(), "ctrl+k opens the key manager") {
		t.Error("keyless picker does not prompt for the key manager")
	}
}

func TestDefaultModelsAppearOnlyForRelevantKey(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	model := New(nil, "dark", config.Config{}, "")
	if got := len(model.picker.Items()); got != 0 {
		t.Fatalf("initial defaults = %d, want none", got)
	}
	if err := keystore.Set(keystore.Anthropic, "sk-ant-test"); err != nil {
		t.Fatal(err)
	}
	model.refreshBuiltinCatalog()
	if got := len(model.picker.Items()); got != 0 {
		t.Errorf("Anthropic key exposed %d OpenAI defaults", got)
	}
	if err := keystore.Set(keystore.OpenAI, "sk-test"); err != nil {
		t.Fatal(err)
	}
	model.refreshBuiltinCatalog()
	if got := len(model.picker.Items()); got != len(config.Builtin) {
		t.Errorf("OpenAI key exposed %d defaults, want %d", got, len(config.Builtin))
	}
}

func TestKeyManagerRejectsEmptyKey(t *testing.T) {
	model := newKeyManagerModel(t)
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := model.(Model)
	if am.keyErr == "" || am.keyValidating {
		t.Errorf("empty key state: error=%q validating=%v", am.keyErr, am.keyValidating)
	}
}

func TestClosingKeyManagerRestartsPickerSparkles(t *testing.T) {
	model := newKeyManagerModel(t)
	before := model.(Model).sparkleTag
	model, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	am := model.(Model)
	if am.state != statePicker {
		t.Fatalf("state = %v, want picker", am.state)
	}
	if am.sparkleTag != before+1 {
		t.Errorf("sparkle tag = %d, want %d", am.sparkleTag, before+1)
	}
	if cmd == nil {
		t.Error("closing key manager did not schedule a new sparkle tick")
	}
}

func TestOpenAIValidationFailureStaysInManager(t *testing.T) {
	model := newKeyManagerModel(t)
	model = typeText(model, "sk-bad")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.(Model).keyValidating {
		t.Fatal("OpenAI key did not start validation")
	}
	model, _ = model.Update(keyCheckMsg{provider: keystore.OpenAI, key: "sk-bad", err: errors.New("invalid API key")})
	am := model.(Model)
	if am.state != stateKeyManager || am.keyErr != "invalid API key" {
		t.Errorf("state=%v error=%q", am.state, am.keyErr)
	}
}

func TestAnthropicKeySavedOnlyToKeychainAndInputCleared(t *testing.T) {
	model := newKeyManagerModel(t)
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = typeText(model, "sk-ant-test")
	model, _ = model.Update(keyCheckMsg{provider: keystore.Anthropic, key: "sk-ant-test"})
	am := model.(Model)
	if am.anthropicKeyInput.Value() != "" {
		t.Error("Anthropic input retained the saved secret")
	}
	got, err := keystore.Get(keystore.Anthropic)
	if err != nil || got != "sk-ant-test" {
		t.Fatalf("keychain value = %q, %v", got, err)
	}
	if am.state != stateKeyManager {
		t.Error("saving a key unexpectedly closed the manager")
	}
}
