package ui

// Shared helpers for the tests in this package. Tests drive the UI the same
// way the Bubble Tea runtime does: build a Model, feed messages to Update,
// and assert on the returned state.

import (
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/openai/openai-go/v3/option"

	"github.com/dicedatalore/oolong/internal/openai"
)

// clientFor returns a client that talks to the fake server instead of the
// real API.
func clientFor(srv *httptest.Server) *openai.Client {
	return openai.New("test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
}

// enterChat gets a fresh model into the chat state with the first model picked.
func enterChat(t *testing.T, srv *httptest.Server) tea.Model {
	t.Helper()
	var model tea.Model = New(clientFor(srv), "dark")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.(Model).state != stateChat {
		t.Fatal("not in chat state after picking a model")
	}
	return model
}

// typeText feeds s to the model one key press at a time.
func typeText(model tea.Model, s string) tea.Model {
	for _, r := range s {
		model, _ = model.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return model
}
