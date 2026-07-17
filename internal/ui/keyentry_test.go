package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newKeyEntryModel builds a model with no client, which starts on the key
// entry screen.
func newKeyEntryModel(t *testing.T) tea.Model {
	t.Helper()
	var model tea.Model = New(nil, "dark")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if model.(Model).state != stateKeyEntry {
		t.Fatal("nil client did not start on the key entry screen")
	}
	return model
}

func TestKeyEntryRejectsEmptyKey(t *testing.T) {
	model := newKeyEntryModel(t)
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := model.(Model)
	if am.keyErr == "" {
		t.Error("enter with empty input did not set an error")
	}
	if am.keyValidating {
		t.Error("enter with empty input started validation")
	}
	if am.state != stateKeyEntry {
		t.Error("enter with empty input left the key entry screen")
	}
}

func TestKeyEntryValidationFailureStaysOnScreen(t *testing.T) {
	model := newKeyEntryModel(t)
	model = typeText(model, "sk-bad")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.(Model).keyValidating {
		t.Fatal("enter with a key did not start validation")
	}

	// Don't run the returned command (it would call the real API); inject
	// the validation result directly instead.
	model, _ = model.Update(keyCheckMsg{key: "sk-bad", err: errors.New("invalid API key")})
	am := model.(Model)
	if am.keyValidating {
		t.Error("still validating after result arrived")
	}
	if am.keyErr != "invalid API key" {
		t.Errorf("keyErr = %q, want %q", am.keyErr, "invalid API key")
	}
	if am.state != stateKeyEntry {
		t.Error("failed validation left the key entry screen")
	}
}
